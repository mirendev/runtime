package cloudauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"miren.dev/runtime/pkg/rbac"
)

// DefaultRefreshInterval is the default interval for refreshing policies
const DefaultRefreshInterval = 60 * time.Second

// PolicyFetcherOptions configures a PolicyFetcher
type PolicyFetcherOptions struct {
	CloudURL        string
	AuthClient      *AuthClient
	Logger          *slog.Logger
	RefreshInterval time.Duration
	HTTPTimeout     time.Duration
}

// PolicyFetcherOption is a functional option for PolicyFetcher
type PolicyFetcherOption func(*PolicyFetcherOptions)

// WithRefreshInterval sets the refresh interval for policy fetching
func WithRefreshInterval(interval time.Duration) PolicyFetcherOption {
	return func(o *PolicyFetcherOptions) {
		o.RefreshInterval = interval
	}
}

// WithHTTPTimeout sets the HTTP client timeout
func WithHTTPTimeout(timeout time.Duration) PolicyFetcherOption {
	return func(o *PolicyFetcherOptions) {
		o.HTTPTimeout = timeout
	}
}

// WithLogger sets the logger
func WithLogger(logger *slog.Logger) PolicyFetcherOption {
	return func(o *PolicyFetcherOptions) {
		o.Logger = logger
	}
}

// PolicyFetcher fetches RBAC policies from miren.cloud
type PolicyFetcher struct {
	cloudURL        string
	authClient      *AuthClient
	httpClient      *http.Client
	logger          *slog.Logger
	refreshInterval time.Duration
	evaluator       *rbac.Evaluator

	mu          sync.RWMutex
	policy      *rbac.Policy
	lastFetched time.Time
	lastError   error
	lastRefresh time.Time

	stop func()
	wg   sync.WaitGroup
}

// NewPolicyFetcher creates a new policy fetcher
func NewPolicyFetcher(cloudURL string, authClient *AuthClient, opts ...PolicyFetcherOption) *PolicyFetcher {
	options := &PolicyFetcherOptions{
		CloudURL:        cloudURL,
		AuthClient:      authClient,
		Logger:          slog.Default(),
		RefreshInterval: DefaultRefreshInterval,
		HTTPTimeout:     30 * time.Second,
	}

	for _, opt := range opts {
		opt(options)
	}

	return &PolicyFetcher{
		cloudURL:   options.CloudURL,
		authClient: options.AuthClient,
		httpClient: &http.Client{
			Timeout: options.HTTPTimeout,
		},
		logger:          options.Logger.With("module", "policy-fetcher"),
		refreshInterval: options.RefreshInterval,
	}
}

// Start begins periodic policy fetching
func (pf *PolicyFetcher) Start(ctx context.Context) error {
	// Fetch initial policy
	if err := pf.fetchPolicy(ctx); err != nil {
		pf.logger.Warn("failed to fetch initial policy", "error", err)
		// Don't fail startup if we can't fetch the policy initially
		// We'll keep trying in the background
	}

	sub, cancel := context.WithCancel(ctx)
	pf.stop = cancel

	pf.logger.Info("starting policy fetcher",
		"cloud-url", pf.cloudURL, "refresh", pf.refreshInterval)

	// Start background refresh goroutine
	pf.wg.Add(1)
	go pf.refreshLoop(sub)

	return nil
}

// Stop stops the policy fetcher
func (pf *PolicyFetcher) Stop() {
	if pf.stop != nil {
		pf.stop()
	}

	pf.wg.Wait()
}

// refreshLoop periodically fetches the policy
func (pf *PolicyFetcher) refreshLoop(ctx context.Context) {
	defer pf.wg.Done()

	ticker := time.NewTicker(pf.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pf.fetchPolicy(ctx); err != nil {
				pf.logger.Warn("failed to refresh policy", "error", err)
			} else {
				pf.logger.Debug("policy refreshed successfully")
			}
		}
	}
}

// fetchPolicy fetches the policy from miren.cloud
func (pf *PolicyFetcher) fetchPolicy(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/self/rbac-rules", pf.cloudURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if we have an auth client
	if pf.authClient != nil {
		token, err := pf.authClient.GetToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get auth token: %w", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "miren-runtime/1.0")

	resp, err := pf.httpClient.Do(req)
	if err != nil {
		pf.setError(err)
		return fmt.Errorf("failed to fetch policy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("failed to fetch policy: status %d: %s", resp.StatusCode, body)
		pf.setError(err)
		return err
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		pf.setError(err)
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Parse the policy
	var policy rbac.Policy
	if err := json.Unmarshal(body, &policy); err != nil {
		pf.setError(err)
		return fmt.Errorf("failed to parse policy: %w", err)
	}

	// Update the stored policy
	pf.setPolicy(&policy)
	pf.logger.Info("policy fetched successfully", "rules", len(policy.Rules))

	// Update refresh timestamp and clear evaluator cache
	pf.mu.Lock()
	pf.lastRefresh = time.Now()
	if pf.evaluator != nil {
		pf.evaluator.ClearCache()
	}
	pf.mu.Unlock()

	return nil
}

// GetPolicy returns the current policy
func (pf *PolicyFetcher) GetPolicy() *rbac.Policy {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	return pf.policy
}

// Fetch performs an immediate, synchronous fetch of the policy
// This is useful for one-time operations like debugging
func (pf *PolicyFetcher) Fetch(ctx context.Context) error {
	return pf.fetchPolicy(ctx)
}

// setPolicy updates the stored policy
func (pf *PolicyFetcher) setPolicy(policy *rbac.Policy) {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	pf.policy = policy
	pf.lastFetched = time.Now()
	pf.lastError = nil
}

// setError records the last error
func (pf *PolicyFetcher) setError(err error) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.lastError = err
}

// RefreshIfNeeded performs an immediate refresh if more than 30 seconds have passed since last refresh
func (pf *PolicyFetcher) RefreshIfNeeded(ctx context.Context) {
	pf.mu.Lock()
	now := time.Now()
	shouldRefresh := now.Sub(pf.lastRefresh) > 30*time.Second
	if shouldRefresh {
		pf.lastRefresh = now
	}
	pf.mu.Unlock()

	if !shouldRefresh {
		// Don't log here, it's too noisy.
		return
	}

	pf.logger.Info("refreshing RBAC rules after rejection")

	go func() {
		if err := pf.fetchPolicy(ctx); err != nil {
			pf.logger.Warn("failed to refresh policy after rejection", "error", err)
		} else {
			pf.logger.Info("successfully refreshed policy after rejection")
			// Clear the evaluator's cache if we have one
			if pf.evaluator != nil {
				pf.evaluator.ClearCache()
			}
		}
	}()
}

// SetEvaluator sets the RBAC evaluator (for cache clearing on refresh)
func (pf *PolicyFetcher) SetEvaluator(evaluator *rbac.Evaluator) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.evaluator = evaluator
}
