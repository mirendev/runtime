package rpc

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/fxamacker/cbor/v2"
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
	state   *State
	objects map[OID]*Interface
}

func newServer() *Server {
	return &Server{
		objects: make(map[OID]*Interface),
	}
}

type Method struct {
	Name          string
	InterfaceName string
	Index         int
	Handler       func(ctx context.Context, call *Call) error
}

type Interface struct {
	methods map[string]Method
}

func NewInterface(methods []Method) *Interface {
	m := make(map[string]Method)
	for _, mm := range methods {
		m[mm.Name] = mm
	}

	return &Interface{
		methods: m,
	}
}

func (s *Server) ExposeValue(oid OID, iface *Interface) {
	if s.objects == nil {
		s.objects = make(map[OID]*Interface)
	}

	s.objects[oid] = iface
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
	oid := OID("blah")

	s.ExposeValue(oid, i)

	return &Capability{
		OID:     oid,
		Address: s.state.transport.Conn.LocalAddr().String(),
	}
}

func (s *Server) attachClient(w http.ResponseWriter, r *http.Request) {

}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	fields := strings.SplitN(r.URL.Path, "/", 4)

	w.Header().Set("Trailer", "rpc-status, rpc-error")

	oid := OID(fields[1])
	method := fields[2]

	ctx := r.Context()

	call := &Call{
		s:      s,
		r:      r,
		oid:    oid,
		method: method,
	}

	defer r.Body.Close()

	if iface, ok := s.objects[oid]; ok {
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
