package rpc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
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

type Server struct {
	state *State

	mu       *sync.Mutex
	objects  map[OID]*heldCapability
	registry map[string]OID

	persistent map[string]*Interface

	knownAddresses map[string]string

	mux *http.ServeMux
}

type heldInterface struct {
	*Interface
	refs atomic.Int32
}

type heldCapability struct {
	*heldInterface

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

type Interface struct {
	methods map[string]Method
	closer  io.Closer
}

func NewInterface(methods []Method, obj any) *Interface {
	m := make(map[string]Method)
	for _, mm := range methods {
		m[mm.Name] = mm
	}

	i := &Interface{
		methods: m,
	}

	if c, ok := obj.(io.Closer); ok {
		i.closer = c
	}

	return i
}

func (s *Server) ExposeValue(name string, iface *Interface) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.persistent[name] = iface
}

const BootstrapOID = "!bootstrap"

func (s *Server) assignCapability(i *Interface, pub ed25519.PublicKey, contactAddr string) *Capability {
	if len(pub) != ed25519.PublicKeySize {
		panic("bad key!!!")
	}

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
		heldInterface: &heldInterface{
			Interface: i,
		},
		lastContact: time.Now(),
		pub:         pub,
	}

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

	s.objects[oid] = hc

	return capa
}

func (s *Server) setupMux() {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /_rpc/call/{oid}/{method}", s.handleCalls)
	mux.HandleFunc("POST /_rpc/lookup/{name}", s.lookup)
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
		s.state.log.Info("Failed to parse timestamp", "error", err)
		return "", false
	}

	if time.Since(t) > 10*time.Minute {
		s.state.log.Info("Timestamp too old", "timestamp", t)
		return "", false
	}

	sign := r.Header.Get("rpc-signature")
	if sign == "" {
		s.state.log.Info("No signature provided")
		return "", false
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s %s %s", r.Method, r.URL.Path, ts)

	bsign, err := base58.Decode(sign)
	if err != nil {
		s.state.log.Info("Failed to decode signature", "error", err)
		return "", false
	}

	spkey := r.Header.Get("rpc-public-key")

	key, err := base58.Decode(spkey)
	if err != nil {
		return "", false
	}

	pub := ed25519.PublicKey(key)

	if len(pub) != ed25519.PublicKeySize {
		s.state.log.Info("Invalid public key size", "size", len(pub))
		return "", false
	}

	if !ed25519.Verify(pub, buf.Bytes(), bsign) {
		s.state.log.Info("Failed to verify signature")
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

	_, ok := s.authRequest(r, oid)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
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
	defer s.mu.Unlock()

	if hc, ok := s.objects[oid]; ok {
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

	s.mu.Lock()
	defer s.mu.Unlock()

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
	iface, ok := s.persistent[name]
	if !ok {
		cbor.NewEncoder(w).Encode(lookupResponse{Error: "unknown object: " + name})
	} else {
		data, err := base58.Decode(pk)
		if err != nil {
			cbor.NewEncoder(w).Encode(lookupResponse{Error: "invalid public key"})
			return
		}

		capa := s.assignCapability(iface, ed25519.PublicKey(data), ca)

		cbor.NewEncoder(w).Encode(lookupResponse{Capability: capa})
	}
}

type refResponse struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) refCapa(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	_, ok := s.authRequest(r, oid)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
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

	_, ok := s.authRequest(r, oid)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusOK)

	s.mu.Lock()
	defer s.mu.Unlock()

	if hc, ok := s.objects[oid]; ok {
		var rep refResponse

		if hc.refs.Add(-1) == 0 {
			delete(s.objects, oid)
			go hc.Close()

			rep.Status = "removed"
		} else {
			rep.Status = "ok"
		}

		json.NewEncoder(w).Encode(rep)
	} else {
		json.NewEncoder(w).Encode(refResponse{Error: "unknown capability: " + string(oid)})
	}
}

func (s *Server) authRequest(r *http.Request, oid OID) (ed25519.PublicKey, bool) {
	ts := r.Header.Get("rpc-timestamp")

	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		s.state.log.Info("Failed to parse timestamp", "error", err)
		return nil, false
	}

	if time.Since(t) > 10*time.Minute {
		s.state.log.Info("Timestamp too old", "timestamp", t)
		return nil, false
	}

	sign := r.Header.Get("rpc-signature")
	if sign == "" {
		s.state.log.Info("No signature provided")
		return nil, false
	}

	s.mu.Lock()
	capa, ok := s.objects[oid]
	s.mu.Unlock()

	if !ok {
		s.state.log.Info("Found capability", "oid", oid)
		return nil, false
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s %s %s", r.Method, r.URL.Path, ts)

	bsign, err := base58.Decode(sign)
	if err != nil {
		s.state.log.Info("Failed to decode signature", "error", err)
		return nil, false
	}

	if len(capa.pub) != ed25519.PublicKeySize {
		s.state.log.Info("Invalid public key size", "size", len(capa.pub))
		return nil, false
	}

	if !ed25519.Verify(capa.pub, buf.Bytes(), bsign) {
		s.state.log.Info("Failed to verify signature")
		return nil, false
	}

	return capa.pub, true
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	user, ok := s.authRequest(r, oid)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	method := r.PathValue("method")

	w.Header().Set("Trailer", "rpc-status, rpc-error")

	ctx := r.Context()

	call := &Call{
		s:      s,
		r:      r,
		oid:    oid,
		method: method,
		caller: user,
	}

	defer r.Body.Close()

	if iface, ok := s.objects[oid]; ok {
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

		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			call.peer = r.TLS.PeerCertificates[0]
			ctx = context.WithValue(ctx, connectionKey{}, &CurrentConnectionInfo{
				PeerSubject: r.TLS.PeerCertificates[0].Subject.String(),
			})
		}

		err := mm.Handler(ctx, call)
		if err != nil {
			s.state.log.Error("rpc call errored", "method", mm.InterfaceName+"."+mm.Name, "error", err)
			w.Header().Add("rpc-status", "error")
			w.Header().Add("rpc-error", err.Error())
			s.handleError(w, r, err)
			return
		}

		cbor.NewEncoder(w).Encode(call.results)
		w.Header().Add("rpc-status", "ok")
	} else {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("rpc-status", "unknown")
		w.Header().Add("rpc-error", "unknown object: "+string(oid))
	}
}

func (s *Server) handleError(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
