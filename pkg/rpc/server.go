package rpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"github.com/quic-go/webtransport-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"miren.dev/runtime/pkg/cond"
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

	// lastContact is unix nanos, held atomically: concurrent calls against the
	// same capability all touch it, and they are not otherwise serialized.
	lastContact atomic.Int64

	pub ed25519.PublicKey
}

func (h *heldCapability) touch() {
	h.lastContact.Store(time.Now().UnixNano())
}

func (h *heldCapability) LastContact() time.Time {
	return time.Unix(0, h.lastContact.Load())
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
	Handler       func(ctx context.Context, call Call) error
	// Public marks this method as accessible without TLS client certificate authentication.
	// The RPC layer will reject unauthenticated calls to non-public methods automatically.
	Public bool
	// Params lists the method's parameter names in schema order. It powers
	// parameter-level capability detection: a client can ask whether a server
	// understands a specific parameter (e.g. one added after the method first
	// shipped) instead of only whether the method exists. See HasMethodParam.
	Params []string
	// HTTP describes how to expose this method over a plain HTTP/JSON REST API.
	// It is nil unless the method carries an http: annotation in the IDL. The
	// REST gateway (RegisterREST) only mounts routes for methods with a binding.
	HTTP *HTTPBinding
}

// HTTPBinding maps an RPC method onto an HTTP route for the REST gateway. It is
// populated by generated AdaptXxx code from the method's IDL http: annotation.
type HTTPBinding struct {
	// Verb is the HTTP method (GET, POST, PUT, DELETE, PATCH).
	Verb string
	// Path is the fully-resolved route template, including any interface
	// prefix, using Go 1.22 ServeMux wildcards (e.g. /api/v1/apps/{app}/config).
	Path string
	// Body designates where the request body maps: "*" binds the whole JSON
	// body onto the args, "" means no body (params come from path/query).
	Body string
	// PathParams lists the wildcard names embedded in Path, in order.
	PathParams []string
	// Query lists the parameters bound from the URL query string, with the type
	// info needed to coerce their string values into typed JSON. It is only
	// populated for bodyless bindings (Body == ""); when a body is present the
	// non-path params ride in the JSON body instead.
	Query []HTTPParam
}

// HTTPParam describes a single query-bound parameter for the REST gateway.
type HTTPParam struct {
	// Name is the parameter (and query key) name.
	Name string
	// Kind selects how the raw string value is coerced into JSON: one of
	// "string", "bool", "int", "uint", "float", or "timestamp".
	Kind string
}

type HasRestoreState interface {
	RestoreState(iface any) (any, error)
}

type Interface struct {
	name    string
	methods map[string]Method
	closer  io.Closer

	value         any
	aroundContext func(ctx context.Context, call Call) (context.Context, func())

	forbidRestore bool
	restoreState  HasRestoreState
	constructor   HasReconstructFromState
}

func (i *Interface) Value() any {
	return i.value
}

// Name returns the schema name of the interface (e.g. "Crud").
func (i *Interface) Name() string {
	return i.name
}

// Methods returns the interface's methods. The order is unspecified. It exists
// so external packages (notably the REST gateway) can enumerate methods without
// access to the private method map.
func (i *Interface) Methods() []Method {
	methods := make([]Method, 0, len(i.methods))
	for _, m := range i.methods {
		methods = append(methods, m)
	}
	return methods
}

func (i *Interface) SetAroundContext(fn func(ctx context.Context, call Call) (context.Context, func())) {
	i.aroundContext = fn
}

func NewInterface(methods []Method, obj any) *Interface {
	m := make(map[string]Method)
	for _, mm := range methods {
		m[mm.Name] = mm
	}

	i := &Interface{
		name:    methods[0].InterfaceName,
		value:   obj,
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
		category: category,
		pub:      pub,
	}
	hc.touch()

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
		contactAddr = s.state.contactAddr()
	}

	capa := &Capability{
		OID:     oid,
		User:    pub,
		Issuer:  s.state.pubkey,
		Address: contactAddr,
	}

	hc := &heldCapability{
		heldInterface: cur.heldInterface,
		pub:           pub,
	}
	hc.touch()

	hc.refs.Add(1)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.objects[oid] = hc

	return capa
}

func (s *Server) setupMux() {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /_rpc/call/{oid}/{method}", s.handleCalls)
	// CONNECT upgrades to WebTransport over HTTP/3. TCP clients use the message
	// transport, which multiplexes every operation over one session and never
	// reaches this mux.
	mux.HandleFunc("CONNECT /_rpc/callstream/{oid}/{method}", s.startCallStream)
	mux.HandleFunc("POST /_rpc/lookup/{name}", s.lookup)
	mux.HandleFunc("GET /_rpc/methods/{oid}", s.listMethods)
	mux.HandleFunc("POST /_rpc/reresolve", s.reresolve)
	mux.HandleFunc("POST /_rpc/reexport/{oid}", s.reexport)
	mux.HandleFunc("POST /_rpc/ref/{oid}", s.refCapa)
	mux.HandleFunc("POST /_rpc/deref/{oid}", s.derefCapa)
	mux.HandleFunc("POST /_rpc/identify", s.clientIdentify)
	mux.HandleFunc("GET /api/v1/debug-auth", s.handleDebugAuth)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	s.mux = mux
}

// mountHTTPHandlers adds caller-supplied routes to the mux. The RPC prefix is
// reserved: a handler mounted under it would shadow method dispatch, and the
// resulting failure (methods quietly answered by the wrong handler) is far
// harder to diagnose than a refusal at startup.
func (s *Server) mountHTTPHandlers(mounts []httpHandlerMount) error {
	for _, m := range mounts {
		if m.handler == nil {
			return fmt.Errorf("http handler for %q is nil", m.pattern)
		}
		if strings.Contains(m.pattern, rpcPathPrefix) {
			return fmt.Errorf("http handler pattern %q is reserved for RPC", m.pattern)
		}
		s.mux.Handle(m.pattern, m.handler)
	}

	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// If no authenticator configured, allow all requests
	if s.state.authenticator == nil {
		s.mux.ServeHTTP(w, r)
		return
	}

	// Try to authenticate the request
	identity, err := s.state.authenticator.Authenticate(ctx, CredentialsFromRequest(r))
	if err != nil {
		s.state.log.Warn("authentication failed", "error", err, "path", r.URL.Path)
		writeAuthFailure(w, err)
		return
	}

	// Store identity in context if authenticated
	if identity != nil {
		ctx = ContextWithIdentity(ctx, identity)
		r = r.WithContext(ctx)

		// Audit successful cert-method auth. See logCertAuth for why this only
		// ever records legitimate (internal) mTLS and never an attacker.
		if identity.Method == AuthMethodCert {
			logCertAuth(ctx, s.state.audit(), s.state.certAuth, r)
		}
	}

	// For RPC paths, let the method-level check in handleCalls decide based on public flag
	// For non-RPC paths, require authentication
	if identity == nil && !isRPCPath(r.URL.Path) {
		s.state.log.Warn("request requires authentication", "path", r.URL.Path)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// rpcPathPrefix is the path namespace reserved for RPC dispatch.
const rpcPathPrefix = "/_rpc/"

// writeAuthFailure responds 401 to a failed authentication, explaining why only
// when the authenticator opted in by implementing DisclosableAuthError. Every
// other failure stays a bare 401: the caller is unauthenticated, and a chatty
// rejection is a fine oracle for probing how a cluster is configured.
func writeAuthFailure(w http.ResponseWriter, err error) {
	if disclosable, ok := errors.AsType[DisclosableAuthError](err); ok {
		w.Header().Add("rpc-status", disclosable.AuthErrorCode())
		w.Header().Add("rpc-error", disclosable.Error())
	}
	http.Error(w, "authentication failed", http.StatusUnauthorized)
}

// isRPCPath returns true if the path is an RPC endpoint
func isRPCPath(path string) bool {
	return strings.HasPrefix(path, rpcPathPrefix)
}

type identifyResponse struct {
	Ok       bool   `json:"ok,omitempty"`
	Error    string `json:"error,omitempty"`
	Address  string `json:"address,omitempty"`
	Identity string `json:"identity,omitempty"`

	// MinVersion and MaxVersion advertise the protocol versions this peer
	// accepts, so a client can adapt instead of discovering the mismatch one
	// rejected stream at a time. Set only on message transports. See PROTOCOL.md.
	MinVersion uint `json:"min_version,omitempty"`
	MaxVersion uint `json:"max_version,omitempty"`
}

func (s *Server) checkIdentity(r *http.Request) (string, bool) {
	ts := r.Header.Get("rpc-timestamp")

	if err := freshTimestamp(ts); err != nil {
		s.state.log.Warn("identity timestamp rejected", "error", err)
		return "", false
	}

	sign := r.Header.Get("rpc-signature")
	if sign == "" {
		s.state.log.Warn("No signature provided")
		return "", false
	}

	key, err := base58.Decode(r.Header.Get("rpc-public-key"))
	if err != nil {
		return "", false
	}

	pub := ed25519.PublicKey(key)

	if err := verifyString(pub, httpCanonical(r.Method, r.URL.Path, ts), sign); err != nil {
		s.state.log.Warn("Failed to verify identity signature", "error", err)
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

// DebugAuthResponse represents the response from the debug-auth endpoint
type DebugAuthResponse struct {
	Success       bool              `json:"success"`
	ServerVersion string            `json:"server_version,omitempty"`
	AuthMethod    string            `json:"auth_method,omitempty"`
	Identity      string            `json:"identity,omitempty"`
	UserInfo      map[string]string `json:"user_info,omitempty"`
	Message       string            `json:"message,omitempty"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleDebugAuth(w http.ResponseWriter, r *http.Request) {
	resp := DebugAuthResponse{
		Success:       true,
		ServerVersion: "1.0.0", // You can replace this with an actual version
		UserInfo:      make(map[string]string),
	}

	// Check authentication using the new Authenticate interface
	if s.state.authenticator != nil {
		identity, err := s.state.authenticator.Authenticate(r.Context(), CredentialsFromRequest(r))
		if err != nil {
			resp.Success = false
			resp.Message = fmt.Sprintf("Authentication failed: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
		} else if identity != nil {
			resp.AuthMethod = string(identity.Method)
			resp.Identity = identity.Subject
			resp.UserInfo["authenticated"] = "true"
			resp.UserInfo["subject"] = identity.Subject
			resp.UserInfo["method"] = string(identity.Method)
			if len(identity.Groups) > 0 {
				resp.UserInfo["groups"] = fmt.Sprintf("%v", identity.Groups)
			}
		} else {
			resp.Success = false
			resp.Message = "Authentication required"
			w.WriteHeader(http.StatusUnauthorized)
		}
	} else {
		resp.AuthMethod = "none"
		resp.Message = "No authentication configured"
	}

	// Add connection information
	resp.UserInfo["remote_addr"] = r.RemoteAddr
	resp.UserInfo["request_uri"] = r.RequestURI

	// Write JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
		ca = s.state.contactAddr()
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

type methodsResponse struct {
	Methods []string `json:"methods,omitempty" cbor:"methods,omitempty"`
	Error   string   `json:"error,omitempty" cbor:"error,omitempty"`
	// Params maps each method name to its parameter names. Added after Methods,
	// so older servers simply omit it and newer clients read a nil map as "this
	// server can't report parameters" rather than "no parameters."
	Params map[string][]string `json:"params,omitempty" cbor:"params,omitempty"`
}

// newMethodsResponse builds an introspection response from an interface's method
// set. Shared by the network discovery endpoint and the in-process transport so
// the two can't report different things.
func newMethodsResponse(m map[string]Method) methodsResponse {
	methods := make([]string, 0, len(m))
	params := make(map[string][]string, len(m))
	for name, mm := range m {
		methods = append(methods, name)
		if len(mm.Params) > 0 {
			params[name] = mm.Params
		}
	}
	sort.Strings(methods)
	return methodsResponse{Methods: methods, Params: params}
}

func (s *Server) listMethods(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	s.mu.Lock()
	hc, ok := s.objects[oid]
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/cbor")
	w.WriteHeader(http.StatusOK)

	if !ok {
		cbor.NewEncoder(w).Encode(methodsResponse{
			Error: "unknown capability: " + string(oid),
		})
		return
	}

	cbor.NewEncoder(w).Encode(newMethodsResponse(hc.methods))
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
		ca = s.state.contactAddr()
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

//nolint:errcheck
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
		category = rs.Category

		s.mu.Lock()
		res, ok := s.resolvers[rs.Category]
		s.mu.Unlock()

		if !ok {
			// No resolver means iface stays nil, and assignCapability below
			// dereferences it. Fail the lookup instead of panicking.
			cbor.NewEncoder(w).Encode(lookupResponse{Error: "no resolver for category: " + rs.Category})
			return
		}

		iface, err = res.ReconstructFromState(&rs)
		if err != nil {
			cbor.NewEncoder(w).Encode(lookupResponse{Error: "failed to resolve: " + err.Error()})
			return
		}

		if iface == nil {
			cbor.NewEncoder(w).Encode(lookupResponse{Error: "unable to restore capability"})
			return
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
		ca = s.state.contactAddr()
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
	Status   string `json:"status,omitempty" cbor:"status,omitempty"`
	Error    string `json:"error,omitempty" cbor:"error,omitempty"`
	Category string `json:"category,omitempty" cbor:"category,omitempty"`
	Code     string `json:"code,omitempty" cbor:"code,omitempty"`
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
		logAuthReject(s.state.audit(), r, oid, "no timestamp provided")
		http.Error(w, "no timestamp provided", http.StatusForbidden)
		return nil, false
	}

	if err := freshTimestamp(ts); err != nil {
		logAuthReject(s.state.audit(), r, oid, err.Error())
		http.Error(w, err.Error(), http.StatusForbidden)
		return nil, false
	}

	sign := r.Header.Get("rpc-signature")
	if sign == "" {
		logAuthReject(s.state.audit(), r, oid, "no signature provided")
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

	if err := verifyString(capa.pub, httpCanonical(r.Method, r.URL.Path, ts), sign); err != nil {
		logAuthReject(s.state.audit(), r, oid, err.Error())
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

	Category string `json:"category" cbor:"category"`
	Code     string `json:"code" cbor:"code"`
	Error    string `json:"error" cbor:"error"`
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

// acceptSession upgrades an incoming callstream request to a multiplexed
// session. The HTTP mux is served only over HTTP/3, where WebTransport provides
// QUIC sub-streams natively; TCP clients use the message transport instead,
// which multiplexes every operation over one session and never reaches here.
func (s *Server) acceptSession(w http.ResponseWriter, r *http.Request) (rpcSession, error) {
	if r.ProtoMajor != 3 {
		return nil, fmt.Errorf("callstream requires HTTP/3; use the message transport over TCP")
	}

	// webtransport.Upgrade writes the 200 status itself.
	sess, err := s.ws.Upgrade(w, r)
	if err != nil {
		return nil, err
	}
	return &wtSession{sess: sess}, nil
}

func (s *Server) startCallStream(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	method := r.PathValue("method")

	w.Header().Set("Trailer", "rpc-status, rpc-error, rpc-error-category, rpc-error-code")

	user, ok := s.authRequest(r, w, oid)
	if !ok {
		return
	}

	ctx := r.Context()

	s.mu.Lock()
	iface, ok := s.objects[oid]
	s.mu.Unlock()
	if !ok {
		w.Header().Add("rpc-status", "unknown-capability")
		w.Header().Add("rpc-error", "unknown object: "+string(oid))
		w.WriteHeader(http.StatusNotFound)
		return
	}

	iface.touch()

	mm := iface.methods[method]
	if mm.Handler == nil {
		w.Header().Add("rpc-status", "unknown")
		w.Header().Add("rpc-error", "unknown method: "+method)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Enforce authentication for non-public methods
	if !mm.Public {
		identity := IdentityFromContext(ctx)
		if identity == nil {
			logAccess(ctx, s.state.audit(), r, mm, "unauthorized")
			w.Header().Add("rpc-status", "unauthorized")
			w.Header().Add("rpc-error", "authentication required")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Check authorization if an authorizer is configured
		if s.state.authorizer != nil {
			resource := strings.ToLower(mm.InterfaceName)
			action := strings.ToLower(mm.Name)
			if err := s.state.authorizer.Authorize(ctx, identity, resource, action); err != nil {
				logAccess(ctx, s.state.audit(), r, mm, "forbidden", "error", err)
				w.Header().Add("rpc-status", "forbidden")
				w.Header().Add("rpc-error", err.Error())
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		logAccess(ctx, s.state.audit(), r, mm, "ok")
	}

	ctx = Propagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

	tracer := Tracer()

	ctx, span := tracer.Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)

	defer span.End()

	span.SetAttributes(attribute.String("oid", string(oid)))

	sess, err := s.acceptSession(w, r)
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

	call := &NetworkCall{
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
		ctx = ContextWithConnectionInfo(ctx, &CurrentConnectionInfo{
			PeerSubject:     r.TLS.PeerCertificates[0].Subject.String(),
			PeerCertificate: r.TLS.PeerCertificates[0],
		})
	}

	s.runCallStream(ctx, &cs, mm, call)
}

// runCallStream invokes a streaming handler and emits its result, error, or
// panic over the control stream. It is transport-neutral: both the HTTP
// callstream handler and the message-transport router use it.
func (s *Server) runCallStream(ctx context.Context, cs *controlStream, mm Method, call *NetworkCall) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			s.state.log.Error("panic in streaming RPC handler",
				"panic", r,
				"method", call.method,
				"stack", string(debug.Stack()))
			cs.NoReply(streamRequest{Kind: "panic", Error: fmt.Sprint(r)}, nil)
		}
	}()

	err := cond.Wrap(mm.Handler(ctx, call))
	if err != nil {
		msg, category, code := errorFields(err)
		cs.NoReply(streamRequest{Kind: "error", Error: msg, Category: category, Code: code}, nil)
		return
	}

	res := call.results
	if res == nil {
		res = struct{}{}
	}
	cs.NoReply(streamRequest{Kind: "result"}, res)
}

// errorFields extracts the wire error fields from a handler error, honoring the
// optional ErrorMessage/ErrorCategory/ErrorCode interfaces.
func errorFields(err error) (msg, category, code string) {
	if emsg, ok := err.(ErrorMessage); ok {
		msg = emsg.ErrorMessage()
	} else {
		msg = err.Error()
	}
	if ecat, ok := err.(ErrorCategory); ok {
		category = ecat.ErrorCategory()
	}
	if ecode, ok := err.(ErrorCode); ok {
		code = ecode.ErrorCode()
	}
	return
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	oid := OID(r.PathValue("oid"))

	w.Header().Set("Trailer", "rpc-status, rpc-error, rpc-error-category, rpc-error-code")

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
			w.Header().Add("rpc-status", "unknown")
			w.Header().Add("rpc-error", "unknown method: "+method)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Enforce authentication for non-public methods
		if !mm.Public {
			identity := IdentityFromContext(ctx)
			if identity == nil {
				logAccess(ctx, s.state.audit(), r, mm, "unauthorized")
				w.Header().Add("rpc-status", "unauthorized")
				w.Header().Add("rpc-error", "authentication required")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Check authorization if an authorizer is configured
			if s.state.authorizer != nil {
				resource := strings.ToLower(mm.InterfaceName)
				action := strings.ToLower(mm.Name)
				if err := s.state.authorizer.Authorize(ctx, identity, resource, action); err != nil {
					logAccess(ctx, s.state.audit(), r, mm, "forbidden", "error", err)
					w.Header().Add("rpc-status", "forbidden")
					w.Header().Add("rpc-error", err.Error())
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}

			logAccess(ctx, s.state.audit(), r, mm, "ok")
		}

		w.WriteHeader(http.StatusOK)

		defer func() {
			if r := recover(); r != nil {
				s.state.log.Error("panic in RPC handler",
					"panic", r,
					"method", method,
					"stack", string(debug.Stack()))
				w.Header().Add("rpc-status", "panic")
				w.Header().Add("rpc-error", fmt.Sprint(r))
			}
		}()

		ctx = Propagator().Extract(ctx, propagation.HeaderCarrier(r.Header))

		tracer := Tracer()

		ctx, span := tracer.Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)

		defer span.End()

		span.SetAttributes(attribute.String("oid", string(oid)))

		call := &NetworkCall{
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

		if iface.aroundContext != nil {
			var cancel func()
			ctx, cancel = iface.aroundContext(ctx, call)
			defer cancel()
		}

		err := mm.Handler(ctx, call)
		if err != nil {
			w.Header().Add("rpc-status", "error")

			if emsg, ok := err.(ErrorMessage); ok {
				w.Header().Add("rpc-error", emsg.ErrorMessage())
			} else {
				w.Header().Add("rpc-error", err.Error())
			}

			if ecat, ok := err.(ErrorCategory); ok {
				w.Header().Add("rpc-error-category", ecat.ErrorCategory())
			}

			if ecode, ok := err.(ErrorCode); ok {
				w.Header().Add("rpc-error-code", ecode.ErrorCode())
			}

			s.handleError(w, r, err)
			return
		}

		cbor.NewEncoder(w).Encode(call.results)
		w.Header().Add("rpc-status", "ok")
	} else {
		w.Header().Add("rpc-status", "unknown-capability")
		w.Header().Add("rpc-error", "unknown object: "+string(oid))
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Server) handleError(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
