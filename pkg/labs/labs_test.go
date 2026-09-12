package labs

import (
	"bytes"
	"log/slog"
	"sort"
	"strings"
	"testing"
)

// quiet is for the tests that assert on feature state rather than on log output.
func quiet() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// withFeatures swaps in a synthetic feature registry for one test and returns
// its names sorted.
//
// Everything below is about how Init parses flags, which is independent of
// whichever features happen to be in labs this release. Naming real features
// here meant rewriting this file every time one graduated out, and the registry
// eventually gets too small to say anything: with a single feature left there
// is no way to tell "the exclusion disabled the one I named" apart from "the
// exclusion disabled everything". Synthetic names cost nothing and never churn.
func withFeatures(t *testing.T, defaults map[string]bool) []string {
	t.Helper()

	mu.Lock()
	savedDefaults, savedEnabled := featureDefaults, enabledFeatures
	featureDefaults = defaults
	enabledFeatures = make(map[string]bool)
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		featureDefaults, enabledFeatures = savedDefaults, savedEnabled
	})

	names := make([]string, 0, len(defaults))
	for name := range defaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestDefaultsApplyWithoutFlags(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": true, "beta": false})

	Init(quiet(), nil)

	if !IsEnabled("alpha") {
		t.Error("alpha defaults to on and should be enabled with no flags")
	}
	if IsEnabled("beta") {
		t.Error("beta defaults to off and should stay disabled with no flags")
	}
}

func TestDisableFeatureWithPrefix(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": false})

	// Enable first, then disable
	Init(quiet(), []string{"alpha", "-alpha"})

	if IsEnabled("alpha") {
		t.Error("alpha should be disabled after '-alpha'")
	}
}

// A feature that ships on by default keeps its flag for a release purely so an
// operator can put the old behavior back. That escape hatch has to work from a
// cold start, with nothing else named.
func TestNegativePrefixAloneDisablesDefaultOnFeature(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": true, "beta": true})

	Init(quiet(), []string{"-alpha"})

	if IsEnabled("alpha") {
		t.Error("alpha should be disabled by '-alpha' alone")
	}
	if !IsEnabled("beta") {
		t.Error("beta should stay enabled when only alpha is named")
	}
}

func TestCaseInsensitiveFeatureNames(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": false, "beta": false})

	Init(quiet(), []string{"Alpha", "BETA"})

	if !IsEnabled("alpha") {
		t.Error("alpha should be enabled (case-insensitive)")
	}
	if !IsEnabled("beta") {
		t.Error("beta should be enabled (case-insensitive)")
	}
}

func TestUnknownFeatureLogsWarning(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": false})

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	Init(logger, []string{"unknownfeature"})

	logOutput := buf.String()
	if !strings.Contains(logOutput, "unknown labs feature flag") {
		t.Errorf("Expected warning about unknown feature, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "unknownfeature") {
		t.Errorf("Expected warning to contain the unknown feature name, got: %s", logOutput)
	}
}

func TestEmptyAndWhitespaceFlags(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": false})

	Init(quiet(), []string{"", "  ", "alpha", "  ", ""})

	if !IsEnabled("alpha") {
		t.Error("alpha should be enabled despite empty/whitespace flags")
	}
}

func TestAllKeywordEnablesAllFeatures(t *testing.T) {
	names := withFeatures(t, map[string]bool{"alpha": false, "beta": false})

	Init(quiet(), []string{"all"})

	for _, name := range names {
		if !IsEnabled(name) {
			t.Errorf("Feature %q should be enabled after Init with 'all'", name)
		}
	}
}

func TestAllKeywordWithExclusion(t *testing.T) {
	names := withFeatures(t, map[string]bool{"alpha": false, "beta": false})

	Init(quiet(), []string{"all", "-beta"})

	for _, name := range names {
		if name == "beta" {
			if IsEnabled(name) {
				t.Error("beta should be disabled after 'all,-beta'")
			}
		} else if !IsEnabled(name) {
			t.Errorf("Feature %q should be enabled after 'all,-beta'", name)
		}
	}
}

func TestNegativeAllDisablesAll(t *testing.T) {
	names := withFeatures(t, map[string]bool{"alpha": true, "beta": true})

	Init(quiet(), []string{"alpha", "beta", "-all"})

	for _, name := range names {
		if IsEnabled(name) {
			t.Errorf("Feature %q should be disabled after '-all'", name)
		}
	}
}

func TestResetRestoresDefaults(t *testing.T) {
	withFeatures(t, map[string]bool{"alpha": true, "beta": false})

	Init(quiet(), []string{"-alpha", "beta"})
	Reset()

	if !IsEnabled("alpha") {
		t.Error("Reset should restore alpha to its default of on")
	}
	if IsEnabled("beta") {
		t.Error("Reset should restore beta to its default of off")
	}
}
