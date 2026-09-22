package certificate

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/singleflight"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/pkg/entity"
)

const (
	// How long to suppress ACME retries for a domain after a failure. Matches
	// autocert.Manager's internal createCertRetryAfter so we don't retry before
	// the Manager has cleaned up its failed state. Server-sent Retry-After
	// values override this when longer.
	acmeFailureCooldown = time.Minute

	// Max time to wait for autocert before falling back to the self-signed cert.
	// The upstream autocert.Manager uses a hardcoded 5-minute timeout internally;
	// this shorter deadline lets TLS handshakes complete promptly with the fallback.
	inlineGetCertTimeout = 10 * time.Second

	// Max time to wait for autocert during eager provisioning in Reconcile.
	// Longer than inlineGetCertTimeout since we're not blocking a TLS handshake,
	// but much shorter than the Manager's internal 5-minute timeout so we don't
	// wedge the controller or prevent graceful shutdown.
	reconcileGetCertTimeout = 30 * time.Second

	// Bounds on remembered HostChecker answers. Every TLS handshake for a name
	// that isn't itself a route consults the checker, and scanners send a
	// steady stream of junk SNI, so answers are cached. The caches are keyed
	// by client-supplied names and so must stay bounded. A refusal is kept
	// briefly because it can go stale in a way users see: a preview deploy
	// that comes up after something probed its name. A stale approval costs
	// nothing, since the certificate already exists.
	hostDecisionCacheSize = 4096
	hostAllowTTL          = 5 * time.Minute
	hostDenyTTL           = 30 * time.Second

	// Max HostChecker calls in flight at once. Each one can make a route's app
	// do work (and wake it from zero), so a scan of random names must not be
	// able to fan out without limit. Names over the cap are refused for now
	// and not remembered.
	maxConcurrentHostChecks = 16

	// Max time a handshake waits on the HostChecker before treating the name
	// as not allowed. The checker may take longer (asking a cold app can wait
	// on a sandbox boot), in which case its answer still lands in the cache
	// for the next handshake.
	defaultHostCheckTimeout = 6 * time.Second
)

// HostChecker decides whether a name that is not itself a route, but sits one
// label under one, should get an on-demand certificate. It is supplied by the
// ingress, which knows about ephemeral deploys and can ask the route's app. A
// non-nil error means the answer is unknown.
type HostChecker func(ctx context.Context, host string) (bool, error)

// AutocertController provisions TLS certificates eagerly using HTTP-01 ACME challenges
// via autocert.Manager. It watches http_route entities and triggers cert provisioning
// when routes are created, rather than waiting for the first TLS handshake.
type AutocertController struct {
	log              *slog.Logger
	eac              *entityserver_v1alpha.EntityAccessClient
	mgr              *autocert.Manager
	dataPath         string
	email            string
	fallbackCert     tls.Certificate
	allowedHosts     sync.Map      // domain -> struct{}
	ready            chan struct{} // closed when port-80 ACME challenge server is up
	publicIPs        func() []net.IP
	failures         sync.Map // domain -> acmeFailure
	clusterHostnames map[string]struct{}

	hostChecker      atomic.Pointer[HostChecker]
	hostAllowed      *expirable.LRU[string, struct{}]
	hostDenied       *expirable.LRU[string, struct{}]
	hostChecks       singleflight.Group
	hostCheckSlots   chan struct{}
	hostCheckTimeout time.Duration

	// routeChecks remembers each route's host and tls_check so Reconcile can
	// tell a real change from a periodic resync.
	routeChecks sync.Map // entity.Id -> routeCheck

	// hostGen counts invalidations of the answer caches. A check records its answer
	// only if no invalidation happened while it was in flight, so an answer
	// computed against a route's old tls_check can't outlive the purge.
	hostGenMu sync.Mutex
	hostGen   uint64
}

// acmeFailure records when an ACME attempt failed and how long to wait before
// retrying. The cooldown is at least acmeFailureCooldown, but if the ACME
// server returned a Retry-After header we honor that instead.
type acmeFailure struct {
	when     time.Time
	cooldown time.Duration
}

type AutocertControllerOpts struct {
	Log      *slog.Logger
	EAC      *entityserver_v1alpha.EntityAccessClient
	DataPath string
	Email    string

	// PublicIPs, if non-nil, is called before eager provisioning to verify DNS
	// points to this cluster; when nil the check is skipped.
	PublicIPs func() []net.IP

	// ClusterHostnames are hostnames that should always have valid TLS
	// certificates, independent of any HttpRoute entities (e.g., a
	// cloud-provisioned *.miren.systems address).
	ClusterHostnames []string
}

func NewAutocertController(opts AutocertControllerOpts) *AutocertController {
	pinned := make(map[string]struct{}, len(opts.ClusterHostnames))
	for _, h := range opts.ClusterHostnames {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			pinned[h] = struct{}{}
		}
	}
	return &AutocertController{
		log:              opts.Log.With("module", "autocert-controller"),
		eac:              opts.EAC,
		dataPath:         opts.DataPath,
		email:            opts.Email,
		ready:            make(chan struct{}),
		publicIPs:        opts.PublicIPs,
		clusterHostnames: pinned,
		hostAllowed:      expirable.NewLRU[string, struct{}](hostDecisionCacheSize, nil, hostAllowTTL),
		hostDenied:       expirable.NewLRU[string, struct{}](hostDecisionCacheSize, nil, hostDenyTTL),
		hostCheckSlots:   make(chan struct{}, maxConcurrentHostChecks),
		hostCheckTimeout: defaultHostCheckTimeout,
	}
}

// SetHostChecker installs the function that vouches for names under a route.
// Until one is set, such names get the fallback certificate.
func (c *AutocertController) SetHostChecker(fn HostChecker) {
	c.hostChecker.Store(&fn)
	c.forgetHostDecisions()
}

// forgetHostDecisions drops every remembered HostChecker answer and any answer
// still in flight.
func (c *AutocertController) forgetHostDecisions() {
	c.hostGenMu.Lock()
	defer c.hostGenMu.Unlock()
	c.hostGen++
	c.hostAllowed.Purge()
	c.hostDenied.Purge()
}

// routeCheck is the part of a route that decides answers about names under it.
type routeCheck struct {
	host     string
	tlsCheck string
}

// Init implements ReconcileControllerI — creates the autocert.Manager and loads the fallback cert.
func (c *AutocertController) Init(ctx context.Context) error {
	certsDir := filepath.Join(c.dataPath, "certs")

	fallbackCert, err := autotls.LoadOrGenerateFallbackCert(certsDir)
	if err != nil {
		return err
	}
	c.fallbackCert = fallbackCert
	c.log.Info("loaded fallback self-signed certificate for unconfigured hosts")

	c.mgr = &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(certsDir),
		Email:  c.email,
		HostPolicy: func(ctx context.Context, host string) error {
			if c.isAllowedHost(ctx, strings.ToLower(host)) {
				return nil
			}
			return fmt.Errorf("host %q not in allowed set", host)
		},
	}

	// Pre-populate allowedHosts from existing http_route entities so the
	// isAllowedHost guard in GetCertificate works immediately — before the
	// controller manager starts reconciling routes one by one.
	if err := c.loadExistingRoutes(ctx); err != nil {
		c.log.Warn("failed to pre-populate allowed hosts from existing routes", "error", err)
	}

	// Add cluster-level hostnames (e.g., cloud-provisioned *.miren.systems)
	// to allowedHosts so TLS handshakes succeed immediately. These are pinned
	// and never removed by route deletion.
	for h := range c.clusterHostnames {
		c.allowedHosts.Store(h, struct{}{})
	}
	if len(c.clusterHostnames) > 0 {
		c.log.Info("added cluster hostnames to allowed hosts", "count", len(c.clusterHostnames))
		go c.provisionClusterHostnames(ctx)
	}

	c.log.Info("autocert controller initialized")
	return nil
}

// loadExistingRoutes queries all http_route entities and adds their hosts to allowedHosts.
func (c *AutocertController) loadExistingRoutes(ctx context.Context) error {
	if c.eac == nil {
		return nil
	}
	res, err := c.eac.List(ctx, entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute))
	if err != nil {
		return fmt.Errorf("failed to list http_route entities: %w", err)
	}

	count := 0
	for _, ent := range res.Values() {
		var route ingress_v1alpha.HttpRoute
		route.Decode(ent.Entity())
		domain := strings.ToLower(strings.TrimSpace(route.Host))
		if domain != "" {
			c.allowedHosts.Store(domain, struct{}{})
			count++
		}
	}

	if count > 0 {
		c.log.Info("pre-populated allowed hosts from existing routes", "count", count)
	}
	return nil
}

// Reconcile implements ReconcileControllerI — adds the route's domain to allowedHosts
// and eagerly provisions a TLS certificate via autocert.
func (c *AutocertController) Reconcile(ctx context.Context, route *ingress_v1alpha.HttpRoute, meta *entity.Meta) error {
	domain := strings.ToLower(strings.TrimSpace(route.Host))
	routeID := meta.Id()
	if domain == "" {
		c.log.Warn("http_route has empty host, skipping certificate provisioning", "route", routeID)
		return nil
	}

	c.allowedHosts.Store(domain, struct{}{})

	// A new route, or one whose host or tls_check changed, invalidates earlier
	// answers about names under it. A resync of an unchanged route doesn't.
	current := routeCheck{host: domain, tlsCheck: route.TlsCheck}
	if prev, ok := c.routeChecks.Swap(routeID, current); !ok || prev.(routeCheck) != current {
		c.forgetHostDecisions()
	}

	log := c.log.With("domain", domain, "route", routeID)

	// Wildcard routes (*.example.com) can't be eagerly provisioned — HTTP-01 can't
	// issue wildcard certs and we don't know which subdomains will be requested.
	// Subdomains provision inline, but only once the HostChecker vouches for them.
	if strings.HasPrefix(domain, "*.") {
		log.Info("wildcard route: subdomains will provision certs inline when the route's tls_check or an ephemeral deploy vouches for them")
		return nil
	}

	// Check DNS before attempting ACME to avoid wasting rate-limited authorizations
	// on domains that don't resolve to this cluster yet (e.g., during DNS migration).
	if c.publicIPs != nil {
		if !c.dnsPointsToUs(domain) {
			log.Info("skipping eager cert provisioning: DNS does not point to this cluster (will provision inline when DNS propagates)")
			return nil
		}
	}

	// Skip ACME if this domain recently failed, matching the guard in GetCertificate.
	// Without this, every Reconcile resync would fire a new ACME attempt even while
	// rate-limited.
	if c.inCooldown(domain) {
		log.Debug("skipping eager provisioning: domain in failure cooldown")
		return nil
	}

	// Wait for port-80 ACME challenge server to be ready before attempting provisioning
	select {
	case <-c.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := c.eagerProvision(ctx, domain); err != nil {
		return err
	}

	return nil
}

// provisionClusterHostnames eagerly provisions TLS certificates for cluster-level
// hostnames. It waits for the port-80 ACME challenge server before attempting
// provisioning, matching the pattern used by Reconcile for route-based certs.
func (c *AutocertController) provisionClusterHostnames(ctx context.Context) {
	select {
	case <-c.ready:
	case <-ctx.Done():
		return
	}

	for domain := range c.clusterHostnames {
		if ctx.Err() != nil {
			return
		}

		log := c.log.With("domain", domain, "source", "cluster-hostname")

		if c.publicIPs != nil {
			if len(c.publicIPs()) == 0 {
				log.Info("skipping eager cert provisioning: cluster has no public IPs")
				continue
			}
			if !c.dnsPointsToUs(domain) {
				log.Info("skipping eager cert provisioning: DNS does not point to this cluster")
				continue
			}
		}

		if c.inCooldown(domain) {
			log.Debug("skipping eager provisioning: domain in failure cooldown")
			continue
		}

		if err := c.eagerProvision(ctx, domain); err != nil {
			return
		}
	}
}

// eagerProvision attempts to provision a TLS certificate for the given domain
// with a timeout, recording failures for cooldown. Returns a non-nil error only
// when the context is cancelled.
func (c *AutocertController) eagerProvision(ctx context.Context, domain string) error {
	hello := &tls.ClientHelloInfo{
		ServerName:        domain,
		SupportedVersions: []uint16{tls.VersionTLS13, tls.VersionTLS12},
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
		SupportedCurves: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		SignatureSchemes: []tls.SignatureScheme{
			tls.ECDSAWithP256AndSHA256,
			tls.ECDSAWithP384AndSHA384,
			tls.PSSWithSHA256,
			tls.PSSWithSHA384,
			tls.PKCS1WithSHA256,
			tls.PKCS1WithSHA384,
		},
	}

	type certResult struct {
		cert *tls.Certificate
		err  error
	}
	ch := make(chan certResult, 1)
	go func() {
		cert, err := c.mgr.GetCertificate(hello)
		ch <- certResult{cert, err}
	}()

	log := c.log.With("domain", domain)

	select {
	case res := <-ch:
		if res.err != nil {
			c.recordFailure(domain, res.err)
			log.Warn("eager cert provisioning failed (will retry on next TLS handshake)", "error", res.err)
		} else {
			c.failures.Delete(domain)
			log.Info("certificate provisioned successfully")
		}
	case <-time.After(reconcileGetCertTimeout):
		c.recordFailure(domain, nil)
		log.Warn("eager cert provisioning timed out", "timeout", reconcileGetCertTimeout)
	case <-ctx.Done():
		c.recordFailure(domain, nil)
		return ctx.Err()
	}

	return nil
}

// Delete implements DeletingReconcileController — removes the domain from allowedHosts
// only if no other http_route entities reference the same host.
func (c *AutocertController) Delete(ctx context.Context, id entity.Id) error {
	// The deleted entity is gone, so we can't read its host directly.
	// Scan allowedHosts and for each domain, check if any routes still exist.
	// This is simple and correct; the set is small (number of unique domains).
	c.allowedHosts.Range(func(key, _ any) bool {
		domain := key.(string)
		res, err := c.eac.List(ctx, entity.String(ingress_v1alpha.HttpRouteHostId, domain))
		if err != nil {
			c.log.Warn("failed to query routes for domain during delete", "domain", domain, "error", err)
			return true // keep iterating, leave domain in set (safe default)
		}
		if len(res.Values()) == 0 {
			if _, pinned := c.clusterHostnames[domain]; !pinned {
				c.allowedHosts.Delete(domain)
				c.log.Debug("removed domain from allowed hosts (no remaining routes)", "domain", domain)
			}
		}
		return true
	})
	c.routeChecks.Delete(id)
	c.forgetHostDecisions()
	return nil
}

// GetCertificate implements autotls.CertificateProvider — returns a cert from autocert,
// falling back to the self-signed cert on any error or timeout.
func (c *AutocertController) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := strings.ToLower(hello.ServerName)

	if !c.isAllowedHost(hello.Context(), host) {
		return &c.fallbackCert, nil
	}

	// Skip ACME if this domain recently failed — prevents rapid-fire attempts
	// from every incoming TLS handshake while rate-limited.
	if c.inCooldown(host) {
		return &c.fallbackCert, nil
	}

	// Run autocert with a deadline so handshakes fall back to the self-signed
	// cert promptly instead of blocking for the upstream 5-minute timeout.
	type certResult struct {
		cert *tls.Certificate
		err  error
	}
	ch := make(chan certResult, 1)
	go func() {
		cert, err := c.mgr.GetCertificate(hello)
		ch <- certResult{cert, err}
	}()

	select {
	case res := <-ch:
		if res.err == nil {
			return res.cert, nil
		}
		c.recordFailure(host, res.err)
		c.log.Debug("autocert failed, using fallback", "host", host, "error", res.err)
	case <-time.After(inlineGetCertTimeout):
		c.recordFailure(host, nil)
		c.log.Warn("autocert timed out, using fallback", "host", host, "timeout", inlineGetCertTimeout)
	}

	return &c.fallbackCert, nil
}

// HTTPHandler returns an http.Handler that serves ACME HTTP-01 challenge responses,
// delegating non-challenge requests to the provided fallback handler.
func (c *AutocertController) HTTPHandler(fallback http.Handler) http.Handler {
	return c.mgr.HTTPHandler(fallback)
}

// dnsPointsToUs resolves domain and checks if any of the returned IPs match
// one of the cluster's known public IPs. Returns true if there's a match, or
// if public IPs are unavailable (fail open so we don't block provisioning
// when netcheck hasn't run yet).
func (c *AutocertController) dnsPointsToUs(domain string) bool {
	ips := c.publicIPs()
	if len(ips) == 0 {
		return true // no known IPs — skip the check
	}

	addrs, err := net.LookupHost(domain)
	if err != nil {
		return false
	}

	for _, addr := range addrs {
		resolved := net.ParseIP(addr)
		if resolved == nil {
			continue
		}
		if slices.ContainsFunc(ips, resolved.Equal) {
			return true
		}
	}
	return false
}

// isAllowedHost reports whether host should get a certificate. A name that a
// route (or a cluster hostname) names exactly is always allowed. A name one
// label under a route, through a wildcard ("foo.example.com" under
// "*.example.com") or as an ephemeral subdomain ("pr-33.app.example.com" under
// "app.example.com"), is allowed only if the HostChecker vouches for it.
//
// The shape of a name is not enough on its own: wildcard DNS resolves every
// label to the cluster, so a scanner that sends arbitrary SNI would otherwise
// get a real certificate for each label it tries (MIR-1919).
func (c *AutocertController) isAllowedHost(ctx context.Context, host string) bool {
	if _, ok := c.allowedHosts.Load(host); ok {
		return true
	}
	idx := strings.IndexByte(host, '.')
	if idx <= 0 {
		return false
	}
	parent := host[idx+1:]
	_, underWildcard := c.allowedHosts.Load("*." + parent)
	_, underRoute := c.allowedHosts.Load(parent)
	if !underWildcard && !underRoute {
		return false
	}
	return c.checkHost(ctx, host)
}

// checkHost asks the HostChecker about host, remembering the answer for a
// while and collapsing concurrent handshakes for the same name into one ask.
func (c *AutocertController) checkHost(ctx context.Context, host string) bool {
	if _, ok := c.hostAllowed.Get(host); ok {
		return true
	}
	if _, ok := c.hostDenied.Get(host); ok {
		return false
	}

	fn := c.hostChecker.Load()
	if fn == nil {
		return false
	}

	c.hostGenMu.Lock()
	gen := c.hostGen
	c.hostGenMu.Unlock()

	// The generation is part of the key so a handshake arriving after an
	// invalidation starts a fresh check instead of joining a stale one.
	key := strconv.FormatUint(gen, 10) + "/" + host
	ch := c.hostChecks.DoChan(key, func() (any, error) {
		select {
		case c.hostCheckSlots <- struct{}{}:
			defer func() { <-c.hostCheckSlots }()
		default:
			c.log.Debug("too many host checks in flight; serving fallback", "host", host)
			return false, nil
		}

		// Detached from any one handshake's context: other handshakes may be
		// waiting on this same answer.
		checkCtx, cancel := context.WithTimeout(context.Background(), c.hostCheckTimeout)
		defer cancel()

		allowed, err := c.callHostChecker(checkCtx, *fn, host)
		if err != nil {
			c.log.Warn("could not decide whether to issue a certificate; serving fallback", "host", host, "error", err)
			return false, nil
		}

		c.hostGenMu.Lock()
		if c.hostGen == gen {
			if allowed {
				c.hostAllowed.Add(host, struct{}{})
			} else {
				c.hostDenied.Add(host, struct{}{})
			}
		}
		c.hostGenMu.Unlock()

		c.log.Debug("host check decided", "host", host, "allowed", allowed)
		return allowed, nil
	})

	if ctx == nil {
		ctx = context.Background()
	}
	// The checker may not honor its deadline, so the handshake enforces its own.
	timer := time.NewTimer(c.hostCheckTimeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.Val.(bool)
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// recordFailure stores a cooldown entry for a domain. If the error is an ACME
// rate-limit with a Retry-After duration, we honor that instead of our default.
func (c *AutocertController) recordFailure(domain string, err error) {
	cooldown := acmeFailureCooldown
	if err != nil {
		if ra, ok := acme.RateLimit(err); ok && ra > cooldown {
			cooldown = ra
		}
	}
	c.failures.Store(domain, acmeFailure{when: time.Now(), cooldown: cooldown})
}

// inCooldown reports whether a domain is in the failure cooldown window.
// Returns true if retries should be suppressed. Cleans up expired entries.
func (c *AutocertController) inCooldown(domain string) bool {
	v, ok := c.failures.Load(domain)
	if !ok {
		return false
	}
	f := v.(acmeFailure)
	if time.Since(f.when) < f.cooldown {
		return true
	}
	c.failures.Delete(domain)
	return false
}

// callHostChecker runs fn, turning a panic into an error. The checker drives
// the ingress's proxy path from a singleflight goroutine, with no http.Server
// above it to recover, and singleflight re-panics where nothing can catch it,
// so an unrecovered panic here (ReverseProxy panics with http.ErrAbortHandler
// on a broken copy) would take down the whole process.
func (c *AutocertController) callHostChecker(ctx context.Context, fn HostChecker, host string) (allowed bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			allowed, err = false, fmt.Errorf("host checker panicked: %v", r)
		}
	}()
	return fn(ctx, host)
}

// SetReady signals that the port-80 ACME challenge server is up and accepting connections.
// This unblocks Reconcile calls that are waiting to provision certificates.
func (c *AutocertController) SetReady() {
	select {
	case <-c.ready:
		// Already closed
	default:
		close(c.ready)
	}
}
