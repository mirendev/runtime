package workloadidentity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	oldAnchor = "https://cluster-abc.miren.systems"
	newAnchor = "https://api.miren.cloud/identity/cluster-abc"
)

// issuerAt builds an issuer over dataPath, reusing whatever key and
// prev-issuer record are already there — which is what a restart does.
func issuerAt(t *testing.T, dataPath, issuerURL string) *Issuer {
	t.Helper()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:       dataPath,
		IssuerURL:      issuerURL,
		OrganizationID: "org-1",
		ClusterID:      "cluster-abc",
	})
	require.NoError(t, err)
	return iss
}

// The scenario the overlap exists for: a token minted under the old anchor is
// still held by a workload when the cluster restarts under the new one. It has
// to keep verifying, or the flip takes out the cluster's own services.
func TestTokenFromSupersededAnchorStillVerifies(t *testing.T) {
	dataPath := t.TempDir()

	before := issuerAt(t, dataPath, oldAnchor)
	token, err := before.IssueToken("web", "sandbox-1")
	require.NoError(t, err)

	// Restarting under a different anchor is all it takes: the issuer notices
	// the move against the anchor the last run recorded.
	after := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor, oldAnchor}, after.AcceptedIssuers())

	// The old token verifies...
	claims, err := after.VerifyToken(token, DefaultAudience)
	require.NoError(t, err)
	require.Equal(t, oldAnchor, claims.Issuer)

	// ...through the API validator too, which is the path in-cluster calls take.
	apiToken, err := before.IssueTokenWithOptions("web", "sandbox-1", TokenOptions{Audience: []string{APIAudience}})
	require.NoError(t, err)
	validated, err := NewValidator(after).Validate(apiToken)
	require.NoError(t, err)
	require.Equal(t, oldAnchor, validated.Issuer)

	// ...while new tokens carry the new anchor.
	fresh, err := after.IssueToken("web", "sandbox-1")
	require.NoError(t, err)
	freshClaims, err := after.VerifyToken(fresh, DefaultAudience)
	require.NoError(t, err)
	require.Equal(t, newAnchor, freshClaims.Issuer)
}

// Without the overlap record, the old token must be rejected — otherwise the
// window never closes and any issuer would be accepted forever.
// Once the overlap lapses the old issuer stops being accepted, even though the
// key that signed it is still the cluster's own. This is the check that makes
// the window actually close.
func TestTokenFromLapsedAnchorIsRejected(t *testing.T) {
	dataPath := t.TempDir()

	before := issuerAt(t, dataPath, oldAnchor)
	token, err := before.IssueToken("web", "sandbox-1")
	require.NoError(t, err)

	moved := issuerAt(t, dataPath, newAnchor)
	_, err = moved.VerifyToken(token, DefaultAudience)
	require.NoError(t, err, "still inside the overlap window")

	// Age the record past its deadline and restart.
	prevPath := filepath.Join(dataPath, "server", PrevIssuerFile)
	require.NoError(t, writePrevIssuer(prevPath, oldAnchor, time.Now().Add(-issuerOverlap-time.Hour)))

	lapsed := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor}, lapsed.AcceptedIssuers())

	_, err = lapsed.VerifyToken(token, DefaultAudience)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not this cluster")
}

// The move is recorded once. A restart that does not change the anchor must
// leave the overlap alone rather than resetting its clock.
func TestRestartWithoutAMoveKeepsTheSameOverlap(t *testing.T) {
	dataPath := t.TempDir()

	issuerAt(t, dataPath, oldAnchor)
	moved := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor, oldAnchor}, moved.AcceptedIssuers())

	firstDeadline := overlapDeadline(t, dataPath)

	restarted := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor, oldAnchor}, restarted.AcceptedIssuers())
	require.Equal(t, firstDeadline, overlapDeadline(t, dataPath))
}

// A cluster that has never recorded an anchor — a first boot, or an upgrade
// from before this existed — must not treat its own anchor as superseded.
func TestFirstBootRecordsNoOverlap(t *testing.T) {
	dataPath := t.TempDir()

	iss := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor}, iss.AcceptedIssuers())

	recorded, err := readIssuerFile(filepath.Join(dataPath, "server", CurrentIssuerFile))
	require.NoError(t, err)
	require.Equal(t, newAnchor, recorded)
}

func overlapDeadline(t *testing.T, dataPath string) time.Time {
	t.Helper()

	record, err := readPrevIssuer(filepath.Join(dataPath, "server", PrevIssuerFile), time.Now())
	require.NoError(t, err)
	require.NotNil(t, record)
	return record.NotAfter
}

// A lapsed record stops widening what verifies, so the overlap is genuinely
// time-bounded rather than permanent.
func TestLapsedOverlapIsIgnored(t *testing.T) {
	dataPath := t.TempDir()
	serverDir := filepath.Join(dataPath, "server")
	require.NoError(t, os.MkdirAll(serverDir, 0700))
	require.NoError(t, writePrevIssuer(filepath.Join(serverDir, PrevIssuerFile), oldAnchor,
		time.Now().Add(-issuerOverlap-time.Hour)))

	after := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor}, after.AcceptedIssuers())
	require.False(t, after.AcceptsIssuer(oldAnchor))
}

// There is one overlap slot, so moving twice inside a window keeps the most
// recent anchor. The earliest is dropped — pathological, warned about, and not
// worth blocking a move over.
func TestSecondMoveInsideTheWindowKeepsTheLatest(t *testing.T) {
	dataPath := t.TempDir()
	third := "https://third.example"

	issuerAt(t, dataPath, oldAnchor)
	issuerAt(t, dataPath, newAnchor)
	last := issuerAt(t, dataPath, third)

	require.Equal(t, []string{third, newAnchor}, last.AcceptedIssuers())
	require.False(t, last.AcceptsIssuer(oldAnchor))
}

// The record only widens what verifies, so an unreadable one costs an overlap
// and must not strand the cluster on a boot failure.
func TestMalformedOverlapRecordDoesNotBlockStartup(t *testing.T) {
	dataPath := t.TempDir()
	path := filepath.Join(dataPath, "server", PrevIssuerFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0644))

	after := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{newAnchor}, after.AcceptedIssuers())
}

// Discovery keeps answering on the old host during the overlap, so a verifier
// pinned to the previous anchor can still fetch keys for tokens it holds.
func TestSupersededHostStillServesDiscovery(t *testing.T) {
	dataPath := t.TempDir()
	issuerAt(t, dataPath, oldAnchor)

	after := issuerAt(t, dataPath, newAnchor)
	require.Equal(t, []string{"api.miren.cloud", "cluster-abc.miren.systems"}, after.Hostnames())

	// The discovery document itself always advertises the current anchor —
	// nothing new is minted under the old one.
	require.Contains(t, string(after.DiscoveryDocument()), newAnchor)
}

// Moving back inside the window leaves the intermediate anchor accepted, not
// the one being returned to: tokens minted during the excursion are the ones
// still in circulation, and the cluster must not list its own anchor as its
// own predecessor.
func TestMovingBackKeepsTheIntermediateAnchor(t *testing.T) {
	dataPath := t.TempDir()

	issuerAt(t, dataPath, oldAnchor)
	issuerAt(t, dataPath, newAnchor)
	back := issuerAt(t, dataPath, oldAnchor)

	require.Equal(t, []string{oldAnchor, newAnchor}, back.AcceptedIssuers())
	require.True(t, back.AcceptsIssuer(newAnchor))
}
