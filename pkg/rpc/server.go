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
	"runtime/debug"
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

	mu       sync.Mutex
	objects  map[OID]*heldCapability
	registry map[string]OID

	persistent map[string]*Interface

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
		objects:    make(map[OID]*heldCapability),
		registry:   make(map[string]OID),
		persistent: make(map[string]*Interface),
	}

	s.setupMux()

	return s
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

	//capa := s.AssignCapability(iface)

	//s.mu.Lock()
	//defer s.mu.Unlock()

	//s.registry[name] = capa.OID
}

func (s *Server) NewClient(capa *Capability) *Client {
	return &Client{
		State:  s.state,
		oid:    capa.OID,
		remote: capa.Address,
	}
}

const BootstrapOID = "!bootstrap"

func (s *Server) assignCapability(i *Interface, pub ed25519.PublicKey) *Capability {
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
		Address: s.state.transport.Conn.LocalAddr().String(),
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

func (s *Server) reexportCapability(target OID, cur *heldCapability, pub ed25519.PublicKey) *Capability {
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
		Address: s.state.transport.Conn.LocalAddr().String(),
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

	s.mux = mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.state.log.Info("HTTP Request", "method", r.Method, "path", r.URL.Path)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) reexport(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	pk := r.Header.Get("rpc-target-public-key")
	if pk == "" {
		http.Error(w, "public key not provided", http.StatusForbidden)
		return
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

		capa := s.reexportCapability(oid, hc, ed25519.PublicKey(data))

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

	s.state.log.Info("Lookup", "name", name)

	iface, ok := s.persistent[name]
	if !ok {
		json.NewEncoder(w).Encode(lookupResponse{Error: "unknown object: " + name})
	} else {
		data, err := base58.Decode(pk)
		if err != nil {
			json.NewEncoder(w).Encode(lookupResponse{Error: "invalid public key"})
			return
		}

		capa := s.assignCapability(iface, ed25519.PublicKey(data))

		cbor.NewEncoder(w).Encode(lookupResponse{Capability: capa})
	}
}

type refResponse struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) refCapa(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

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
		panic("bad key!!!")
	}

	if !ed25519.Verify(capa.pub, buf.Bytes(), bsign) {
		s.state.log.Info("Failed to verify signature")
		return nil, false
	}

	return capa.pub, true
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	s.state.log.Info("RPC Call", "oid", oid)

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

		fmt.Printf("Calling %s.%s\n", oid, method)

		defer func() {
			if r := recover(); r != nil {
				fmt.Println(r)
				debug.PrintStack()
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

		err := mm.Handler(ctx, call)
		if err != nil {
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

func (s *Server) handleError(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
