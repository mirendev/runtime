// Package certrevocation serves client certificate revocation to operators.
package certrevocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"miren.dev/runtime/api/certrevocation/certrevocation_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

type Server struct {
	log         *slog.Logger
	ec          *aes.Client
	revocations *rpc.CertRevocations
}

var _ certrevocation_v1alpha.CertRevocations = (*Server)(nil)

func NewServer(log *slog.Logger, ec *aes.Client, revocations *rpc.CertRevocations) *Server {
	return &Server{log: log.With("module", "cert-revocations"), ec: ec, revocations: revocations}
}

// allowOperator gates this service on identity, because the authorizer waves
// every certificate through and runner certificates must not qualify. JWT callers
// are additionally subject to cloud RBAC at the transport.
func allowOperator(ctx context.Context) (*rpc.Identity, error) {
	id := rpc.IdentityFromContext(ctx)
	if id == nil || (id.Method != rpc.AuthMethodCert && id.Method != rpc.AuthMethodJWT) || rpc.BoundApp(ctx) != "" {
		return nil, fmt.Errorf("%w: certificate revocation requires a cluster operator", rpc.ErrUnauthorized)
	}
	if id.Method == rpc.AuthMethodCert && (id.Subject == "miren-runner" || strings.HasPrefix(id.Subject, "runner-")) {
		return nil, fmt.Errorf("%w: certificate revocation requires a cluster operator", rpc.ErrUnauthorized)
	}
	return id, nil
}

func (s *Server) Revoke(ctx context.Context, state *certrevocation_v1alpha.CertRevocationsRevoke) error {
	id, err := allowOperator(ctx)
	if err != nil {
		return err
	}

	args := state.Args()
	serial, err := rpc.ParseCertSerial(args.Serial())
	if err != nil {
		return err
	}
	if serial == rpc.CallerCertSerial(ctx) {
		return fmt.Errorf("refusing to revoke certificate %s: this command is running with it; reissue first, then revoke from the new certificate", serial)
	}

	results := state.Results()

	var existing core_v1alpha.CertificateRevocation
	err = s.ec.Get(ctx, serial, &existing)
	switch {
	case err == nil:
		// The set may not have heard yet if the entity arrived some other way.
		s.revocations.Add(serial)
		results.SetRevocation(revocationInfo(&existing))
		results.SetAlreadyRevoked(true)
		return nil
	case !errors.Is(err, cond.ErrNotFound{}):
		return fmt.Errorf("checking for an existing revocation: %w", err)
	}

	revocation := &core_v1alpha.CertificateRevocation{
		Serial:    serial,
		RevokedAt: time.Now().UTC(),
		RevokedBy: id.Subject,
		Reason:    args.Reason(),
	}
	if _, err := s.ec.Create(ctx, serial, revocation); err != nil {
		return fmt.Errorf("storing revocation: %w", err)
	}

	// Write through rather than wait for the watch, so the certificate is
	// refused by the time this call returns.
	s.revocations.Add(serial)
	s.log.Info("client certificate revoked", "serial", serial, "revoked_by", id.Subject, "reason", args.Reason())

	results.SetRevocation(revocationInfo(revocation))
	results.SetAlreadyRevoked(false)
	return nil
}

func (s *Server) List(ctx context.Context, state *certrevocation_v1alpha.CertRevocationsList) error {
	if _, err := allowOperator(ctx); err != nil {
		return err
	}

	listed, err := s.ec.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindCertificateRevocation))
	if err != nil {
		return fmt.Errorf("listing revocations: %w", err)
	}

	var stored []*core_v1alpha.CertificateRevocation
	for listed.Next() {
		var revocation core_v1alpha.CertificateRevocation
		if err := listed.Read(&revocation); err != nil {
			return err
		}
		stored = append(stored, &revocation)
	}
	slices.SortFunc(stored, func(a, b *core_v1alpha.CertificateRevocation) int {
		return a.RevokedAt.Compare(b.RevokedAt)
	})

	infos := make([]*certrevocation_v1alpha.Revocation, 0, len(stored))
	for _, revocation := range stored {
		infos = append(infos, revocationInfo(revocation))
	}
	state.Results().SetRevocations(infos)
	return nil
}

func revocationInfo(r *core_v1alpha.CertificateRevocation) *certrevocation_v1alpha.Revocation {
	var info certrevocation_v1alpha.Revocation
	info.SetSerial(r.Serial)
	info.SetRevokedAt(r.RevokedAt.Format(time.RFC3339))
	info.SetRevokedBy(r.RevokedBy)
	info.SetReason(r.Reason)
	return &info
}
