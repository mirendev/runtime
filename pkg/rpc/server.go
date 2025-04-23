package rpc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"miren.dev/runtime/pkg/webtransport"
)

func init() {
	/*
		                          _   _
		                         | | | |
		 _         _   ,_    __  | | | |
		|/  |   | |/  /  |  /  \_|/  |/
		|__/ \_/|/|__/   |_/\__/ |__/|__/
		       /|
		       \|

	*/
	os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
}

type HasReconstructFromState interface {
	ReconstructFromState(is *InterfaceState) (*Interface, error)
}

type Server struct {
	state *State

	mu       *sync.Mutex
	objects  map[OID]*heldCapability
	registry map[string]OID

	persistent map[string]*Interface

	knownAddresses map[string]string

	resolvers map[string]HasReconstructFromState

	mux *http.ServeMux
	ws  *webtransport.Server
}

type heldInterface struct {
	*Interface
	refs atomic.Int32
}

type heldCapability struct {
	*heldInterface

	category string

	lastContact time.Time

	pub ed25519.PublicKey
}

func (h *heldCapability) touch() {
	h.lastContact = time.Now()
}

func (h *heldCapability) Close() error {
	if h.closer != nil {
		return h.closer.Close()
	}

	return nil
}

func newServer() *Server {
	s := &Server{
		mu:             new(sync.Mutex),
		objects:        make(map[OID]*heldCapability),
		registry:       make(map[string]OID),
		persistent:     make(map[string]*Interface),
		knownAddresses: make(map[string]string),
		resolvers:      make(map[string]HasReconstructFromState),
	}

	s.setupMux()

	return s
}

func (s *Server) Clone(state *State) *Server {
	ns := *s
	ns.state = state

	return &ns
}

type Method struct {
	Name          string
	InterfaceName string
	Index         int
	Handler       func(ctx context.Context, call *Call) error
}

type HasRestoreState interface {
	RestoreState(iface any) (any, error)
}

type Interface struct {
	name    string
	methods map[string]Method
	closer  io.Closer

	forbidRestore bool
	restoreState  HasRestoreState
	constructor   HasReconstructFromState
}

func typeNameHash(obj any) string {
	t := reflect.TypeOf(obj)
	name := t.PkgPath() + "." + t.Name()
	return base58.Encode([]byte(name))
}

func NewInterface(methods []Method, obj any) *Interface {
	m := make(map[string]Method)
	for _, mm := range methods {
		m[mm.Name] = mm
	}

	i := &Interface{
		name:    methods[0].InterfaceName,
		methods: m,
	}

	if c, ok := obj.(io.Closer); ok {
		i.closer = c
	}

	if r, ok := obj.(HasRestoreState); ok {
		i.restoreState = r
	}

	if c, ok := obj.(HasReconstructFromState); ok {
		i.constructor = c
	}

	if _, ok := obj.(ForbidRestore); ok {
		i.forbidRestore = true
	}

	return i
}

func (s *Server) ExposeValue(name string, iface *Interface) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.persistent[name] = iface

	if iface.constructor != nil {
		s.resolvers[name] = iface.constructor
	}
}

const BootstrapOID = "!bootstrap"

func (s *Server) assignCapability(i *Interface, pub ed25519.PublicKey, contactAddr string, category string, inline bool) *Capability {
	if len(pub) != ed25519.PublicKeySize {
		panic("bad key!!!")
	}

	buf := make([]byte, 16)

	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		panic(err)
	}

	oid := OID(base58.Encode(buf))

	capa := &Capability{
		OID:     oid,
		User:    pub,
		Issuer:  s.state.pubkey,
		Address: contactAddr,
		Inline:  inline,
	}

	if inline {
		capa.Address = ""
	}

	hc := &heldCapability{
		heldInterface: &heldInterface{
			Interface: i,
		},
		category:    category,
		lastContact: time.Now(),
		pub:         pub,
	}

	if i.restoreState != nil {
		if rs, err := i.restoreState.RestoreState(i); err == nil {
			capa.RestoreState = &InterfaceState{
				Category:  category,
				Interface: i.name,
				Data:      rs,
			}
		}
	} else if !i.forbidRestore {
		capa.RestoreState = &InterfaceState{
			Category:  category,
			Interface: i.name,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	hc.refs.Add(1)

	s.objects[oid] = hc

	return capa
}

func (s *Server) reexportCapability(target OID, cur *heldCapability, pub ed25519.PublicKey, contactAddr string) *Capability {
	buf := make([]byte, 16)

	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		panic(err)
	}

	oid := OID(base58.Encode(buf))

	if contactAddr == "" {
		contactAddr = s.state.transport.Conn.LocalAddr().String()
	}

	capa := &Capability{
		OID:     oid,
		User:    pub,
		Issuer:  s.state.pubkey,
		Address: contactAddr,
	}

	hc := &heldCapability{
		heldInterface: cur.heldInterface,
		lastContact:   time.Now(),
		pub:           pub,
	}

	hc.refs.Add(1)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.objects[oid] = hc

	return capa
}

func (s *Server) setupMux() {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /_rpc/call/{oid}/{method}", s.handleCalls)
	mux.HandleFunc("CONNECT /_rpc/callstream/{oid}/{method}", s.startCallStream)
	mux.HandleFunc("POST /_rpc/lookup/{name}", s.lookup)
	mux.HandleFunc("POST /_rpc/reresolve", s.reresolve)
	mux.HandleFunc("POST /_rpc/reexport/{oid}", s.reexport)
	mux.HandleFunc("POST /_rpc/ref/{oid}", s.refCapa)
	mux.HandleFunc("POST /_rpc/deref/{oid}", s.derefCapa)
	mux.HandleFunc("POST /_rpc/identify", s.clientIdentify)

	s.mux = mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//s.state.log.Info("HTTP Request", "method", r.Method, "path", r.URL.Path)
	s.mux.ServeHTTP(w, r)
}

type identifyResponse struct {
	Ok       bool   `json:"ok,omitempty"`
	Error    string `json:"error,omitempty"`
	Address  string `json:"address,omitempty"`
	Identity string `json:"identity,omitempty"`
}

func (s *Server) checkIdentity(r *http.Request) (string, bool) {
	ts := r.Header.Get("rpc-timestamp")

	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		s.state.log.Warn("Failed to parse timestamp", "error", err)
		return "", false
	}

	if time.Since(t) > 10*time.Minute {
		s.state.log.Warn("Timestamp too old", "timestamp", t)
		return "", false
	}

	sign := r.Header.Get("rpc-signature")
	if sign == "" {
		s.state.log.Warn("No signature provided")
		return "", false
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s %s %s", r.Method, r.URL.Path, ts)

	bsign, err := base58.Decode(sign)
	if err != nil {
		s.state.log.Warn("Failed to decode signature", "error", err)
		return "", false
	}

	spkey := r.Header.Get("rpc-public-key")

	key, err := base58.Decode(spkey)
	if err != nil {
		return "", false
	}

	pub := ed25519.PublicKey(key)

	if len(pub) != ed25519.PublicKeySize {
		s.state.log.Warn("Invalid public key size", "size", len(pub))
		return "", false
	}

	if !ed25519.Verify(pub, buf.Bytes(), bsign) {
		s.state.log.Warn("Failed to verify signature")
		return "", false
	}

	return base58.Encode(pub), true
}

func (s *Server) clientIdentify(w http.ResponseWriter, r *http.Request) {
	id, ok := s.checkIdentity(r)
	if !ok {
		cbor.NewEncoder(w).Encode(identifyResponse{Error: "invalid identity"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cbor.NewEncoder(w).Encode(identifyResponse{
		Ok:       true,
		Address:  r.RemoteAddr,
		Identity: id,
	})
}

func (s *Server) reexport(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	_, ok := s.authRequest(r, w, oid)
	if !ok {
		return
	}

	pk := r.Header.Get("rpc-target-public-key")
	if pk == "" {
		http.Error(w, "public key not provided", http.StatusForbidden)
		return
	}

	// having the client provide the contact address allows the server
	// to provide capabilities for OTHER servers rather than just itself.
	// in this way, the client doesn't have to assume that just because it
	// looked up the capability here, the functionality is also here.
	//
	// NOTE: We don't actually support that atm, but this provides future
	// abilities.
	ca := r.Header.Get("rpc-contact-addr")
	if ca != "" {
		ca = s.state.transport.Conn.LocalAddr().String()
	}

	w.WriteHeader(http.StatusOK)

	s.mu.Lock()
	hc, ok := s.objects[oid]
	s.mu.Unlock()

	if ok {
		data, err := base58.Decode(pk)
		if err != nil {
			json.NewEncoder(w).Encode(lookupResponse{Error: "invalid public key"})
			return
		}

		capa := s.reexportCapability(oid, hc, ed25519.PublicKey(data), ca)

		cbor.NewEncoder(w).Encode(lookupResponse{Capability: capa})
	} else {
		cbor.NewEncoder(w).Encode(lookupResponse{
			Error: "unknown capability: " + string(oid),
		})
	}
}

type lookupResponse struct {
	Capability *Capability `json:"capability,omitempty"`
	Error      string      `json:"error,omitempty"`
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	pk := r.Header.Get("rpc-public-key")
	if pk == "" {
		http.Error(w, "public key not provided", http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusOK)

	w.Header().Set("Content-Type", "application/cbor")

	// having the client provide the contact address allows the server
	// to provide capabilities for OTHER servers rather than just itself.
	// in this way, the client doesn't have to assume that just because it
	// looked up the capability here, the functionality is also here.
	//
	// NOTE: We don't actually support that atm, but this provides future
	// abilities.
	ca := r.Header.Get("rpc-contact-addr")
	if ca != "" {
		ca = s.state.transport.Conn.LocalAddr().String()
	}

	//s.state.log.Info("Lookup", "name", name)

	// TODO: add condition codes to the error response rather than just a string
	s.mu.Lock()
	iface, ok := s.persistent[name]
	s.mu.Unlock()

	if !ok {
		cbor.NewEncoder(w).Encode(lookupResponse{Error: "unknown object: " + name})
	} else {
		data, err := base58.Decode(pk)
		if err != nil {
			cbor.NewEncoder(w).Encode(lookupResponse{Error: "invalid public key"})
			return
		}

		capa := s.assignCapability(iface, ed25519.PublicKey(data), ca, name, false)
		capa.RestoreState = &InterfaceState{
			Category:  "!persistent",
			Interface: name,
		}

		cbor.NewEncoder(w).Encode(lookupResponse{Capability: capa})
	}
}

func (s *Server) reresolve(w http.ResponseWriter, r *http.Request) {
	var rs InterfaceState

	err := cbor.NewDecoder(r.Body).Decode(&rs)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	pk := r.Header.Get("rpc-public-key")
	if pk == "" {
		http.Error(w, "public key not provided", http.StatusForbidden)
		return
	}

	var (
		iface    *Interface
		category string
	)

	if rs.Category == "!persistent" {
		name := rs.Interface

		category = name

		var ok bool
		// TODO: add condition codes to the error response rather than just a string
		s.mu.Lock()
		iface, ok = s.persistent[name]
		s.mu.Unlock()

		if !ok {
			cbor.NewEncoder(w).Encode(lookupResponse{Error: "unknown object: " + name})
			return
		}
	} else {
		s.mu.Lock()
		res, ok := s.resolvers[rs.Category]
		s.mu.Unlock()

		if ok {
			iface, err = res.ReconstructFromState(&rs)
			if err != nil {
				cbor.NewEncoder(w).Encode(lookupResponse{Error: "failed to resolve: " + err.Error()})
				return
			}

			if iface == nil {
				cbor.NewEncoder(w).Encode(lookupResponse{Error: fmt.Sprintf("unable to restore capability")})
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)

	w.Header().Set("Content-Type", "application/cbor")

	// having the client provide the contact address allows the server
	// to provide capabilities for OTHER servers rather than just itself.
	// in this way, the client doesn't have to assume that just because it
	// looked up the capability here, the functionality is also here.
	//
	// NOTE: We don't actually support that atm, but this provides future
	// abilities.
	ca := r.Header.Get("rpc-contact-addr")
	if ca != "" {
		ca = s.state.transport.Conn.LocalAddr().String()
	}

	// TODO: add condition codes to the error response rather than just a string
	pkdata, err := base58.Decode(pk)
	if err != nil {
		cbor.NewEncoder(w).Encode(lookupResponse{Error: "invalid public key"})
		return
	}

	capa := s.assignCapability(iface, ed25519.PublicKey(pkdata), ca, category, false)
	capa.RestoreState = &rs

	cbor.NewEncoder(w).Encode(lookupResponse{Capability: capa})
}

type refResponse struct {
	Status string `json:"status,omitempty" cbor:"status,omitempty"`
	Error  string `json:"error,omitempty" cbor:"error,omitempty"`
}

func (s *Server) refCapa(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	_, ok := s.authRequest(r, w, oid)
	if !ok {
		return
	}

	w.WriteHeader(http.StatusOK)

	s.mu.Lock()
	defer s.mu.Unlock()

	if hc, ok := s.objects[oid]; ok {
		hc.refs.Add(1)
		json.NewEncoder(w).Encode(refResponse{Status: "ok"})
	} else {
		json.NewEncoder(w).Encode(refResponse{Error: "unknown capability: " + string(oid)})
	}
}

func (s *Server) derefCapa(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	_, ok := s.authRequest(r, w, oid)
	if !ok {
		return
	}

	w.WriteHeader(http.StatusOK)

	if s.Deref(oid) {
		var rep refResponse
		rep.Status = "ok"
		json.NewEncoder(w).Encode(rep)
	} else {
		json.NewEncoder(w).Encode(refResponse{Error: "unknown capability: " + string(oid)})
	}
}

func (s *Server) Deref(oid OID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if hc, ok := s.objects[oid]; ok {
		if hc.refs.Add(-1) == 0 {
			delete(s.objects, oid)
			go hc.Close()

		}

		return true
	}

	return false
}

func (s *Server) authRequest(r *http.Request, w http.ResponseWriter, oid OID) (ed25519.PublicKey, bool) {
	ts := r.Header.Get("rpc-timestamp")

	if ts == "" {
		s.state.log.Warn("No timestamp provided for authentication")
		http.Error(w, "no timestamp provided", http.StatusForbidden)
		return nil, false
	}

	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		s.state.log.Warn("Failed to parse timestamp", "error", err)
		http.Error(w, "failed to parse timestamp", http.StatusForbidden)
		return nil, false
	}

	if time.Since(t) > 10*time.Minute {
		s.state.log.Warn("Timestamp too old", "timestamp", t)
		http.Error(w, "timestamp too old", http.StatusForbidden)
		return nil, false
	}

	sign := r.Header.Get("rpc-signature")
	if sign == "" {
		s.state.log.Warn("No signature provided")
		http.Error(w, "no signature provided", http.StatusForbidden)
		return nil, false
	}

	s.mu.Lock()
	capa, ok := s.objects[oid]
	s.mu.Unlock()

	if !ok {
		w.Header().Add("rpc-status", "unknown-capability")
		w.Header().Add("rpc-error", "unknown capability: "+string(oid))
		http.Error(w, "unknown capability", http.StatusNotFound)
		return nil, false
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s %s %s", r.Method, r.URL.Path, ts)

	bsign, err := base58.Decode(sign)
	if err != nil {
		s.state.log.Warn("Failed to decode signature", "error", err)
		http.Error(w, "failed to decode signature", http.StatusForbidden)
		return nil, false
	}

	if len(capa.pub) != ed25519.PublicKeySize {
		s.state.log.Warn("Invalid public key size", "size", len(capa.pub))
		http.Error(w, "invalid public key size", http.StatusForbidden)
		return nil, false
	}

	if !ed25519.Verify(capa.pub, buf.Bytes(), bsign) {
		s.state.log.Warn("Failed to verify signature")
		http.Error(w, "failed to verify signature", http.StatusForbidden)
		return nil, false
	}

	return capa.pub, true
}

type streamRequest struct {
	Kind   string `json:"kind" cbor:"kind"`
	OID    OID    `json:"oid" cbor:"oid"`
	Method string `json:"method" cbor:"method"`
	Status string `json:"status" cbor:"status"`
}

type controlStream struct {
	mu  sync.Mutex
	dec *cbor.Decoder
	enc *cbor.Encoder
}

func (cs *controlStream) NoReply(rs streamRequest, arg any) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	err := cs.enc.Encode(rs)
	if err != nil {
		return err
	}

	if arg != nil {
		return cs.enc.Encode(arg)
	}

	return nil
}

func (s *Server) startCallStream(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	method := r.PathValue("method")

	w.Header().Set("Trailer", "rpc-status, rpc-error")

	user, ok := s.authRequest(r, w, oid)
	if !ok {
		return
	}

	ctx := r.Context()

	s.mu.Lock()
	iface, ok := s.objects[oid]
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("rpc-status", "unknown-capability")
		w.Header().Add("rpc-error", "unknown object: "+string(oid))
		return
	}

	iface.touch()

	mm := iface.methods[method]
	if mm.Handler == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("rpc-status", "unknown")
		w.Header().Add("rpc-error", "unknown method: "+method)
		return
	}

	w.WriteHeader(http.StatusOK)

	defer func() {
		if r := recover(); r != nil {
			w.Header().Add("rpc-status", "panic")
			w.Header().Add("rpc-error", fmt.Sprint(r))
			panic(r)
		}
	}()

	ctx = Propagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

	tracer := Tracer()

	ctx, span := tracer.Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)

	defer span.End()

	span.SetAttributes(attribute.String("oid", string(oid)))

	sess, err := s.ws.Upgrade(w, r)
	if err != nil {
		s.state.log.Error("failed to upgrade connection", "error", err)
		http.Error(w, "failed to upgrade connection", http.StatusInternalServerError)
		return
	}

	ctrlstream, err := sess.AcceptStream(ctx)
	if err != nil {
		s.state.log.Error("failed to accept arg stream", "error", err)
		return
	}

	var cs controlStream
	cs.dec = cbor.NewDecoder(ctrlstream)
	cs.enc = cbor.NewEncoder(ctrlstream)

	defer ctrlstream.Close()

	call := &Call{
		s:        s,
		r:        r,
		oid:      oid,
		method:   method,
		caller:   user,
		category: iface.category,

		dec: cs.dec,

		wsSession: sess,
		ctrl:      &cs,
	}

	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		call.peer = r.TLS.PeerCertificates[0]
		ctx = context.WithValue(ctx, connectionKey{}, &CurrentConnectionInfo{
			PeerSubject: r.TLS.PeerCertificates[0].Subject.String(),
		})
	}

	err = mm.Handler(ctx, call)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.state.log.Error("rpc call errored", "method", mm.InterfaceName+"."+mm.Name, "error", err)
		}
		w.Header().Add("rpc-status", "error")
		w.Header().Add("rpc-error", err.Error())
		s.handleError(w, r, err)
		return
	}

	cs.NoReply(streamRequest{
		Kind: "result",
	}, call.results)
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	w.Header().Set("Trailer", "rpc-status, rpc-error")

	user, ok := s.authRequest(r, w, oid)
	if !ok {
		return
	}

	method := r.PathValue("method")

	ctx := r.Context()

	defer r.Body.Close()

	s.mu.Lock()
	iface, ok := s.objects[oid]
	s.mu.Unlock()

	if ok {
		iface.touch()

		mm := iface.methods[method]
		if mm.Handler == nil {
			w.WriteHeader(http.StatusNotFound)
			w.Header().Add("rpc-status", "unknown")
			w.Header().Add("rpc-error", "unknown method: "+method)
			return
		}

		w.WriteHeader(http.StatusOK)

		defer func() {
			if r := recover(); r != nil {
				w.Header().Add("rpc-status", "panic")
				w.Header().Add("rpc-error", fmt.Sprint(r))
				panic(r)
			}
		}()

		ctx = Propagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

		tracer := Tracer()

		ctx, span := tracer.Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)

		defer span.End()

		span.SetAttributes(attribute.String("oid", string(oid)))

		call := &Call{
			s:        s,
			r:        r,
			oid:      oid,
			method:   method,
			caller:   user,
			category: iface.category,
		}

		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			call.peer = r.TLS.PeerCertificates[0]
			ctx = context.WithValue(ctx, connectionKey{}, &CurrentConnectionInfo{
				PeerSubject: r.TLS.PeerCertificates[0].Subject.String(),
			})
		}

		err := mm.Handler(ctx, call)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				s.state.log.Error("rpc call errored", "method", mm.InterfaceName+"."+mm.Name, "error", err)
			}
			w.Header().Add("rpc-status", "error")
			w.Header().Add("rpc-error", err.Error())
			s.handleError(w, r, err)
			return
		}

		cbor.NewEncoder(w).Encode(call.results)
		w.Header().Add("rpc-status", "ok")
	} else {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("rpc-status", "unknown-capability")
		w.Header().Add("rpc-error", "unknown object: "+string(oid))
	}
}

func (s *Server) handleError(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
