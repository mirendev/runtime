package entityserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Client struct {
	log       *slog.Logger
	eac       *entityserver_v1alpha.EntityAccessClient
	sessionId string
}

func NewClient(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Client {
	return &Client{
		log: log,
		eac: eac,
	}
}

// EAC returns the underlying EntityAccessClient.
func (c *Client) EAC() *entityserver_v1alpha.EntityAccessClient {
	return c.eac
}

type SchemaEncoder interface {
	ShortKind() string
	Kind() entity.Id
	Encode() []entity.Attr
	Decode(e entity.AttrGetter)
}

type SchemaEncoderWithId interface {
	SchemaEncoder
	EntityId() entity.Id
}

func (c *Client) Get(ctx context.Context, name string, sc SchemaEncoder) error {
	ret, err := c.eac.Get(ctx, sc.ShortKind()+"/"+name)
	if err != nil {
		return err
	}

	sc.Decode(ret.Entity().Entity())
	return nil
}

func (c *Client) GetById(ctx context.Context, id entity.Id, sc SchemaEncoder) error {
	ret, err := c.eac.Get(ctx, id.String())
	if err != nil {
		return err
	}

	sc.Decode(ret.Entity().Entity())
	return nil
}

func (c *Client) GetByIdWithEntity(ctx context.Context, id entity.Id, sc SchemaEncoder) (*entityserver_v1alpha.Entity, error) {
	ret, err := c.eac.Get(ctx, id.String())
	if err != nil {
		return nil, err
	}

	sc.Decode(ret.Entity().Entity())
	return ret.Entity(), nil
}

// GetWithEntity is like Get but also returns the raw entity so callers can read
// its revision for a revision-guarded (optimistic-concurrency) Patch.
func (c *Client) GetWithEntity(ctx context.Context, name string, sc SchemaEncoder) (*entityserver_v1alpha.Entity, error) {
	ret, err := c.eac.Get(ctx, sc.ShortKind()+"/"+name)
	if err != nil {
		return nil, err
	}

	sc.Decode(ret.Entity().Entity())
	return ret.Entity(), nil
}

type ListResults struct {
	values []*entity.Entity
	cur    *entity.Entity
	len    int
}

func (l *ListResults) Next() bool {
	if len(l.values) == 0 {
		return false
	}

	l.cur = l.values[0]
	l.values = l.values[1:]

	return true
}

func (l *ListResults) Read(sc SchemaEncoder) error {
	if l.cur == nil {
		return fmt.Errorf("no more values")
	}

	sc.Decode(l.cur)
	return nil
}

func (l *ListResults) Metadata() *core_v1alpha.Metadata {
	if l.cur == nil {
		return nil
	}

	var md core_v1alpha.Metadata
	md.Decode(l.cur)

	return &md
}

func (l *ListResults) Entity() *entity.Entity {
	if l.cur == nil {
		return nil
	}

	return l.cur
}

func (l *ListResults) Length() int {
	return l.len
}

func (c *Client) List(ctx context.Context, index entity.Attr) (*ListResults, error) {
	ret, err := c.eac.List(ctx, index)
	if err != nil {
		return nil, err
	}

	var lr ListResults

	for _, v := range ret.Values() {
		lr.values = append(lr.values, v.Entity())
		lr.len++
	}

	return &lr, nil
}

func (c *Client) OneAtIndex(ctx context.Context, index entity.Attr, sc SchemaEncoder) error {
	ret, err := c.eac.List(ctx, index)
	if err != nil {
		return err
	}

	if len(ret.Values()) == 0 {
		return cond.NotFound("entity", index)
	}

	if len(ret.Values()) > 1 {
		return cond.Conflict("entity", "more than one entity found")
	}

	sc.Decode(ret.Values()[0].Entity())

	return nil
}

type createOp struct {
	labels types.Labels
}

type CreateOptions func(o *createOp)

func WithLabels(labels types.Labels) CreateOptions {
	return func(o *createOp) {
		o.labels = labels
	}
}

func (c *Client) Create(ctx context.Context, name string, sc SchemaEncoder, opts ...CreateOptions) (entity.Id, error) {
	var op createOp
	for _, opt := range opts {
		opt(&op)
	}

	var rpcE entityserver_v1alpha.Entity

	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name:   name,
			Labels: op.labels,
		}).Encode,
		sc.Encode,
		entity.Ident, types.Keyword(sc.ShortKind()+"/"+name),
	).Attrs())

	if c.sessionId != "" {
		pr, err := c.eac.PutSession(ctx, &rpcE, c.sessionId)
		if err != nil {
			return "", err
		}

		return entity.Id(pr.Id()), nil
	}

	pr, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return "", err
	}

	return entity.Id(pr.Id()), nil
}

// CreateOrReplace creates a new entity or fully replaces an existing one.
// Unlike CreateOrUpdate which merges attrs (appending to "many" component
// fields), this method replaces all attrs atomically when the entity exists.
// Note: not safe for concurrent use on the same entity — the Replace will
// fail with a revision mismatch if another writer updates between Get and
// Replace. This is fine for startup-time initialization but callers needing
// concurrent safety should add retry logic.
func (c *Client) CreateOrReplace(ctx context.Context, name string, sc SchemaEncoder, opts ...CreateOptions) (entity.Id, error) {
	var op createOp
	for _, opt := range opts {
		opt(&op)
	}

	gr, err := c.eac.Get(ctx, sc.ShortKind()+"/"+name)
	if err == nil {
		// Entity exists — build full attrs including metadata and identity,
		// then replace atomically.
		fullAttrs := entity.New(
			(&core_v1alpha.Metadata{
				Name:   name,
				Labels: op.labels,
			}).Encode,
			sc.Encode,
			entity.DBId, entity.Id(gr.Entity().Id()),
			entity.Ident, types.Keyword(sc.ShortKind()+"/"+name),
		).Attrs()

		rr, err := c.eac.Replace(ctx, fullAttrs, gr.Entity().Revision())
		if err != nil {
			return "", err
		}

		return entity.Id(rr.Id()), nil
	}

	if !errors.Is(err, cond.ErrNotFound{}) {
		return "", err
	}

	// Entity does not exist — create it.
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(
		entity.New(
			(&core_v1alpha.Metadata{
				Name:   name,
				Labels: op.labels,
			}).Encode,
			sc.Encode,
			entity.Ident, types.Keyword(sc.ShortKind()+"/"+name),
		).Attrs())

	pr, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return "", err
	}

	return entity.Id(pr.Id()), nil
}

func (c *Client) CreateOrUpdate(ctx context.Context, name string, sc SchemaEncoder, opts ...CreateOptions) (entity.Id, error) {
	var op createOp
	for _, opt := range opts {
		opt(&op)
	}

	var rpcE entityserver_v1alpha.Entity

	gr, err := c.eac.Get(ctx, sc.ShortKind()+"/"+name)
	if err == nil {
		rpcE.SetId(gr.Entity().Id())
		rpcE.SetAttrs(sc.Encode())
	} else {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return "", err
		}
		rpcE.SetAttrs(
			entity.New(
				(&core_v1alpha.Metadata{
					Name:   name,
					Labels: op.labels,
				}).Encode,
				sc.Encode,
				entity.Ident, types.Keyword(sc.ShortKind()+"/"+name),
			).Attrs())
	}

	if c.sessionId != "" {
		pr, err := c.eac.PutSession(ctx, &rpcE, c.sessionId)
		if err != nil {
			return "", err
		}

		return entity.Id(pr.Id()), nil
	}

	pr, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return "", err
	}

	return entity.Id(pr.Id()), nil
}

func (c *Client) Update(ctx context.Context, sc SchemaEncoderWithId) error {
	id := sc.EntityId()

	if id == "" {
		return fmt.Errorf("entity id is empty")
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(string(id))
	rpcE.SetAttrs(sc.Encode())

	if c.sessionId != "" {
		_, err := c.eac.PutSession(ctx, &rpcE, c.sessionId)
		if err != nil {
			return err
		}

		return nil
	}

	_, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) UpdateAttrs(ctx context.Context, id entity.Id, attrs ...any) error {
	var rpcE2 entityserver_v1alpha.Entity
	rpcE2.SetId(string(id))
	rpcE2.SetAttrs(entity.New(attrs...).Attrs())

	if c.sessionId != "" {
		_, err := c.eac.PutSession(ctx, &rpcE2, c.sessionId)
		if err != nil {
			return err
		}

		return nil
	}

	_, err := c.eac.Put(ctx, &rpcE2)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) Delete(ctx context.Context, id entity.Id) error {
	_, err := c.eac.Delete(ctx, id.String())
	if err != nil {
		return err
	}

	return nil
}

// Patch updates specific attributes on an entity without replacing the entire entity.
// Pass revision 0 to skip optimistic concurrency control, or a specific revision to
// ensure the entity hasn't been modified since it was read.
func (c *Client) Patch(ctx context.Context, id entity.Id, revision int64, attrs ...entity.Attr) error {
	allAttrs := append([]entity.Attr{entity.Ref(entity.DBId, id)}, attrs...)
	_, err := c.eac.Patch(ctx, allAttrs, revision)
	return err
}

func (c *Client) GetAttributesByTag(ctx context.Context, tag string) (*entityserver_v1alpha.EntityAccessClientGetAttributesByTagResults, error) {
	return c.eac.GetAttributesByTag(ctx, tag)
}

func (c *Client) WatchEntity(ctx context.Context, id entity.Id) chan *entity.Entity {
	ch := make(chan *entity.Entity, 1)

	go func() {
		c.eac.WatchEntity(ctx, id.String(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if op.HasEntity() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case ch <- op.Entity().Entity():
					// ok
				}
			} else {
				close(ch)
			}

			return nil
		}))
	}()

	return ch
}

type Session struct {
	c  *Client
	id string

	mu sync.Mutex

	cancel context.CancelFunc

	// dead is closed once the keepalive loop concludes the lease is
	// unrecoverable (e.g. the coordinator/etcd that granted it restarted).
	// The owner selects on Dead() to re-establish a fresh session.
	dead     chan struct{}
	deadOnce sync.Once
}

const defaultTTL = 60

// Dead returns a channel that is closed when the session's lease has been lost
// and cannot be pinged back to life. Callers should re-establish a new session
// (via NewSession) rather than continuing to use this one.
func (l *Session) Dead() <-chan struct{} {
	return l.dead
}

func (l *Session) markDead() {
	l.deadOnce.Do(func() { close(l.dead) })
}

// isLeaseGone reports whether err indicates the etcd lease backing the session
// no longer exists, meaning the session is unrecoverable and must be rebuilt.
// The error crosses an RPC boundary and arrives as a flattened string, so we
// match on the underlying etcd message ("requested lease not found") rather
// than a typed sentinel.
func isLeaseGone(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "lease not found")
}

type pingAction int

const (
	pingOK    pingAction = iota // lease renewed; session healthy
	pingRetry                   // transient failure; keep trying
	pingDead                    // lease unrecoverable; rebuild the session
)

// evalPing classifies the result of one keepalive ping. firstFailure is the
// time of the first failure in the current run of consecutive failures (zero
// if the previous ping succeeded); it returns the updated firstFailure to carry
// forward. A definitive "lease not found" is fatal immediately; other errors
// are treated as transient until they persist longer than the lease TTL, past
// which the lease has certainly expired regardless of what the error says.
func evalPing(err error, firstFailure, now time.Time, ttl time.Duration) (pingAction, time.Time) {
	if err == nil {
		return pingOK, time.Time{}
	}
	if isLeaseGone(err) {
		return pingDead, firstFailure
	}
	if firstFailure.IsZero() {
		firstFailure = now
	}
	if now.Sub(firstFailure) > ttl {
		return pingDead, firstFailure
	}
	return pingRetry, firstFailure
}

// Grant creates a new lease with the given TTL
func (c *Client) NewSession(ctx context.Context, usage string) (*Session, *Client, error) {
	ttl := int64(defaultTTL)
	ret, err := c.eac.CreateSession(ctx, ttl, usage)
	if err != nil {
		return nil, nil, err
	}

	sc := &Client{
		eac:       c.eac,
		sessionId: ret.Id(),
	}

	ctx, cancel := context.WithCancel(ctx)

	session := &Session{
		c:  c,
		id: ret.Id(),

		cancel: cancel,

		dead: make(chan struct{}),
	}

	go func() {
		defer c.log.Debug("session closed", "id", session.id)

		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.Revoke(ctx)
		}()

		leaseTTL := time.Duration(ttl) * time.Second
		ticker := time.NewTicker(leaseTTL / 2)
		defer ticker.Stop()

		// firstFailure is the time of the first ping failure in the current
		// run of consecutive failures; zero when the last ping succeeded.
		var firstFailure time.Time

		for {
			select {
			case <-ticker.C:
				// Bound each ping so a hung request can't stall the ticker and
				// freeze dead-detection; a stuck ping simply times out and is
				// treated as a failure by evalPing.
				pingCtx, pingCancel := context.WithTimeout(ctx, leaseTTL/2)
				err := session.Ping(pingCtx)
				pingCancel()

				var action pingAction
				action, firstFailure = evalPing(err, firstFailure, time.Now(), leaseTTL)

				switch action {
				case pingDead:
					c.log.Warn("session lease lost, signaling for re-establish",
						"id", session.id, "error", err)
					session.markDead()
					// Release the session's context; this goroutine owns it and
					// is exiting, and nothing re-pings a dead session.
					cancel()
					return
				case pingRetry:
					c.log.Error("failed to ping session", "error", err)
				case pingOK:
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return session, sc, nil
}

func (l *Session) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancel != nil {
		l.cancel()
	}

	if l.id == "" {
		return nil
	}

	_, err := l.c.eac.RevokeSession(context.Background(), l.id)
	if err != nil {
		return err
	}

	l.id = ""
	return nil
}

// Revoke revokes the lease
func (l *Session) Revoke(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.c.eac.RevokeSession(ctx, l.id)
	if err != nil {
		return err
	}

	l.id = ""

	return nil
}

// Assert keeps the lease alive
func (l *Session) Ping(ctx context.Context) error {
	_, err := l.c.eac.PingSession(ctx, l.id)
	if err != nil {
		return err
	}

	return nil
}
