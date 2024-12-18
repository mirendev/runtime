package rpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/quic-go/quic-go/http3"
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
	objects map[OID]*Interface
}

func NewServer() *Server {
	return &Server{
		objects: make(map[OID]*Interface),
	}
}

type Method struct {
	Name    string
	Index   int
	Handler func(ctx context.Context, call *Call) error
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

func (s *Server) Serve(addr string) error {
	// If there is only one object, make it the bootstrap object
	if len(s.objects) == 1 {
		for _, iface := range s.objects {
			s.objects[BootstrapOID] = iface
		}
	}

	cert, err := generateSelfSignedCert()
	if err != nil {
		return err
	}

	serv := &http3.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(s.handleCalls),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{http3.NextProtoH3},
		},
		Logger: slog.Default(),
	}

	return serv.ListenAndServe()
}

const BootstrapOID = "!bootstrap"

func (s *Server) AssignOID(i *Interface) OID {
	oid := OID("blah")

	s.ExposeValue(oid, i)

	return oid
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
				w.Header().Add("rpc-status", "panic")
				w.Header().Add("rpc-error", fmt.Sprint(r))
				panic(r)
			}
		}()

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
