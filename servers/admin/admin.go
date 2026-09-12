package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/admin/admin_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/httpingress/httpingress_v1alpha"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/mapx"
)

const (
	adminEndpoint    = "/.well-known/miren/admin"
	adminCallTimeout = 30 * 1000 // timeout in milliseconds
)

// jsonrpc2Request is a JSON-RPC 2.0 request (conservative, not using jsonrpc3 extensions)
type jsonrpc2Request struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int    `json:"id"`
}

// Admin endpoints speak JSON over HTTP. Keeping the response envelope here
// avoids coupling those fields to a library's unrelated QUIC transports.
type jsonrpc2Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpc2Error  `json:"error,omitempty"`
	ID      any             `json:"id"`
}

type jsonrpc2Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// InternalHTTPRequester is an interface for making internal HTTP requests to app sandboxes.
// This is implemented by httpingress.Server.
type InternalHTTPRequester interface {
	DoRequest(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error)
}

type Server struct {
	Log       *slog.Logger
	EC        *entityserver.Client
	ingress   InternalHTTPRequester
	logWriter observability.LogWriter
}

var _ admin_v1alpha.Admin = (*Server)(nil)

func NewServer(
	log *slog.Logger,
	ec *entityserver.Client,
	ingress InternalHTTPRequester,
	logWriter observability.LogWriter,
) *Server {
	return &Server{
		Log:       log.With("module", "admin"),
		EC:        ec,
		ingress:   ingress,
		logWriter: logWriter,
	}
}

func (s *Server) Invoke(ctx context.Context, state *admin_v1alpha.AdminInvoke) error {
	startTime := time.Now()
	args := state.Args()
	results := state.Results()

	// Validate required fields
	if !args.HasApp() || args.App() == "" {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError("app is required")
		results.SetResult(result)
		return nil
	}
	if !args.HasMethod() || args.Method() == "" {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError("method is required")
		results.SetResult(result)
		return nil
	}

	appName := args.App()
	methodName := args.Method()

	// Look up app and get active version
	app, appVersion, err := s.getAppAndVersion(ctx, appName)
	if err != nil {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(err.Error())
		results.SetResult(result)
		return nil
	}

	// Get admin token from version
	if appVersion.AdminToken == "" {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError("app does not have admin token configured (redeploy may be required)")
		results.SetResult(result)
		return nil
	}

	s.Log.Info("calling admin method",
		"app", appName,
		"method", methodName,
		"app_id", app.ID,
		"version", appVersion.Version)

	// Parse params to ensure valid JSON before sending
	var params any
	if args.HasParams() && args.Params() != "" {
		if err := json.Unmarshal([]byte(args.Params()), &params); err != nil {
			result := &admin_v1alpha.AdminCallResult{}
			result.SetError(fmt.Sprintf("invalid params JSON: %v", err))
			results.SetResult(result)
			return nil
		}
	}

	// Build JSON-RPC 2.0 request body
	// (conservative because the server might be normal jsonrpc 2 server)
	rpcReq := jsonrpc2Request{
		JSONRPC: "2.0",
		Method:  methodName,
		Params:  params,
		ID:      1,
	}
	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(fmt.Sprintf("failed to marshal request: %v", err))
		results.SetResult(result)
		return nil
	}

	// Build internal HTTP request
	httpReq := &httpingress_v1alpha.InternalHttpRequest{}
	httpReq.SetAppId(app.ID.String())
	httpReq.SetMethod("POST")
	httpReq.SetPath(adminEndpoint)
	httpReq.SetService("web")
	httpReq.SetTimeoutMs(adminCallTimeout)
	httpReq.SetBody(&reqBody)

	// Set headers
	headers := []*httpingress_v1alpha.HttpHeader{
		newHeader("Content-Type", "application/json"),
		newHeader("Authorization", "Bearer "+appVersion.AdminToken),
	}
	httpReq.SetHeaders(headers)

	// Make the request via httpingress
	httpResp, err := s.ingress.DoRequest(ctx, httpReq)
	if err != nil {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(fmt.Sprintf("failed to call admin endpoint: %v", err))
		results.SetResult(result)
		return nil
	}

	// Check for httpingress-level errors
	if httpResp.HasError() && httpResp.Error() != "" {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(httpResp.Error())
		results.SetResult(result)
		return nil
	}

	// Check HTTP status code
	if httpResp.StatusCode() != 200 {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(fmt.Sprintf("admin endpoint returned status %d", httpResp.StatusCode()))
		results.SetResult(result)
		return nil
	}

	// Parse JSON-RPC response
	var rpcResp jsonrpc2Response
	respBody := httpResp.Body()
	if respBody == nil {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError("empty response from admin endpoint")
		results.SetResult(result)
		return nil
	}
	if err := json.Unmarshal(*respBody, &rpcResp); err != nil {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(fmt.Sprintf("failed to parse response: %v", err))
		results.SetResult(result)
		return nil
	}

	// Check for JSON-RPC error
	if rpcResp.Error != nil {
		result := &admin_v1alpha.AdminCallResult{}
		result.SetError(rpcResp.Error.Message)
		result.SetErrorCode(int32(rpcResp.Error.Code))
		results.SetResult(result)
		return nil
	}

	// Encode result as JSON string
	result := &admin_v1alpha.AdminCallResult{}
	resultJSON, err := json.Marshal(rpcResp.Result)
	if err != nil {
		result.SetError(fmt.Sprintf("failed to encode result: %v", err))
	} else {
		result.SetResult(string(resultJSON))
	}
	results.SetResult(result)

	// Log the admin call
	paramsLen := 0
	if args.HasParams() {
		paramsLen = len(args.Params())
	}
	s.logAdminCall(app.ID.String(), appName, methodName, paramsLen, startTime, "")

	return nil
}

// logAdminCall logs an admin function call to the app's log stream
func (s *Server) logAdminCall(appEntityID, appName, method string, paramsSize int, startTime time.Time, errorMsg string) {
	if s.logWriter == nil {
		return
	}

	duration := time.Since(startTime)

	var logMsg string
	if errorMsg != "" {
		logMsg = fmt.Sprintf("rpc=%s params=%d error=\"%s\" duration_ms=%d",
			method, paramsSize, errorMsg, duration.Milliseconds())
	} else {
		logMsg = fmt.Sprintf("rpc=%s params=%d status=ok duration_ms=%d",
			method, paramsSize, duration.Milliseconds())
	}

	err := s.logWriter.WriteEntry(appEntityID, observability.LogEntry{
		Timestamp: time.Now(),
		Stream:    observability.UserOOB,
		Body:      logMsg,
		Attributes: map[string]string{
			"source": "admin",
			"method": method,
		},
	})
	if err != nil {
		s.Log.Error("failed to write admin call log entry", "error", err, "app", appName)
	}
}

func (s *Server) ListMethods(ctx context.Context, state *admin_v1alpha.AdminListMethods) error {
	startTime := time.Now()
	args := state.Args()
	results := state.Results()

	if !args.HasApp() || args.App() == "" {
		results.SetError("app is required")
		return nil
	}

	appName := args.App()

	methods, appEntityID, err := s.fetchMethods(ctx, appName)
	if err != nil {
		results.SetError(err.Error())
		return nil
	}

	results.SetMethods(methods)
	s.logAdminCall(appEntityID, appName, "$methods", 0, startTime, "")

	return nil
}

func (s *Server) DescribeMethods(ctx context.Context, state *admin_v1alpha.AdminDescribeMethods) error {
	startTime := time.Now()
	args := state.Args()
	results := state.Results()

	if !args.HasApp() || args.App() == "" {
		results.SetError("app is required")
		return nil
	}

	if !args.HasMethods() || len(args.Methods()) == 0 {
		results.SetError("at least one method name is required")
		return nil
	}

	appName := args.App()

	allMethods, appEntityID, err := s.fetchMethods(ctx, appName)
	if err != nil {
		results.SetError(err.Error())
		return nil
	}

	// Build lookup set of requested names
	wanted := make(map[string]struct{}, len(args.Methods()))
	for _, name := range args.Methods() {
		wanted[name] = struct{}{}
	}

	// Filter to only requested methods
	var matched []*admin_v1alpha.AdminMethod
	for _, m := range allMethods {
		if _, ok := wanted[m.Name()]; ok {
			matched = append(matched, m)
		}
	}

	results.SetMethods(matched)
	s.logAdminCall(appEntityID, appName, "$methods", 0, startTime, "")

	return nil
}

// fetchMethods calls $methods on the app's admin endpoint and returns the parsed methods.
func (s *Server) fetchMethods(ctx context.Context, appName string) ([]*admin_v1alpha.AdminMethod, string, error) {
	app, appVersion, err := s.getAppAndVersion(ctx, appName)
	if err != nil {
		return nil, "", err
	}

	if appVersion.AdminToken == "" {
		return nil, "", fmt.Errorf("app does not have admin token configured")
	}

	rpcReq := jsonrpc2Request{
		JSONRPC: "2.0",
		Method:  "$methods",
		ID:      1,
	}
	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %v", err)
	}

	httpReq := &httpingress_v1alpha.InternalHttpRequest{}
	httpReq.SetAppId(app.ID.String())
	httpReq.SetMethod("POST")
	httpReq.SetPath(adminEndpoint)
	httpReq.SetService("web")
	httpReq.SetTimeoutMs(adminCallTimeout)
	httpReq.SetBody(&reqBody)

	headers := []*httpingress_v1alpha.HttpHeader{
		newHeader("Content-Type", "application/json"),
		newHeader("Authorization", "Bearer "+appVersion.AdminToken),
	}
	httpReq.SetHeaders(headers)

	httpResp, err := s.ingress.DoRequest(ctx, httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to discover methods: %v", err)
	}

	if httpResp.HasError() && httpResp.Error() != "" {
		return nil, "", fmt.Errorf("%s", httpResp.Error())
	}

	if httpResp.StatusCode() != 200 {
		return nil, "", fmt.Errorf("admin endpoint returned status %d", httpResp.StatusCode())
	}

	var rpcResp jsonrpc2Response
	respBody := httpResp.Body()
	if respBody == nil {
		return nil, "", fmt.Errorf("empty response from admin endpoint")
	}
	if err := json.Unmarshal(*respBody, &rpcResp); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %v", err)
	}

	if rpcResp.Error != nil {
		return nil, "", fmt.Errorf("method discovery failed: %s", rpcResp.Error.Message)
	}

	type methodInfo struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Category    string `json:"category,omitempty"`
		Params      any    `json:"params,omitempty"`
	}

	resultJSON := rpcResp.Result

	var methodInfos []methodInfo

	// Try decoding as array first (standard format)
	if err := json.Unmarshal(resultJSON, &methodInfos); err != nil {
		s.Log.Info("failed to unmarshal $methods result", "error", "value was not an array of objects", "app", appName, "result", string(resultJSON))
		return nil, "", fmt.Errorf("method discovery not supported by app (invalid response)")
	}

	var methods []*admin_v1alpha.AdminMethod
	for _, m := range methodInfos {
		if m.Name == "$methods" || m.Name == "$type" {
			continue
		}

		method := &admin_v1alpha.AdminMethod{}
		method.SetName(m.Name)
		if m.Description != "" {
			method.SetDescription(m.Description)
		}
		if m.Category != "" {
			method.SetCategory(m.Category)
		}

		// A present "params" key (even if empty) means the method explicitly
		// declares its parameter list. A missing key leaves Params unset on the
		// wire, so the client can distinguish "method takes no parameters" from
		// "this method does not advertise its parameters".
		params := []*admin_v1alpha.AdminMethodParam{}
		switch p := m.Params.(type) {
		case map[string]any:
			for name, typeVal := range mapx.StableOrder(p) {
				param := &admin_v1alpha.AdminMethodParam{}
				param.SetName(name)
				if typeStr, ok := typeVal.(string); ok {
					param.SetParamType(typeStr)
				}
				params = append(params, param)
			}
			method.SetParams(params)
		case []any:
			for i, typeVal := range p {
				param := &admin_v1alpha.AdminMethodParam{}
				param.SetName(fmt.Sprintf("arg%d", i))
				if typeStr, ok := typeVal.(string); ok {
					param.SetParamType(typeStr)
				}
				params = append(params, param)
			}
			method.SetParams(params)
		}

		methods = append(methods, method)
	}

	return methods, app.ID.String(), nil
}

// getAppAndVersion looks up an app by name and returns it along with its active version
func (s *Server) getAppAndVersion(ctx context.Context, appName string) (*core_v1alpha.App, *core_v1alpha.AppVersion, error) {
	var app core_v1alpha.App
	err := s.EC.Get(ctx, appName, &app)
	if err != nil {
		return nil, nil, fmt.Errorf("app not found: %s", appName)
	}

	if app.ActiveVersion == "" {
		return nil, nil, fmt.Errorf("app has no active version")
	}

	// Get the active version
	var ver core_v1alpha.AppVersion
	err = s.EC.GetById(ctx, app.ActiveVersion, &ver)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get app version: %w", err)
	}

	return &app, &ver, nil
}

// newHeader creates a new HttpHeader with key and value
func newHeader(key, value string) *httpingress_v1alpha.HttpHeader {
	h := &httpingress_v1alpha.HttpHeader{}
	h.SetKey(key)
	h.SetValue(value)
	return h
}
