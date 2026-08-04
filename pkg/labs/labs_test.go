package labs

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// quiet is for the tests that assert on feature state rather than on log output.
func quiet() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestDisableFeatureWithPrefix(t *testing.T) {
	Reset()

	// Enable first, then disable
	Init(quiet(), []string{"sagas", "-sagas"})

	if Sagas() {
		t.Error("Sagas should be disabled after '-sagas'")
	}
}

func TestDistributedRunnersEnabledByDefault(t *testing.T) {
	Reset()

	// GA: distributed runners are on by default with no flags set.
	Init(quiet(), nil)

	if !DistributedRunners() {
		t.Error("DistributedRunners should be enabled by default")
	}
}

func TestCaseInsensitiveFeatureNames(t *testing.T) {
	Reset()

	Init(quiet(), []string{"Sagas", "DISTRIBUTEDRUNNERS"})

	if !Sagas() {
		t.Error("Sagas should be enabled (case-insensitive)")
	}
	if !DistributedRunners() {
		t.Error("DistributedRunners should be enabled (case-insensitive)")
	}
}

func TestUnknownFeatureLogsWarning(t *testing.T) {
	Reset()

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
	Reset()

	Init(quiet(), []string{"", "  ", "sagas", "  ", ""})

	if !Sagas() {
		t.Error("Sagas should be enabled despite empty/whitespace flags")
	}
}

func TestAllKeywordEnablesAllFeatures(t *testing.T) {
	Reset()

	Init(quiet(), []string{"all"})

	for _, name := range AllFeatures() {
		if !IsEnabled(name) {
			t.Errorf("Feature %q should be enabled after Init with 'all'", name)
		}
	}
}

func TestAllKeywordWithExclusion(t *testing.T) {
	Reset()

	Init(quiet(), []string{"all", "-distributedrunners"})

	for _, name := range AllFeatures() {
		if name == FeatureDistributedRunners {
			if IsEnabled(name) {
				t.Error("DistributedRunners should be disabled after 'all,-distributedrunners'")
			}
		} else {
			if !IsEnabled(name) {
				t.Errorf("Feature %q should be enabled after 'all,-distributedrunners'", name)
			}
		}
	}
}

func TestNegativeAllDisablesAll(t *testing.T) {
	Reset()

	Init(quiet(), []string{"sagas", "distributedrunners", "-all"})

	for _, name := range AllFeatures() {
		if IsEnabled(name) {
			t.Errorf("Feature %q should be disabled after '-all'", name)
		}
	}
}
