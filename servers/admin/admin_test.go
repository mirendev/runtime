package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/admin/admin_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/httpingress/httpingress_v1alpha"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// mockIngress implements InternalHTTPRequester for testing.
type mockIngress struct {
	doRequest func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error)
}

func (m *mockIngress) DoRequest(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
	return m.doRequest(ctx, req)
}

// jsonrpcResponse builds a JSON-RPC 2.0 success response body.
func jsonrpcResponse(result any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"result":  result,
		"id":      1,
	})
	return b
}

// jsonrpcErrorResponse builds a JSON-RPC 2.0 error response body.
func jsonrpcErrorResponse(code int, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
		"id": 1,
	})
	return b
}

// httpResponse builds an InternalHttpResponse with a status code and body.
func httpResponse(status int32, body []byte) *httpingress_v1alpha.InternalHttpResponse {
	resp := &httpingress_v1alpha.InternalHttpResponse{}
	resp.SetStatusCode(status)
	resp.SetBody(&body)
	return resp
}

// httpErrorResponse builds an InternalHttpResponse with an ingress-level error.
func httpErrorResponse(errMsg string) *httpingress_v1alpha.InternalHttpResponse {
	resp := &httpingress_v1alpha.InternalHttpResponse{}
	resp.SetError(errMsg)
	return resp
}

type testEnv struct {
	ctx     context.Context
	ec      *entityserver.Client
	inmem   *testutils.InMemEntityServer
	ingress *mockIngress
	client  *admin_v1alpha.AdminClient
	logs    *recordingLogWriter
}

type recordingLogWriter struct {
	mu      sync.Mutex
	entries []observability.LogEntry
}

func (w *recordingLogWriter) WriteEntry(_ string, le observability.LogEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, le)
	return nil
}

func setup(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	log := testutils.TestLogger(t)
	ec := entityserver.NewClient(log, inmem.EAC)

	ing := &mockIngress{}

	logs := &recordingLogWriter{}
	server := NewServer(log, ec, ing, logs)

	client := admin_v1alpha.NewAdminClient(
		rpc.LocalClient(admin_v1alpha.AdaptAdmin(server)),
	)

	return &testEnv{
		ctx:     ctx,
		ec:      ec,
		inmem:   inmem,
		ingress: ing,
		client:  client,
		logs:    logs,
	}
}

// createApp creates an app with an active version that has the given admin token.
func (e *testEnv) createApp(t *testing.T, name, adminToken string) {
	t.Helper()

	app := &core_v1alpha.App{}
	appID, err := e.inmem.Client.Create(e.ctx, name, app)
	require.NoError(t, err, "create app")
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		Version:    "v1",
		AdminToken: adminToken,
	}
	verID, err := e.inmem.Client.Create(e.ctx, name+"-v1", ver)
	require.NoError(t, err, "create version")

	app.ActiveVersion = verID
	require.NoError(t, e.inmem.Client.Update(e.ctx, app), "update app")
}

// --- Invoke tests ---

func TestInvoke_MissingApp(t *testing.T) {
	env := setup(t)

	result, err := env.client.Invoke(env.ctx, "", "some-method", "{}")
	require.NoError(t, err)
	require.True(t, result.HasResult())
	assert.Equal(t, "app is required", result.Result().Error())
}

func TestInvoke_MissingMethod(t *testing.T) {
	env := setup(t)

	result, err := env.client.Invoke(env.ctx, "myapp", "", "{}")
	require.NoError(t, err)
	assert.Equal(t, "method is required", result.Result().Error())
}

func TestInvoke_AppNotFound(t *testing.T) {
	env := setup(t)

	result, err := env.client.Invoke(env.ctx, "nonexistent", "test", "{}")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result().Error())
}

func TestInvoke_NoAdminToken(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "")

	result, err := env.client.Invoke(env.ctx, "myapp", "test", "{}")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result().Error())
}

func TestInvoke_InvalidParamsJSON(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	result, err := env.client.Invoke(env.ctx, "myapp", "test", "{bad json")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result().Error())
}

func TestInvoke_Success(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		assert.Equal(t, "/.well-known/miren/admin", req.Path())
		assert.Equal(t, "POST", req.Method())

		// Check authorization header
		for _, h := range req.Headers() {
			if h.Key() == "Authorization" {
				assert.Equal(t, "Bearer tok123", h.Value())
			}
		}

		// Verify the JSON-RPC request body
		var rpcReq map[string]any
		require.NoError(t, json.Unmarshal(*req.Body(), &rpcReq))
		assert.Equal(t, "2.0", rpcReq["jsonrpc"])
		assert.Equal(t, "get-user", rpcReq["method"])

		body := jsonrpcResponse(map[string]any{"name": "alice"})
		return httpResponse(200, body), nil
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "get-user", `{"user_id":"1"}`)
	require.NoError(t, err)
	assert.Empty(t, result.Result().Error())

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Result().Result()), &parsed))
	assert.Equal(t, "alice", parsed["name"])
}

func TestInvoke_JSONRPCError(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		body := jsonrpcErrorResponse(-32001, "user not found")
		return httpResponse(200, body), nil
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "get-user", `{"user_id":"bad"}`)
	require.NoError(t, err)
	assert.Equal(t, "user not found", result.Result().Error())
	assert.Equal(t, int32(-32001), result.Result().ErrorCode())
}

func TestInvoke_HTTPError(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		return httpResponse(500, nil), nil
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "test", "{}")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result().Error())
}

func TestInvoke_IngressError(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		return httpErrorResponse("no sandbox available"), nil
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "test", "{}")
	require.NoError(t, err)
	assert.Equal(t, "no sandbox available", result.Result().Error())
}

func TestInvoke_IngressTransportError(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		return nil, fmt.Errorf("connection refused")
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "test", "{}")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result().Error())
}

func TestInvoke_EmptyResponseBody(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		resp := &httpingress_v1alpha.InternalHttpResponse{}
		resp.SetStatusCode(200)
		return resp, nil
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "test", "{}")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result().Error())
}

func TestInvoke_NoParams(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		body := jsonrpcResponse("ok")
		return httpResponse(200, body), nil
	}

	result, err := env.client.Invoke(env.ctx, "myapp", "clear-cache", "")
	require.NoError(t, err)
	assert.Empty(t, result.Result().Error())
}

// --- ListMethods tests ---

func TestListMethods_MissingApp(t *testing.T) {
	env := setup(t)

	result, err := env.client.ListMethods(env.ctx, "")
	require.NoError(t, err)
	assert.Equal(t, "app is required", result.Error())
}

func TestListMethods_Success(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{
				"name":        "get-user",
				"description": "Get a user",
				"category":    "users",
				"params":      map[string]any{"user_id": "string"},
			},
			{
				"name":        "clear-cache",
				"description": "Clear the cache",
				"category":    "maintenance",
			},
		}
		body := jsonrpcResponse(methods)
		return httpResponse(200, body), nil
	}

	result, err := env.client.ListMethods(env.ctx, "myapp")
	require.NoError(t, err)
	assert.Empty(t, result.Error())
	require.Len(t, result.Methods(), 2)

	m := result.Methods()[0]
	assert.Equal(t, "get-user", m.Name())
	assert.Equal(t, "Get a user", m.Description())
	assert.Equal(t, "users", m.Category())
	require.Len(t, m.Params(), 1)
	assert.Equal(t, "user_id", m.Params()[0].Name())
	assert.Equal(t, "string", m.Params()[0].ParamType())
}

func TestListMethods_FiltersIntrospectionMethods(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{"name": "$methods"},
			{"name": "$type"},
			{"name": "get-user", "description": "Get a user"},
		}
		body := jsonrpcResponse(methods)
		return httpResponse(200, body), nil
	}

	result, err := env.client.ListMethods(env.ctx, "myapp")
	require.NoError(t, err)
	require.Len(t, result.Methods(), 1)
	assert.Equal(t, "get-user", result.Methods()[0].Name())
}

func TestListMethods_PositionalParams(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{
				"name":   "add",
				"params": []any{"number", "number"},
			},
		}
		body := jsonrpcResponse(methods)
		return httpResponse(200, body), nil
	}

	result, err := env.client.ListMethods(env.ctx, "myapp")
	require.NoError(t, err)
	require.Len(t, result.Methods(), 1)

	params := result.Methods()[0].Params()
	require.Len(t, params, 2)
	assert.Equal(t, "arg0", params[0].Name())
	assert.Equal(t, "number", params[0].ParamType())
}

// --- DescribeMethods tests ---

func TestDescribeMethods_MissingApp(t *testing.T) {
	env := setup(t)

	result, err := env.client.DescribeMethods(env.ctx, "", []string{"get-user"})
	require.NoError(t, err)
	assert.Equal(t, "app is required", result.Error())
}

func TestDescribeMethods_MissingMethods(t *testing.T) {
	env := setup(t)

	result, err := env.client.DescribeMethods(env.ctx, "myapp", []string{})
	require.NoError(t, err)
	assert.Equal(t, "at least one method name is required", result.Error())
}

func TestDescribeMethods_FiltersByName(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{"name": "get-user", "description": "Get a user", "params": map[string]any{"user_id": "string"}},
			{"name": "list-users", "description": "List users"},
			{"name": "clear-cache", "description": "Clear cache"},
		}
		body := jsonrpcResponse(methods)
		return httpResponse(200, body), nil
	}

	result, err := env.client.DescribeMethods(env.ctx, "myapp", []string{"get-user"})
	require.NoError(t, err)
	assert.Empty(t, result.Error())
	require.Len(t, result.Methods(), 1)

	m := result.Methods()[0]
	assert.Equal(t, "get-user", m.Name())
	assert.Equal(t, "Get a user", m.Description())
	require.Len(t, m.Params(), 1)
}

func TestDescribeMethods_MultipleNames(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{"name": "get-user"},
			{"name": "list-users"},
			{"name": "clear-cache"},
		}
		body := jsonrpcResponse(methods)
		return httpResponse(200, body), nil
	}

	result, err := env.client.DescribeMethods(env.ctx, "myapp", []string{"get-user", "clear-cache"})
	require.NoError(t, err)
	require.Len(t, result.Methods(), 2)

	names := map[string]bool{}
	for _, m := range result.Methods() {
		names[m.Name()] = true
	}
	assert.True(t, names["get-user"], "expected get-user in results")
	assert.True(t, names["clear-cache"], "expected clear-cache in results")
}

func TestDescribeMethods_NonexistentMethod(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{"name": "get-user"},
		}
		body := jsonrpcResponse(methods)
		return httpResponse(200, body), nil
	}

	result, err := env.client.DescribeMethods(env.ctx, "myapp", []string{"nonexistent"})
	require.NoError(t, err)
	assert.Empty(t, result.Error())
	assert.Empty(t, result.Methods())
}

func TestDescribeMethods_AppNotFound(t *testing.T) {
	env := setup(t)

	result, err := env.client.DescribeMethods(env.ctx, "nonexistent", []string{"test"})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Error())
}

func TestListMethods_SafeRead(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		methods := []map[string]any{
			{"name": "list-users", "safe_read": true},
			{"name": "delete-user", "safe_read": false},
			{"name": "unmarked"},
		}
		return httpResponse(200, jsonrpcResponse(methods)), nil
	}

	result, err := env.client.ListMethods(env.ctx, "myapp")
	require.NoError(t, err)
	assert.Empty(t, result.Error())
	require.Len(t, result.Methods(), 3)
	assert.True(t, result.Methods()[0].SafeRead())
	for _, m := range result.Methods()[1:] {
		assert.False(t, m.HasSafeRead(), m.Name())
	}
}

// A non-boolean mark fails discovery loudly, like a non-string description
// does, so an author sending "true" finds out rather than silently prompting.
func TestListMethods_SafeReadMustBeBool(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		return httpResponse(200, jsonrpcResponse([]map[string]any{{"name": "quoted", "safe_read": "true"}})), nil
	}

	result, err := env.client.ListMethods(env.ctx, "myapp")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Error())
	assert.Empty(t, result.Methods())
}

func TestInvoke_LogsFailedCalls(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		body := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"user not found"}}`)
		return httpResponse(200, body), nil
	}
	result, err := env.client.Invoke(env.ctx, "myapp", "delete-user", `{"id":1}`)
	require.NoError(t, err)
	assert.Equal(t, "user not found", result.Result().Error())

	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		return httpResponse(500, nil), nil
	}
	_, err = env.client.Invoke(env.ctx, "myapp", "delete-user", `{"id":2}`)
	require.NoError(t, err)

	env.logs.mu.Lock()
	defer env.logs.mu.Unlock()
	require.Len(t, env.logs.entries, 2)
	assert.Contains(t, env.logs.entries[0].Body, `error="user not found"`)
	assert.Contains(t, env.logs.entries[1].Body, `error="admin endpoint returned status 500"`)
	assert.Equal(t, "delete-user", env.logs.entries[0].Attributes["method"])
}

// App-controlled error text is quoted, so it can't forge a status=ok line.
func TestInvoke_LogQuotesErrorMessages(t *testing.T) {
	env := setup(t)
	env.createApp(t, "myapp", "tok123")
	env.ingress.doRequest = func(ctx context.Context, req *httpingress_v1alpha.InternalHttpRequest) (*httpingress_v1alpha.InternalHttpResponse, error) {
		body := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"x\" status=ok"}}`)
		return httpResponse(200, body), nil
	}
	_, err := env.client.Invoke(env.ctx, "myapp", "delete-user", `{}`)
	require.NoError(t, err)

	env.logs.mu.Lock()
	defer env.logs.mu.Unlock()
	require.Len(t, env.logs.entries, 1)
	assert.Contains(t, env.logs.entries[0].Body, `error="x\" status=ok"`)
}
