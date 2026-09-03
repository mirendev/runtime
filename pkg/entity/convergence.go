package entity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mr-tron/base58"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/crypto/blake2b"
)

const (
	convergenceBatchSize        = 100
	convergenceProgressInterval = 1000
)

var errConvergenceBudgetExhausted = errors.New("schema convergence: pass budget exhausted")

// ConvergenceRule describes one historical value that should be rewritten to
// its canonical representation wherever the attribute occurs, including
// inside components.
type ConvergenceRule struct {
	Attribute Id
	From      Value
	To        Value
}

// ConvergencePlan is a deterministic, idempotent set of representation
// rewrites derived from the running schema.
type ConvergencePlan struct {
	Rules []ConvergenceRule
	hash  string
}

// BuildConvergencePlan validates and orders rules, then hashes the resulting
// target. The hash changes only when a stored-value rewrite changes, rather
// than for unrelated schema edits.
func BuildConvergencePlan(rules []ConvergenceRule) (ConvergencePlan, error) {
	rules = slices.Clone(rules)
	for i := range rules {
		rules[i].From = rules[i].From.Clone()
		rules[i].To = rules[i].To.Clone()
		if rules[i].Attribute == "" {
			return ConvergencePlan{}, fmt.Errorf("schema convergence rule has no attribute")
		}
		if rules[i].From.Equal(rules[i].To) {
			return ConvergencePlan{}, fmt.Errorf("schema convergence rule for %s maps a value to itself", rules[i].Attribute)
		}
	}

	slices.SortFunc(rules, func(a, b ConvergenceRule) int {
		if a.Attribute != b.Attribute {
			return strings.Compare(string(a.Attribute), string(b.Attribute))
		}
		if cmp := a.From.Compare(b.From); cmp != 0 {
			return cmp
		}
		return a.To.Compare(b.To)
	})

	compacted := rules[:0]
	for _, rule := range rules {
		if len(compacted) > 0 {
			previous := compacted[len(compacted)-1]
			if previous.Attribute == rule.Attribute && previous.From.Equal(rule.From) {
				if !previous.To.Equal(rule.To) {
					return ConvergencePlan{}, fmt.Errorf("schema convergence rule for %s maps one value to multiple targets", rule.Attribute)
				}
				continue
			}
		}
		compacted = append(compacted, rule)
	}

	h, _ := blake2b.New256(nil)
	for _, rule := range compacted {
		Attr{ID: rule.Attribute, Value: rule.From}.Sum(h)
		h.Write([]byte{0})
		Attr{ID: rule.Attribute, Value: rule.To}.Sum(h)
		h.Write([]byte{0})
	}

	return ConvergencePlan{
		Rules: compacted,
		hash:  base58.Encode(h.Sum(nil)),
	}, nil
}

func (p ConvergencePlan) Hash() string {
	return p.hash
}

type ConvergenceOptions struct {
	StartKey    string
	MaxEntities int
	BatchPause  time.Duration
}

type ConvergenceStats struct {
	EntitiesProcessed int64
	EntitiesRewritten int64
	ValuesRewritten   int64
	EntitiesDeferred  int64
	EntitiesFailed    int64
	Complete          bool
	NextCursor        string
}

// Converge rewrites historical entity representations in a bounded scan. It
// compares each write against the revision that was scanned, so foreground
// updates win and a later pass can reconsider the entity.
func (s *EtcdStore) Converge(ctx context.Context, log *slog.Logger, plan ConvergencePlan, opts ConvergenceOptions) (*ConvergenceStats, error) {
	stats := &ConvergenceStats{}
	byAttribute := make(map[Id][]ConvergenceRule)
	for _, rule := range plan.Rules {
		byAttribute[rule.Attribute] = append(byAttribute[rule.Attribute], rule)
	}

	entityPrefix := fmt.Sprintf("%s/entity/", s.prefix)
	var scanOpts []scanOption
	if opts.StartKey != "" {
		scanOpts = append(scanOpts, withStartKey(opts.StartKey))
		log.Info("schema convergence: resuming entity scan", "cursor", opts.StartKey)
	} else {
		log.Info("schema convergence: starting entity scan")
	}

	scanErr := scanPagedFunc(ctx, s.client, entityPrefix, func(kv *mvccpb.KeyValue) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		key := string(kv.Key)
		if _, ok := entityIDFromKey(log, entityPrefix, key); !ok {
			return nil
		}

		s.convergeEntity(ctx, log, kv, byAttribute, stats)
		stats.NextCursor = key + "\x00"
		stats.EntitiesProcessed++

		if stats.EntitiesProcessed%convergenceProgressInterval == 0 {
			log.Info("schema convergence: progress",
				"processed", stats.EntitiesProcessed,
				"rewritten", stats.EntitiesRewritten)
		}
		if opts.MaxEntities > 0 && stats.EntitiesProcessed >= int64(opts.MaxEntities) {
			return errConvergenceBudgetExhausted
		}
		if opts.BatchPause > 0 && stats.EntitiesProcessed%convergenceBatchSize == 0 {
			select {
			case <-time.After(opts.BatchPause):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}, scanOpts...)

	switch {
	case scanErr == nil:
		stats.Complete = true
		stats.NextCursor = ""
	case errors.Is(scanErr, errConvergenceBudgetExhausted):
		log.Info("schema convergence: pass budget reached, will resume",
			"processed", stats.EntitiesProcessed,
			"cursor", stats.NextCursor)
	default:
		return stats, fmt.Errorf("failed to scan entities for schema convergence: %w", scanErr)
	}

	log.Info("schema convergence: pass finished",
		"complete", stats.Complete,
		"entities_processed", stats.EntitiesProcessed,
		"entities_rewritten", stats.EntitiesRewritten,
		"values_rewritten", stats.ValuesRewritten,
		"entities_deferred", stats.EntitiesDeferred,
		"entities_failed", stats.EntitiesFailed)

	return stats, nil
}

func (s *EtcdStore) convergeEntity(ctx context.Context, log *slog.Logger, kv *mvccpb.KeyValue, rules map[Id][]ConvergenceRule, stats *ConvergenceStats) {
	var original Entity
	if err := decoder.Unmarshal(kv.Value, &original); err != nil {
		log.Warn("schema convergence: failed to decode entity", "key", string(kv.Key), "error", err)
		stats.EntitiesFailed++
		return
	}
	if err := original.postUnmarshal(); err != nil {
		log.Warn("schema convergence: failed to prepare entity", "key", string(kv.Key), "error", err)
		stats.EntitiesFailed++
		return
	}

	rewritten, valuesRewritten := rewriteConvergentAttrs(original.attrs, rules)
	if valuesRewritten == 0 {
		return
	}

	updated := original.Clone()
	updated.attrs = rewritten
	original.SetRevision(kv.ModRevision)
	updated.Remove(Revision)

	if err := s.validator.ValidateUpdate(ctx, updated.attrs, original.attrs); err != nil {
		log.Warn("schema convergence: rewritten entity did not validate", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}

	original.Remove(Revision)
	oldIndexed, err := s.collectIndexedAttributes(ctx, original.attrs)
	if err != nil {
		log.Warn("schema convergence: failed to collect old indexes", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}
	newIndexed, err := s.collectIndexedAttributes(ctx, updated.attrs)
	if err != nil {
		log.Warn("schema convergence: failed to collect new indexes", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}

	collectionOps := s.buildCollectionOps(updated, oldIndexed, newIndexed, "", kv.Lease, kv.Lease != 0)
	uniqueOps, uniqueConditions, err := s.buildUniqueUpdateOps(ctx, updated.Id(), original.Clone(), updated.Clone())
	if err != nil {
		log.Warn("schema convergence: failed to prepare unique values", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}

	primary, session, _, err := s.separateSessionAttributes(ctx, updated.attrs)
	if err != nil {
		log.Warn("schema convergence: failed to separate entity attributes", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}
	if len(session) > 0 {
		log.Warn("schema convergence: refusing to move session attributes from a primary entity", "id", original.Id())
		stats.EntitiesFailed++
		return
	}

	updated.attrs = slices.Clone(primary)
	updated.SetUpdatedAt(time.Now())
	data, err := encoder.Marshal(updated)
	if err != nil {
		log.Warn("schema convergence: failed to serialize entity", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}

	key := string(kv.Key)
	var putOptions []clientv3.OpOption
	if kv.Lease != 0 {
		putOptions = append(putOptions, clientv3.WithLease(clientv3.LeaseID(kv.Lease)))
	}
	entityOps := []clientv3.Op{clientv3.OpPut(key, string(data), putOptions...)}
	entityOps = append(entityOps, collectionOps...)
	entityOps = append(entityOps, uniqueOps...)

	conditions := []clientv3.Cmp{clientv3.Compare(clientv3.ModRevision(key), "=", kv.ModRevision)}
	conditions = append(conditions, uniqueConditions...)
	resp, err := s.client.Txn(ctx).If(conditions...).Then(entityOps...).Commit()
	if err != nil {
		log.Warn("schema convergence: failed to write entity", "id", original.Id(), "error", err)
		stats.EntitiesFailed++
		return
	}
	if !resp.Succeeded {
		log.Debug("schema convergence: entity changed during rewrite, deferring", "id", original.Id())
		stats.EntitiesDeferred++
		return
	}

	stats.EntitiesRewritten++
	stats.ValuesRewritten += int64(valuesRewritten)
}

func rewriteConvergentAttrs(attrs []Attr, rules map[Id][]ConvergenceRule) ([]Attr, int) {
	var rewritten []Attr
	changed := 0

	for i, attr := range attrs {
		next := attr
		attributeChanges := 0

		if attr.Value.Kind() == KindComponent {
			componentAttrs, componentChanges := rewriteConvergentAttrs(attr.Value.Component().attrs, rules)
			if componentChanges > 0 {
				next.Value = ComponentValue(componentAttrs)
				attributeChanges = componentChanges
			}
		} else {
			for _, rule := range rules[attr.ID] {
				if attr.Value.Equal(rule.From) {
					next.Value = rule.To.Clone()
					attributeChanges = 1
					break
				}
			}
		}

		if attributeChanges > 0 {
			if rewritten == nil {
				rewritten = make([]Attr, len(attrs))
				copy(rewritten, attrs[:i])
			}
			rewritten[i] = next
			changed += attributeChanges
		} else if rewritten != nil {
			rewritten[i] = attr
		}
	}

	if changed == 0 {
		return attrs, 0
	}
	return SortedAttrs(rewritten), changed
}
