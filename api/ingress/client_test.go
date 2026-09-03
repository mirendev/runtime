package ingress

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestClientLookupCaseInsensitive(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	// Create ingress client
	client := &Client{
		log: slog.Default(),
		ec:  ec,
		eac: inmem.EAC,
	}

	// Create a test app ID
	testAppID := entity.Id("test-app-123")

	// Test case 1: Store route with mixed case host
	t.Run("LookupWithVariousCases", func(t *testing.T) {
		originalHost := "Example.Com"

		// Set route with mixed case
		route, err := client.SetRoute(ctx, originalHost, testAppID)
		require.NoError(t, err, "failed to set route")
		require.NotNil(t, route, "expected route to be created")

		// Verify the route was stored with lowercase host
		require.Equal(t, "example.com", route.Host, "expected host to be stored as lowercase")
		require.Empty(t, route.Service, "legacy SetRoute call must leave service absent")

		// Test lookup with exact case as stored (lowercase)
		result, err := client.Lookup(ctx, "example.com")
		if err != nil {
			t.Fatalf("lookup with lowercase failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with lowercase host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}

		// Test lookup with all uppercase
		result, err = client.Lookup(ctx, "EXAMPLE.COM")
		if err != nil {
			t.Fatalf("lookup with uppercase failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with uppercase host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}

		// Test lookup with mixed case (different from original)
		result, err = client.Lookup(ctx, "ExAmPlE.CoM")
		if err != nil {
			t.Fatalf("lookup with mixed case failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with mixed case host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}

		// Test lookup with original case used when setting
		result, err = client.Lookup(ctx, originalHost)
		if err != nil {
			t.Fatalf("lookup with original case failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with original case host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}
	})

	// Test case 2: Multiple routes with different hosts
	t.Run("MultipleRoutesCaseInsensitive", func(t *testing.T) {
		testAppID2 := entity.Id("test-app-456")
		testAppID3 := entity.Id("test-app-789")

		// Create routes with different hosts
		_, err := client.SetRoute(ctx, "api.example.com", testAppID2)
		if err != nil {
			t.Fatalf("failed to set route for api.example.com: %v", err)
		}

		_, err = client.SetRoute(ctx, "WEB.EXAMPLE.COM", testAppID3)
		if err != nil {
			t.Fatalf("failed to set route for WEB.EXAMPLE.COM: %v", err)
		}

		// Lookup api.example.com with different cases
		result, err := client.Lookup(ctx, "API.EXAMPLE.COM")
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route")
		} else if result.App != testAppID2 {
			t.Errorf("expected app ID %q, got %q", testAppID2, result.App)
		}

		// Lookup web.example.com with different cases
		result, err = client.Lookup(ctx, "web.example.com")
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route")
		} else if result.App != testAppID3 {
			t.Errorf("expected app ID %q, got %q", testAppID3, result.App)
		}
	})

	// Test case 3: Non-existent host returns nil
	t.Run("NonExistentHostReturnsNil", func(t *testing.T) {
		result, err := client.Lookup(ctx, "does-not-exist.com")
		if err != nil {
			t.Fatalf("lookup should not error on non-existent host: %v", err)
		}
		if result != nil {
			t.Error("expected nil for non-existent host")
		}

		// Try with different case
		result, err = client.Lookup(ctx, "DOES-NOT-EXIST.COM")
		if err != nil {
			t.Fatalf("lookup should not error on non-existent host: %v", err)
		}
		if result != nil {
			t.Error("expected nil for non-existent host")
		}
	})
}

func TestClientLookupWithWildcard(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)
	client := &Client{
		log: slog.Default(),
		ec:  ec,
		eac: inmem.EAC,
	}

	wildcardAppID := entity.Id("wildcard-app")
	exactAppID := entity.Id("exact-app")

	// Set up a wildcard route
	_, err := client.SetRoute(ctx, "*.example.com", wildcardAppID)
	require.NoError(t, err)

	t.Run("WildcardMatchesSubdomain", func(t *testing.T) {
		route, err := client.LookupWithWildcard(ctx, "foo.example.com")
		require.NoError(t, err)
		require.NotNil(t, route)
		require.Equal(t, wildcardAppID, route.App)
	})

	t.Run("WildcardMatchesAnySubdomain", func(t *testing.T) {
		route, err := client.LookupWithWildcard(ctx, "bar.example.com")
		require.NoError(t, err)
		require.NotNil(t, route)
		require.Equal(t, wildcardAppID, route.App)
	})

	t.Run("WildcardDoesNotMatchBareDomain", func(t *testing.T) {
		route, err := client.LookupWithWildcard(ctx, "example.com")
		require.NoError(t, err)
		require.Nil(t, route, "*.example.com should not match example.com")
	})

	t.Run("ExactMatchTakesPriority", func(t *testing.T) {
		_, err := client.SetRoute(ctx, "specific.example.com", exactAppID)
		require.NoError(t, err)

		route, err := client.LookupWithWildcard(ctx, "specific.example.com")
		require.NoError(t, err)
		require.NotNil(t, route)
		require.Equal(t, exactAppID, route.App)
	})

	t.Run("WildcardCaseInsensitive", func(t *testing.T) {
		route, err := client.LookupWithWildcard(ctx, "FOO.EXAMPLE.COM")
		require.NoError(t, err)
		require.NotNil(t, route)
		require.Equal(t, wildcardAppID, route.App)
	})

	t.Run("NoMatchReturnsNil", func(t *testing.T) {
		route, err := client.LookupWithWildcard(ctx, "foo.other.com")
		require.NoError(t, err)
		require.Nil(t, route)
	})
}

func TestIsWildcardHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"*.example.com", true},
		{"*.APP.EXAMPLE.COM", true},
		{"app.example.com", false},
		{"example.com", false},
		{"*", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			require.Equal(t, tt.want, IsWildcardHost(tt.host))
		})
	}
}

func TestExtractSubdomainLabel(t *testing.T) {
	tests := []struct {
		requestHost string
		routeHost   string
		want        string
	}{
		{"feat-x.app.example.com", "*.app.example.com", "feat-x"},
		{"my-branch.app.example.com", "*.app.example.com", "my-branch"},
		{"app.example.com", "*.app.example.com", ""},              // no prefix
		{"app.example.com", "app.example.com", ""},                // not a wildcard
		{"deep.sub.app.example.com", "*.app.example.com", ""},     // multi-level subdomain
		{"feat-x.other.com", "*.app.example.com", ""},             // different domain
		{"FEAT-X.APP.EXAMPLE.COM", "*.app.example.com", "feat-x"}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.requestHost+"_"+tt.routeHost, func(t *testing.T) {
			got := ExtractSubdomainLabel(tt.requestHost, tt.routeHost)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWAFProfile(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)
	client := &Client{
		log: slog.Default(),
		ec:  ec,
		eac: inmem.EAC,
	}

	testAppID := entity.Id("test-app-waf")

	t.Run("CreateAndGet", func(t *testing.T) {
		profile, err := client.CreateWAFProfile(ctx, 2)
		require.NoError(t, err)
		require.Equal(t, int64(2), profile.ParanoiaLevel)

		got, err := client.GetWAFProfileByID(ctx, profile.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, int64(2), got.ParanoiaLevel)
	})

	t.Run("InvalidLevels", func(t *testing.T) {
		_, err := client.CreateWAFProfile(ctx, 0)
		require.Error(t, err)

		_, err = client.CreateWAFProfile(ctx, 5)
		require.Error(t, err)
	})

	t.Run("SetRouteWAFLevel", func(t *testing.T) {
		_, err := client.SetRoute(ctx, "waf.example.com", testAppID)
		require.NoError(t, err)

		route, err := client.SetRouteWAFLevel(ctx, "waf.example.com", 2)
		require.NoError(t, err)
		require.False(t, entity.Empty(route.WafProfile))

		profile, err := client.GetWAFProfileByID(ctx, route.WafProfile)
		require.NoError(t, err)
		require.Equal(t, int64(2), profile.ParanoiaLevel)
	})

	t.Run("DetachWAFProfile", func(t *testing.T) {
		route, err := client.DetachWAFProfile(ctx, "waf.example.com")
		require.NoError(t, err)
		require.True(t, entity.Empty(route.WafProfile))

		looked, err := client.Lookup(ctx, "waf.example.com")
		require.NoError(t, err)
		require.True(t, entity.Empty(looked.WafProfile))
	})

	t.Run("NonExistentRoute", func(t *testing.T) {
		_, err := client.SetRouteWAFLevel(ctx, "nonexistent.example.com", 1)
		require.Error(t, err)
	})

	t.Run("SetOnRoute", func(t *testing.T) {
		_, err := client.SetRoute(ctx, "waf2.example.com", testAppID)
		require.NoError(t, err)

		route, err := client.Lookup(ctx, "waf2.example.com")
		require.NoError(t, err)

		updated, err := client.SetRouteWAFLevelOnRoute(ctx, route, 3)
		require.NoError(t, err)
		require.False(t, entity.Empty(updated.WafProfile))

		profile, err := client.GetWAFProfileByID(ctx, updated.WafProfile)
		require.NoError(t, err)
		require.Equal(t, int64(3), profile.ParanoiaLevel)
	})

	t.Run("AllParanoiaLevels", func(t *testing.T) {
		for _, level := range []int{1, 2, 3, 4} {
			route, err := client.SetRouteWAFLevel(ctx, "waf.example.com", level)
			require.NoError(t, err)

			profile, err := client.GetWAFProfileByID(ctx, route.WafProfile)
			require.NoError(t, err)
			require.Equal(t, int64(level), profile.ParanoiaLevel)
		}
	})
}

func TestValidateWildcardHost(t *testing.T) {
	tests := []struct {
		host    string
		wantErr bool
	}{
		{"example.com", false},
		{"*.example.com", false},
		{"*.sub.example.com", false},
		{"*.com", true},
		{"*.", true},
		{"*.example.*", true},
		{"foo.*.com", false}, // not a wildcard pattern (doesn't start with *.), treated as literal
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			err := ValidateWildcardHost(tt.host)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRouteRequestTimeout(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)
	client := &Client{
		log: slog.Default(),
		ec:  ec,
		eac: inmem.EAC,
	}

	testAppID := entity.Id("test-app-timeout")
	const host = "timeout.example.com"

	_, err := client.SetRoute(ctx, host, testAppID)
	require.NoError(t, err)

	// SetRoute returns the struct it wrote, which has no entity ID; the helpers
	// patch by ID, so work from a looked-up route.
	route, err := client.Lookup(ctx, host)
	require.NoError(t, err)
	require.Empty(t, route.RequestTimeout, "a fresh route carries no override")

	t.Run("SetAndOverwrite", func(t *testing.T) {
		_, err := client.SetRouteRequestTimeout(ctx, route, "10m")
		require.NoError(t, err)

		got, err := client.Lookup(ctx, host)
		require.NoError(t, err)
		require.Equal(t, "10m", got.RequestTimeout)

		_, err = client.SetRouteRequestTimeout(ctx, got, "300s")
		require.NoError(t, err)

		got, err = client.Lookup(ctx, host)
		require.NoError(t, err)
		require.Equal(t, "300s", got.RequestTimeout)
	})

	t.Run("RejectsInvalidDurations", func(t *testing.T) {
		// Establish a known value rather than inheriting whatever an earlier
		// subtest happened to leave behind.
		_, err := client.SetRouteRequestTimeout(ctx, route, "45s")
		require.NoError(t, err)

		for _, bad := range []string{"10", "forever", "0s", "-5m"} {
			_, err := client.SetRouteRequestTimeout(ctx, route, bad)
			require.Error(t, err, "expected %q to be rejected", bad)
		}

		// The rejected writes must not have disturbed the stored value.
		got, err := client.Lookup(ctx, host)
		require.NoError(t, err)
		require.Equal(t, "45s", got.RequestTimeout)
	})

	t.Run("ClearLeavesOtherFieldsIntact", func(t *testing.T) {
		// Clearing an already-empty override would pass vacuously, so set one.
		_, err := client.SetRouteRequestTimeout(ctx, route, "90s")
		require.NoError(t, err)

		withWAF, err := client.SetRouteWAFLevel(ctx, host, 2)
		require.NoError(t, err)
		require.NotEmpty(t, withWAF.WafProfile)
		require.Equal(t, "90s", withWAF.RequestTimeout, "precondition: an override is in place")

		_, err = client.ClearRouteRequestTimeout(ctx, withWAF)
		require.NoError(t, err)

		got, err := client.Lookup(ctx, host)
		require.NoError(t, err)
		require.Empty(t, got.RequestTimeout, "override should be gone")
		require.Equal(t, withWAF.WafProfile, got.WafProfile, "clearing must not disturb the WAF profile")
		require.Equal(t, testAppID, got.App)
		require.Equal(t, host, got.Host)
	})
}

func TestHTTPService(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service string
		spec    core_v1alpha.ConfigSpec
		wantErr string
	}{
		{
			name:    "HTTP port in ports array",
			service: "api",
			spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
				Name: "api", Ports: []core_v1alpha.ConfigSpecServicesPorts{{Port: 8080, Type: "http"}},
			}}},
		},
		{
			name:    "legacy scalar port defaults to HTTP",
			service: "web",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web", Port: 3000}}},
		},
		{
			name:    "implicit web port defaults to HTTP",
			service: "web",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}}},
		},
		{
			name:    "port array without type defaults to HTTP",
			service: "api",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "api", Ports: []core_v1alpha.ConfigSpecServicesPorts{{Port: 8080}}}}},
		},
		{
			name:    "web with only TCP ports keeps implicit HTTP port",
			service: "web",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web", Ports: []core_v1alpha.ConfigSpecServicesPorts{{Port: 7000, Type: "tcp"}}}}},
		},
		{
			name:    "scalar web with explicit non-HTTP port_type is rejected",
			service: "web",
			spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
				Name: "web", Port: 8080, PortType: "tcp",
			}}},
			wantErr: `app service "web" has no HTTP port`,
		},
		{
			name:    "scalar web with no port but non-HTTP port_type is admitted",
			service: "web",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web", PortType: "tcp"}}},
		},
		{
			name:    "missing service",
			service: "api",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web", Port: 3000}}},
			wantErr: `app service "api" does not exist`,
		},
		{
			name:    "non-web service without a port is not HTTP capable",
			service: "worker",
			spec:    core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "worker"}}},
			wantErr: `app service "worker" has no HTTP port`,
		},
		{
			name:    "TCP-only service",
			service: "irc",
			spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
				Name: "irc", Ports: []core_v1alpha.ConfigSpecServicesPorts{{Port: 6667, Type: "tcp"}},
			}}},
			wantErr: `app service "irc" has no HTTP port`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := HTTPService(&tc.spec, tc.service)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

// TestHTTPServiceRejectsScalarWebWithUnroutablePort pins the boundary the fix
// draws, and the reason it is not simply "appspec.Build emitted an HTTP port".
//
// Neither of the two scalar web shapes below gets an HTTP-typed container port
// out of appspec.Build once port_type says tcp, so that alone cannot be the
// test: it would condemn the working case along with the broken one. What
// separates them is whether the port the app is told to use lines up with the
// port the activator dials when it finds no HTTP-typed port (3000, see
// extractHTTPPort in components/activator). Declaring no port leaves both at
// 3000 and serves; naming 8080 leaves the app on 8080 and the activator on
// 3000, so every request fails upstream and the route should never be written.
func TestHTTPServiceRejectsScalarWebWithUnroutablePort(t *testing.T) {
	// httpPortCount reports how many HTTP-typed container ports appspec.Build
	// emits for a spec's web service.
	httpPortCount := func(t *testing.T, spec *core_v1alpha.ConfigSpec) int {
		t.Helper()
		sb, buildErr := appspec.Build(slog.Default(), appspec.Options{
			AppID:   entity.Id("app/demo"),
			AppName: "demo",
			Version: &core_v1alpha.AppVersion{ID: entity.Id("app_version/v1")},
			Config:  spec, Service: "web", Image: "img",
		})
		require.NoError(t, buildErr)

		n := 0
		for _, cp := range sb.Container[0].Port {
			if cp.Type == "http" {
				n++
			}
		}
		return n
	}

	t.Run("declared non-HTTP port is rejected", func(t *testing.T) {
		spec := &core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
			Name: "web", Port: 8080, PortType: "tcp",
		}}}

		err := HTTPService(spec, "web")
		require.Error(t, err)
		require.ErrorContains(t, err, `app service "web" has no HTTP port`)

		require.Equal(t, 0, httpPortCount(t, spec),
			"appspec.Build emits no http port, and PORT=8080 does not match the activator's 3000 fallback")
	})

	t.Run("no declared port stays admitted", func(t *testing.T) {
		spec := &core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
			Name: "web", PortType: "tcp",
		}}}

		require.NoError(t, HTTPService(spec, "web"),
			"the no-port web shape must stay admitted: it works end to end today")

		// Deliberately the same count as the rejected case above. This is the
		// assertion that keeps the test honest about what the rule really is:
		// admission here does not come from appspec.Build emitting an HTTP
		// port, it comes from PORT and the activator's fallback both being
		// 3000. Narrowing HTTPService to require an HTTP-typed port would
		// reject this working configuration.
		require.Equal(t, 0, httpPortCount(t, spec))
	})
}

func TestClientSetRouteStoresService(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	client := &Client{log: slog.Default(), ec: entityserver.NewClient(slog.Default(), inmem.EAC), eac: inmem.EAC}

	appID, err := client.ec.Create(ctx, "app-api", &core_v1alpha.App{})
	require.NoError(t, err)
	configID, err := client.ec.Create(ctx, "app-api-config", &core_v1alpha.ConfigVersion{
		App: appID,
		Spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
			Name: "api", Ports: []core_v1alpha.ConfigSpecServicesPorts{{Port: 8080, Type: "http"}},
		}}},
	})
	require.NoError(t, err)
	versionID, err := client.ec.Create(ctx, "app-api-version", &core_v1alpha.AppVersion{App: appID, ConfigVersion: configID})
	require.NoError(t, err)
	app := &core_v1alpha.App{ID: appID, ActiveVersion: versionID}
	require.NoError(t, client.ec.Update(ctx, app))

	_, err = client.SetRoute(ctx, "api.example.com", appID, "api")
	require.NoError(t, err)
	route, err := client.Lookup(ctx, "api.example.com")
	require.NoError(t, err)
	require.Equal(t, "api", route.Service)
}
