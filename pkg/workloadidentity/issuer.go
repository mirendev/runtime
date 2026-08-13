// Package workloadidentity implements OIDC workload identity tokens for
// sandbox containers, following the GitHub Actions OIDC pattern.
//
// # Trust Model
//
// Each cluster is its own OIDC issuer with an independent signing key. New
// clusters sign with RS256 (RSA), the universally supported default; clusters
// provisioned before that default keep their EdDSA key advertised in JWKS for
// verification while new tokens are signed with a freshly generated RS256 key.
//
// Miren Cloud is never in the signing path. The private key is generated here,
// stays on disk here, and is not sent anywhere — so a compromise of cloud
// yields public keys and no ability to mint an identity. Cloud contributes
// organization_id and cluster_id as claim metadata during registration and,
// when a cluster opts in, hosts discovery on the cluster's behalf.
//
// # Issuer URL (iss claim)
//
// The issuer URL is the cluster's cryptographic identity anchor — it's baked
// into every token and pinned in external trust configurations. In precedence
// order:
//
//   - The cloud-assigned anchor (https://api.miren.cloud/identity/<cluster-id>),
//     when the cluster is registered and --identity-anchor=cloud. The cluster
//     publishes the public half of its key set and cloud serves discovery for
//     it. This is what lets a cluster behind carrier NAT federate to an outside
//     verifier at all, and keeps federation working while the cluster is down.
//   - The cloud-provisioned DNS hostname (e.g. https://cluster-abc.miren.systems),
//     with the cluster serving its own discovery. The default, because moving a
//     registered cluster's iss breaks every external trust configuration pinned
//     to the old value.
//   - cfg.TLS.AdditionalNames[0] for bare-metal clusters without registration,
//     meaning the anchor is determined by config list order. This fallback is
//     intentionally simple for v1; a more deliberate selection mechanism (e.g.,
//     an explicit --issuer-url flag) may be warranted if bare-metal OIDC
//     federation sees adoption.
//
// Anchoring per cluster means external verifiers configure trust per cluster
// rather than once for all of Miren. A central issuer would reduce that to one
// trust config scoped by claims, but would introduce a single point of
// compromise for every cluster — which is precisely what keeping the signing
// keys here avoids, cloud-hosted discovery or not.
//
// A cluster with no hostname at all still gets an issuer, anchored at
// LocalIssuerURL. Identity is not conditional on being externally addressable:
// the cluster's own services authenticate to each other with these tokens and
// verify them in-process against the signing keys, which needs no DNS, no
// reachability, and no publicly trusted certificate. Only federation to an
// outside party needs those, and that is a property of the anchor rather than
// a mode the cluster is in.
package workloadidentity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"miren.dev/runtime/pkg/workloadroles"
)

type IssuerConfig struct {
	DataPath       string
	IssuerURL      string
	OrganizationID string
	ClusterID      string
}

type Issuer struct {
	// primary is the default signing key. All issued tokens are signed with it.
	primary *signingKey
	// keys are the live signing keys: primary first, then any keys loaded from
	// the workload-identity.d directory. All are published in JWKS. Holding
	// several supports key rotation (stage and publish a new key before cutting
	// over) and multiple key types live at once.
	keys []*signingKey
	// advertised holds additional public keys published in JWKS for
	// verification only (e.g. a rotated-out previous key, or a legacy EdDSA key
	// retained after migrating signing to RS256).
	advertised []jose.JSONWebKey
	issuerURL  string
	// supersededIssuer is an anchor this cluster used to mint under, still
	// accepted for verification until the tokens carrying it have expired.
	// Empty once the overlap lapses. Never signed with.
	supersededIssuer string
	organizationID   string
	clusterID        string
}

// TokenIssuer is the minting surface the sandbox controller depends on. The
// concrete *Issuer satisfies it directly (the coordinator holds the signing
// key). Distributed runners have no signing key, so they supply an
// implementation that proxies minting to the coordinator over RPC.
type TokenIssuer interface {
	IssueToken(app, sandboxID string) (string, error)
	IssueTokenWithOptions(app, sandboxID string, opts TokenOptions) (string, error)
	IssueSystemWorkloadToken(workload SystemWorkload, opts TokenOptions) (string, error)
	IssuerURL() string
}

var _ TokenIssuer = (*Issuer)(nil)

// IdentityType names the kind of principal a token represents.
//
// The value travels as its own claim rather than being inferred from the
// subject. This lets verifiers authorize an identity class without reparsing
// the subject grammar. Tokens issued before this claim existed carry no
// identity_type, which reads as "not a system workload" and so fails closed for
// system-only resources.
//
// It is a defined type so that switches over it are exhaustiveness-checked,
// the way rpc.AuthMethod already is. Note that this buys nothing at the trust
// boundary itself: a token arriving from a caller decodes into whatever string
// it contained, so verification still has to compare the value rather than
// assume it is one of the constants below.
type IdentityType string

const (
	IdentityTypeSandbox IdentityType = "sandbox"
	IdentityTypeSystem  IdentityType = "system"
)

type WorkloadClaims struct {
	jwt.RegisteredClaims
	OrganizationID string `json:"organization_id,omitempty"`
	ClusterID      string `json:"cluster_id,omitempty"`
	App            string `json:"app,omitempty"`
	// SandboxID deliberately keeps its unconditional encoding: it predates
	// system workload tokens and external verifiers may already federate on its
	// presence. System workload tokens therefore carry an empty sandbox_id
	// rather than omitting it.
	SandboxID      string         `json:"sandbox_id"`
	IdentityType   IdentityType   `json:"identity_type,omitempty"`
	SystemWorkload SystemWorkload `json:"system_workload,omitempty"`
	// Role is the authorization role the token authenticates as (see
	// pkg/workloadroles). Resolved server-side from the app the sandbox belongs
	// to; never supplied by the workload. Only sandbox tokens carry it.
	Role string `json:"role,omitempty"`
}

// LocalIssuerURL anchors a cluster that has no hostname to advertise.
//
// It is deliberately a name the cluster already owns rather than a synthetic
// placeholder: cluster.local is how the cluster refers to itself internally,
// resolved to whatever is locally appropriate (the registry router on the
// coordinator, the coordinator's address on a runner). Nothing outside can
// resolve it at all, which is the honest signal that such a cluster cannot
// federate to an external party until it is given a real hostname.
//
// Note that this is an identity anchor, not an endpoint. It is compared as a
// string when verifying, never fetched, so it does not matter that the
// resolution it names is set up later in startup or means different things in
// different processes.
const LocalIssuerURL = "https://cluster.local"

func NewIssuer(cfg IssuerConfig) (*Issuer, error) {
	keyPath := filepath.Join(cfg.DataPath, "server", "workload-identity.key")

	primary, err := loadOrGenerateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("workload identity key: %w", err)
	}

	issuerURL := cfg.IssuerURL
	if issuerURL == "" {
		issuerURL = LocalIssuerURL
	}

	iss := &Issuer{
		primary:        primary,
		keys:           []*signingKey{primary},
		issuerURL:      issuerURL,
		organizationID: cfg.OrganizationID,
		clusterID:      cfg.ClusterID,
	}

	// Load previous signing key for rotation overlap. During key rotation,
	// the operator moves workload-identity.key → workload-identity.key.prev
	// and restarts. The EdDSA→RS256 migration in loadOrGenerateKey uses the same
	// .prev slot. The old key is published (verify-only) in JWKS so tokens signed
	// by it remain verifiable until they expire. A present-but-broken .prev is a
	// startup failure: silently dropping it would break verification overlap for
	// already-issued tokens with no operator signal.
	prevPath := keyPath + ".prev"
	prevData, err := os.ReadFile(prevPath)
	switch {
	case err == nil:
		prevKP, err := loadSigningKeyFromPEM(string(prevData))
		if err != nil {
			return nil, fmt.Errorf("loading previous signing key %s: %w", prevPath, err)
		}
		iss.advertised = append(iss.advertised, jwkForPublic(prevKP.public, prevKP.kid))
	case errors.Is(err, fs.ErrNotExist):
		// No previous key; nothing to advertise.
	default:
		return nil, fmt.Errorf("reading previous signing key %s: %w", prevPath, err)
	}

	// Reconcile the anchor against the one the last run used. A move is
	// detected here, by the only thing that knows both values, rather than by
	// whatever changed the setting — which would otherwise have to re-derive
	// this precedence chain and could record the wrong outgoing anchor.
	//
	// Tokens minted under a superseded anchor are still in circulation,
	// including ones an app read once and cached, so it keeps verifying for the
	// rest of its overlap window even though nothing is signed with it.
	superseded, err := trackAnchorMove(filepath.Dir(keyPath), issuerURL, time.Now())
	if err != nil {
		return nil, err
	}
	iss.supersededIssuer = superseded

	// Load additional live keys from the workload-identity.d directory (a sibling
	// of the primary key file). Each key there is sign-capable and published in
	// JWKS, enabling key rotation and multiple key types to coexist. A missing
	// directory is normal; a malformed key is logged and skipped rather than
	// failing startup.
	keysDir := filepath.Join(filepath.Dir(keyPath), "workload-identity.d")
	iss.keys = append(iss.keys, loadLiveKeysDir(keysDir)...)

	return iss, nil
}

// loadLiveKeysDir loads every key file in dir as a live signing key. Directory
// entries are returned by os.ReadDir sorted by name (deterministic). Directories
// and dotfiles are skipped; unreadable or unparseable files are logged and
// skipped.
func loadLiveKeysDir(dir string) []*signingKey {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("reading workload identity keys directory", "dir", dir, "error", err)
		}
		return nil
	}

	var keys []*signingKey
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("reading workload identity key", "path", path, "error", err)
			continue
		}
		kp, err := loadSigningKeyFromPEM(string(data))
		if err != nil {
			slog.Warn("parsing workload identity key", "path", path, "error", err)
			continue
		}
		keys = append(keys, kp)
	}
	return keys
}

func (iss *Issuer) IssuerURL() string {
	return iss.issuerURL
}

// AcceptedIssuers are the iss values this cluster will verify: the current
// anchor, and a superseded one still inside its overlap window.
//
// Only the current anchor is ever minted under — see IssuerURL. Verification
// paths must consult this instead of comparing against IssuerURL, or an anchor
// flip invalidates every token already in circulation.
func (iss *Issuer) AcceptedIssuers() []string {
	if iss.supersededIssuer == "" {
		return []string{iss.issuerURL}
	}
	return []string{iss.issuerURL, iss.supersededIssuer}
}

// AcceptsIssuer reports whether tokens carrying this iss should be verified.
func (iss *Issuer) AcceptsIssuer(issuer string) bool {
	return issuer != "" && (issuer == iss.issuerURL || issuer == iss.supersededIssuer)
}

func (iss *Issuer) PublicKey() any {
	return iss.primary.public
}

func (iss *Issuer) Hostname() string {
	return issuerHostname(iss.issuerURL)
}

// Hostnames are the hosts this cluster answers discovery on: the current anchor
// and a superseded one still inside its overlap window.
//
// The old host keeps serving so an external verifier pinned to the previous
// anchor can still fetch keys for tokens minted before the flip. Nothing new is
// issued under it, so the window closes on its own.
func (iss *Issuer) Hostnames() []string {
	var hosts []string
	for _, issuer := range iss.AcceptedIssuers() {
		if host := issuerHostname(issuer); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func issuerHostname(issuer string) string {
	u, err := url.Parse(issuer)
	if err != nil {
		return ""
	}
	return u.Host
}

const (
	DefaultTTL = 1 * time.Hour
	MaxTTL     = 24 * time.Hour
	MinTTL     = 60 * time.Second

	// DefaultAudience is the audience stamped on a token whose caller did not
	// ask for a specific one. Callers guarding a particular service should
	// always request their own audience and verify it, so that a token minted
	// for one service cannot be replayed against another.
	DefaultAudience = "miren"
)

type TokenOptions struct {
	Audience []string
	TTL      time.Duration
	// Role is the authorization role to embed. Empty means the default
	// (workloadroles.Default). An unknown role name is embedded as-is and is
	// denied everything at authorize time, so a misconfiguration fails closed.
	Role string
}

func (iss *Issuer) IssueToken(app, sandboxID string) (string, error) {
	return iss.IssueTokenWithOptions(app, sandboxID, TokenOptions{})
}

func (iss *Issuer) IssueTokenWithOptions(app, sandboxID string, opts TokenOptions) (string, error) {
	subject, err := newSandboxSubject(iss.organizationID, app, sandboxID)
	if err != nil {
		return "", fmt.Errorf("building sandbox workload subject: %w", err)
	}

	role := opts.Role
	if role == "" {
		role = workloadroles.Default
	}

	claims := iss.baseClaims(subject, opts)
	claims.App = app
	claims.SandboxID = sandboxID
	claims.IdentityType = IdentityTypeSandbox
	claims.Role = role

	return iss.sign(claims)
}

// baseClaims fills in everything common to every token the cluster issues:
// registered claims, the cluster metadata external verifiers federate on, and
// the normalized audience and lifetime.
func (iss *Issuer) baseClaims(subject Subject, opts TokenOptions) WorkloadClaims {
	now := time.Now()

	aud := opts.Audience
	if len(aud) == 0 {
		aud = []string{DefaultAudience}
	}

	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	if ttl < MinTTL {
		ttl = MinTTL
	}

	return WorkloadClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    iss.issuerURL,
			Subject:   subject.String(),
			Audience:  jwt.ClaimStrings(aud),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        uuid.New().String(),
		},
		OrganizationID: iss.organizationID,
		ClusterID:      iss.clusterID,
	}
}

// sign signs claims with the primary key, stamping its kid so verifiers can
// select the right key during rotation.
func (iss *Issuer) sign(claims WorkloadClaims) (string, error) {
	token := jwt.NewWithClaims(iss.primary.method, claims)
	token.Header["kid"] = iss.primary.kid

	return token.SignedString(iss.primary.private)
}

func (iss *Issuer) DiscoveryDocument() []byte {
	doc := map[string]any{
		"issuer":                                iss.issuerURL,
		"jwks_uri":                              iss.issuerURL + "/.well-known/miren/jwks",
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": iss.supportedAlgs(),
	}

	data, _ := json.Marshal(doc)
	return data
}

// supportedAlgs returns the distinct JWA algorithms across the live signing keys
// and any advertised keys, preserving primary-first order.
func (iss *Issuer) supportedAlgs() []string {
	seen := map[string]bool{}
	var algs []string
	add := func(alg string) {
		if alg != "" && !seen[alg] {
			seen[alg] = true
			algs = append(algs, alg)
		}
	}

	for _, k := range iss.keys {
		add(k.alg)
	}
	for _, jwk := range iss.advertised {
		add(jwk.Algorithm)
	}
	return algs
}

// VerificationKeys returns every public key that may have signed a live token:
// the live signing keys plus any advertised verify-only keys (a demoted legacy
// EdDSA key, or a previous key kept for rotation overlap). Keys are deduplicated
// by ID, primary first.
//
// Callers must not cache the result: it tracks whatever key set the Issuer
// currently holds, which is what keeps verification correct across a rotation.
func (iss *Issuer) VerificationKeys() []jose.JSONWebKey {
	seen := map[string]bool{}
	var keys []jose.JSONWebKey
	add := func(jwk jose.JSONWebKey) {
		if jwk.KeyID == "" || seen[jwk.KeyID] {
			return
		}
		seen[jwk.KeyID] = true
		keys = append(keys, jwk)
	}

	for _, k := range iss.keys {
		add(jwkForPublic(k.public, k.kid))
	}
	for _, jwk := range iss.advertised {
		add(jwk)
	}

	return keys
}

func (iss *Issuer) JWKSDocument() ([]byte, error) {
	return json.Marshal(jose.JSONWebKeySet{Keys: iss.VerificationKeys()})
}

// KeySetFingerprint identifies the current set of verification keys.
//
// It exists so publication to Miren Cloud can be skipped when nothing changed:
// the key set turns over on rotation and otherwise stays put for months, so
// republishing an identical set on every status cycle is pure noise. Sorted
// before hashing so a reordering of the same keys is not mistaken for a
// rotation.
func (iss *Issuer) KeySetFingerprint() string {
	keys := iss.VerificationKeys()

	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, key.KeyID)
	}
	sort.Strings(ids)

	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func loadOrGenerateKey(keyPath string) (*signingKey, error) {
	data, err := os.ReadFile(keyPath)
	if err == nil {
		kp, err := loadSigningKeyFromPEM(string(data))
		if err != nil {
			return nil, fmt.Errorf("loading key file: %w", err)
		}
		if kp.alg != "EdDSA" {
			return kp, nil
		}

		// Legacy EdDSA key: migrate signing to RS256 while keeping the EdDSA key
		// advertised in JWKS for verification. Demote it into the .prev slot
		// (picked up by the rotation-overlap loading in NewIssuer) and generate a
		// fresh RS256 key as the active signing key.
		//
		// Refuse to overwrite an existing .prev: os.Rename clobbers silently on
		// most filesystems, which would discard a key still needed to verify
		// in-flight tokens (e.g. from a prior manual rotation). Require the
		// operator to clear it first.
		prevPath := keyPath + ".prev"
		if _, statErr := os.Stat(prevPath); statErr == nil {
			return nil, fmt.Errorf("demoting legacy EdDSA key: %s already exists; remove it manually before migrating", prevPath)
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("checking for existing previous key %s: %w", prevPath, statErr)
		}
		if err := os.Rename(keyPath, prevPath); err != nil {
			return nil, fmt.Errorf("demoting legacy EdDSA key: %w", err)
		}
		slog.Info("migrated workload identity signing key from EdDSA to RS256; old key retained in JWKS for verification",
			"key", keyPath)
		return generateAndWriteKey(keyPath)
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading key file: %w", err)
	}

	return generateAndWriteKey(keyPath)
}

// generateAndWriteKey creates a new RS256 signing key and persists it to keyPath.
func generateAndWriteKey(keyPath string) (*signingKey, error) {
	kp, err := generateSigningKey()
	if err != nil {
		return nil, err
	}

	pemData, err := kp.privateKeyPEM()
	if err != nil {
		return nil, fmt.Errorf("encoding private key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, fmt.Errorf("creating key directory: %w", err)
	}

	if err := os.WriteFile(keyPath, []byte(pemData), 0600); err != nil {
		return nil, fmt.Errorf("writing key file: %w", err)
	}

	return kp, nil
}
