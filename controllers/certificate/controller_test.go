package certificate

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDNSControllerPacesProvisioningFailures(t *testing.T) {
	calls := 0
	controller := &Controller{
		Log: slog.Default(),
	}
	domain := "example.com"
	provision := func(context.Context, string) error {
		calls++
		return &acmeObtainError{err: errors.New("provider unavailable")}
	}
	log := controller.Log.With("domain", domain)

	require.NoError(t, controller.provisionRouteCertificate(t.Context(), log, domain, provision))
	require.Equal(t, 1, calls)

	// A framework retry or noisy route update during the cooldown is cheap and
	// does not call the external DNS/ACME provider again.
	require.NoError(t, controller.provisionRouteCertificate(t.Context(), log, domain, provision))
	require.Equal(t, 1, calls)

	controller.failures.Store(domain, time.Now().Add(-dnsFailureCooldown))
	require.NoError(t, controller.provisionRouteCertificate(t.Context(), log, domain, provision))
	require.Equal(t, 2, calls)
}

func TestDNSControllerRetriesLocalProvisioningFailures(t *testing.T) {
	controller := &Controller{Log: slog.Default()}
	domain := "example.com"
	wantErr := errors.New("writing certificate")
	provision := func(context.Context, string) error {
		return wantErr
	}

	err := controller.provisionRouteCertificate(
		t.Context(), controller.Log.With("domain", domain), domain, provision,
	)
	require.ErrorIs(t, err, wantErr)
	require.False(t, controller.inFailureCooldown(domain))
}
