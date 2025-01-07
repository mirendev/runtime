package rpc

import (
	"context"
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

	mux *http.ServeMux
}

type heldCapability struct {
	*Interface

	lastContact time.Time

	persistent bool
	refs       atomic.Int32
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
		objects:  make(map[OID]*heldCapability),
		registry: make(map[string]OID),
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
	capa := s.AssignCapability(iface)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.registry[name] = capa.OID
}

func (s *Server) NewClient(capa *Capability) *Client {
	return &Client{
		State:  s.state,
		oid:    capa.OID,
		remote: capa.Address,
	}
}

const BootstrapOID = "!bootstrap"

func (s *Server) AssignCapability(i *Interface) *Capability {
	buf := make([]byte, 16)

	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		panic(err)
	}

	oid := OID(base58.Encode(buf))

	capa := &Capability{
		OID:     oid,
		Address: s.state.transport.Conn.LocalAddr().String(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	hc := &heldCapability{
		Interface:   i,
		lastContact: time.Now(),
	}

	hc.refs.Add(1)

	s.objects[oid] = hc

	return capa
}

func (s *Server) setupMux() {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /_rpc/call/{oid}/{method}", s.handleCalls)
	mux.HandleFunc("GET /_rpc/lookup/{name}", s.lookup)
	mux.HandleFunc("POST /_rpc/ref/{oid}", s.refCapa)
	mux.HandleFunc("POST /_rpc/deref/{oid}", s.derefCapa)

	s.mux = mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

type lookupResponse struct {
	OID   string `json:"oid,omitempty"`
	Error string `json:"error,omitempty"`
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	s.mu.Lock()
	defer s.mu.Unlock()

	w.WriteHeader(http.StatusOK)

	w.Header().Set("Content-Type", "application/json")

	oid, ok := s.registry[name]
	if !ok {
		json.NewEncoder(w).Encode(lookupResponse{Error: "unknown object: " + name})
	} else {
		json.NewEncoder(w).Encode(lookupResponse{OID: string(oid)})
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
		if !hc.persistent {
			hc.refs.Add(1)
		}
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

		if hc.persistent {
			rep.Status = "persistent"
		} else if hc.refs.Add(-1) == 0 {
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

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))
	method := r.PathValue("method")

	//fields := strings.SplitN(r.URL.Path, "/", 4)

	w.Header().Set("Trailer", "rpc-status, rpc-error")

	//oid := OID(fields[1])
	//method := fields[2]

	ctx := r.Context()

	call := &Call{
		s:      s,
		r:      r,
		oid:    oid,
		method: method,
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
