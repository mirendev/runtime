package rbac

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"miren.dev/runtime/pkg/mapx"
)

// Decision represents an authorization decision
type Decision int

const (
	DecisionDeny Decision = iota
	DecisionAllow
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// Explainer receives information about the evaluation process
type Explainer interface {
	// RuleConsidered is called for each rule that is evaluated
	RuleConsidered(rule *Rule)

	// RuleSkipped is called when a rule is skipped with the reason
	RuleSkipped(rule *Rule, reason string)

	// RuleMatched is called when a rule matches and grants permission
	RuleMatched(rule *Rule, resource string, action string)

	// NoRulesMatched is called when no rules grant the requested permission
	NoRulesMatched()
}

// EvaluateOptions contains options for the Evaluate method
type EvaluateOptions struct {
	Explainer Explainer
}

// EvaluateOption is a functional option for Evaluate
type EvaluateOption func(*EvaluateOptions)

// WithExplainer adds an explainer to track the evaluation process
func WithExplainer(explainer Explainer) EvaluateOption {
	return func(opts *EvaluateOptions) {
		opts.Explainer = explainer
	}
}

// Evaluator evaluates RBAC policies using a PolicyProvider
type Evaluator struct {
	provider PolicyProvider
	logger   *slog.Logger
	cache    *decisionCache
}

// NewEvaluator creates a new RBAC evaluator with a PolicyProvider
func NewEvaluator(ctx context.Context, provider PolicyProvider, logger *slog.Logger) *Evaluator {
	if logger == nil {
		logger = slog.Default()
	}

	return &Evaluator{
		provider: provider,
		logger:   logger.With("module", "rbac-evaluator"),
		cache:    newDecisionCache(ctx),
	}
}

// Evaluate evaluates a request considering groups and tags
func (e *Evaluator) Evaluate(req *Request, opts ...EvaluateOption) Decision {
	// Parse options
	options := &EvaluateOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Check cache first
	if decision, ok := e.cache.get(req); ok {
		return decision
	}

	// Get current policy from provider
	policy := e.provider.GetPolicy()
	if policy == nil {
		e.logger.Warn("no policy available, denying request")
		if options.Explainer != nil {
			options.Explainer.NoRulesMatched()
		}
		return DecisionDeny
	}

	// Evaluate each rule
	for i := range policy.Rules {
		rule := &policy.Rules[i]
		if options.Explainer != nil {
			options.Explainer.RuleConsidered(rule)
		}

		// Check if tags match
		if !rule.MatchesTags(req.Tags) {
			if options.Explainer != nil {
				options.Explainer.RuleSkipped(rule, "tags do not match")
			}
			continue
		}

		// Check if rule applies to the user's groups
		if !rule.AppliesTo(req.Groups) {
			if options.Explainer != nil {
				options.Explainer.RuleSkipped(rule, "groups do not match")
			}
			continue
		}

		// Check if rule grants the requested permission
		if rule.HasPermission(req.Resource, req.Action) {
			if options.Explainer != nil {
				options.Explainer.RuleMatched(rule, req.Resource, req.Action)
			}
			e.logger.Debug("authorization allowed",
				"subject", req.Subject,
				"groups", req.Groups,
				"resource", req.Resource,
				"action", req.Action,
				"rule", rule.Name,
			)
			decision := DecisionAllow
			e.cache.set(req, decision)
			return decision
		} else {
			if options.Explainer != nil {
				options.Explainer.RuleSkipped(rule, fmt.Sprintf("no permission for %s:%s", req.Resource, req.Action))
			}
		}
	}

	// No matching rules found
	if options.Explainer != nil {
		options.Explainer.NoRulesMatched()
	}
	decision := DecisionDeny
	e.cache.set(req, decision)
	e.logger.Debug("authorization denied",
		"subject", req.Subject,
		"groups", req.Groups,
		"resource", req.Resource,
		"action", req.Action,
		"tags", req.Tags,
	)
	return decision
}

// ClearCache clears the decision cache
func (e *Evaluator) ClearCache() {
	e.cache.clear()
}

// Stop stops any background tasks (for compatibility)
func (e *Evaluator) Stop() {
	// No background tasks, cache cleanup is handled by cache itself
}

// decisionCache caches authorization decisions
type decisionCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	decision Decision
	expires  time.Time
}

func newDecisionCache(ctx context.Context) *decisionCache {
	dc := &decisionCache{
		entries: make(map[string]*cacheEntry),
	}
	// Start cleanup goroutine
	go dc.cleanup(ctx)
	return dc
}

func (dc *decisionCache) get(req *Request) (Decision, bool) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	key := dc.requestKey(req)
	entry, ok := dc.entries[key]
	if !ok {
		return DecisionDeny, false
	}

	if time.Now().After(entry.expires) {
		return DecisionDeny, false
	}

	return entry.decision, true
}

func (dc *decisionCache) set(req *Request, decision Decision) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	key := dc.requestKey(req)
	dc.entries[key] = &cacheEntry{
		decision: decision,
		expires:  time.Now().Add(5 * time.Minute),
	}
}

func (dc *decisionCache) clear() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.entries = make(map[string]*cacheEntry)
}

func (dc *decisionCache) requestKey(req *Request) string {
	// Include all relevant fields in cache key
	key := fmt.Sprintf("%s:%s:%s", req.Subject, req.Resource, req.Action)

	sort.Strings(req.Groups)

	// Add groups to key
	for _, g := range req.Groups {
		key += ":" + g
	}

	// Add tags to key
	for k, v := range mapx.StableOrder(req.Tags) {
		key += fmt.Sprintf(":%s=%v", k, v)
	}

	// Add context to key
	for k, v := range mapx.StableOrder(req.Context) {
		key += fmt.Sprintf(":%s=%v", k, v)
	}

	return key
}

func (dc *decisionCache) cleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dc.mu.Lock()
			now := time.Now()
			for key, entry := range dc.entries {
				if now.After(entry.expires) {
					delete(dc.entries, key)
				}
			}
			dc.mu.Unlock()
		}
	}
}
