package rabbitmq

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

var _ addon.CredentialRotator = (*Provider)(nil)

// rotateCapture carries the RotationResult out of the rotation saga.
type rotateCapture struct {
	Result *addon.RotationResult
}

// RotateCredential implements addon.CredentialRotator for RabbitMQ. A dedicated
// server has a single credential: the `miren` user's password, which the app
// connects with.
//
// RabbitMQ is the outlier. Its password lives inside the node's mnesia database
// (seeded once at first boot from RABBITMQ_DEFAULT_PASS), AMQP has no
// password-change operation, and only the AMQP port is exposed, so there is no
// way to rotate it over the network like the SQL engines. Instead we run
// `rabbitmqctl change_password` inside the running container, record the new
// value on the server entity, and hand the app fresh connection vars to redeploy
// on. rabbitmqctl authenticates to the node via the Erlang cookie, not the user
// password, so a crashed retry converges no matter which password the node
// currently holds (no try-both dance needed).
func (p *Provider) RotateCredential(ctx context.Context, assoc addon.AddonAssociation, credential, newSecret string) (*addon.RotationResult, error) {
	switch credential {
	case "", "user":
		return p.rotateDedicated(ctx, assoc, newSecret)
	default:
		return nil, fmt.Errorf("unknown rabbitmq credential %q (valid: \"user\")", credential)
	}
}

type LoadRotationStateIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
}

type LoadRotationStateOut struct {
	RotatePoolID      entity.Id `saga:"rotatepoolid"`
	RotateOldPassword string    `saga:"rotateoldpassword"`
	RotateServiceHost string    `saga:"rotateservicehost"`
}

func LoadRotationState(ctx context.Context, in LoadRotationStateIn) (LoadRotationStateOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)

	var server addon_v1alpha.RabbitmqServer
	if err := fw.EC.GetById(ctx, in.DedicatedServerID, &server); err != nil {
		return LoadRotationStateOut{}, fmt.Errorf("looking up rabbitmq server: %w", err)
	}
	if server.SandboxPool == "" {
		return LoadRotationStateOut{}, fmt.Errorf("rabbitmq server %s has no sandbox pool", in.DedicatedServerID)
	}

	serviceHost, err := fw.GetServiceAddress(ctx, server.Service)
	if err != nil {
		return LoadRotationStateOut{}, fmt.Errorf("resolving rabbitmq service address: %w", err)
	}

	return LoadRotationStateOut{
		RotatePoolID:      server.SandboxPool,
		RotateOldPassword: server.Password,
		RotateServiceHost: serviceHost,
	}, nil
}

func UndoLoadRotationState(ctx context.Context, in LoadRotationStateIn, out LoadRotationStateOut) error {
	return nil
}

type ChangePasswordIn struct {
	RotatePoolID      entity.Id `saga:"rotatepoolid"`
	RotateNewPassword string    `saga:"rotatenewpassword"`
	RotateOldPassword string    `saga:"rotateoldpassword"`
}

type ChangePasswordOut struct {
	// Gates the entity update on the engine actually changing; passed as data
	// (not a bare edge) so the consumer could verify it if it wanted to.
	PasswordChanged bool `saga:"rmq_password_changed"`
}

func ChangePassword(ctx context.Context, in ChangePasswordIn) (ChangePasswordOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	// The password rides as a rabbitmqctl argv, so it's briefly visible in the
	// container's /proc/<pid>/cmdline. That's inherent to `rabbitmqctl
	// change_password` (which takes no stdin form), and the dedicated broker
	// container is single-tenant, so the exposure is confined to the broker's
	// own process namespace. Accepted rather than worked around.
	if err := fw.ExecInPool(ctx, in.RotatePoolID, "rabbitmqctl", "change_password", defaultUser, in.RotateNewPassword); err != nil {
		return ChangePasswordOut{}, fmt.Errorf("changing rabbitmq password: %w", err)
	}
	return ChangePasswordOut{PasswordChanged: true}, nil
}

func UndoChangePassword(ctx context.Context, in ChangePasswordIn, out ChangePasswordOut) error {
	// An empty RotateOldPassword means there's no prior password to restore
	// (nothing was recorded), so skipping the rollback ALTER is intentional.
	if !out.PasswordChanged || in.RotateOldPassword == "" {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.ExecInPool(ctx, in.RotatePoolID, "rabbitmqctl", "change_password", defaultUser, in.RotateOldPassword)
}

type UpdateServerPasswordIn struct {
	DedicatedServerID entity.Id `saga:"dedicatedserverid"`
	RotateNewPassword string    `saga:"rotatenewpassword"`
	RotateOldPassword string    `saga:"rotateoldpassword"`

	PasswordChanged bool `saga:"rmq_password_changed"`
}

type UpdateServerPasswordOut struct {
	Recorded bool
}

func UpdateServerPassword(ctx context.Context, in UpdateServerPasswordIn) (UpdateServerPasswordOut, error) {
	fw := saga.Get[*addon.ProviderFramework](ctx)
	if err := fw.EC.Patch(ctx, in.DedicatedServerID, 0,
		entity.String(addon_v1alpha.RabbitmqServerPasswordId, in.RotateNewPassword),
	); err != nil {
		return UpdateServerPasswordOut{}, fmt.Errorf("recording new rabbitmq password: %w", err)
	}
	return UpdateServerPasswordOut{Recorded: true}, nil
}

func UndoUpdateServerPassword(ctx context.Context, in UpdateServerPasswordIn, out UpdateServerPasswordOut) error {
	if !out.Recorded {
		return nil
	}
	fw := saga.Get[*addon.ProviderFramework](ctx)
	return fw.EC.Patch(ctx, in.DedicatedServerID, 0,
		entity.String(addon_v1alpha.RabbitmqServerPasswordId, in.RotateOldPassword),
	)
}

type BuildRotationResultIn struct {
	RotateServiceHost string `saga:"rotateservicehost"`
	RotateNewPassword string `saga:"rotatenewpassword"`
}

type BuildRotationResultOut struct {
	Done bool
}

func BuildRotationResult(ctx context.Context, in BuildRotationResultIn) (BuildRotationResultOut, error) {
	rc := saga.Get[*rotateCapture](ctx)
	rc.Result = &addon.RotationResult{
		EnvVars: buildEnvVars(defaultUser, in.RotateNewPassword, in.RotateServiceHost, rabbitmqPort, defaultVhost),
	}
	return BuildRotationResultOut{Done: true}, nil
}

func UndoBuildRotationResult(ctx context.Context, in BuildRotationResultIn, out BuildRotationResultOut) error {
	return nil
}

// RegisterRotateDedicatedSaga registers the dedicated RabbitMQ rotation saga.
func RegisterRotateDedicatedSaga(registry *saga.Registry, fw *addon.ProviderFramework, rc *rotateCapture) error {
	return saga.Define("rotate-dedicated-rabbitmq").
		Using(fw).
		Using(rc).
		Action(DecodeDedicatedAttrs).Undo(UndoDecodeDedicatedAttrs).
		Action(LoadRotationState).Undo(UndoLoadRotationState).
		Action(ChangePassword).Undo(UndoChangePassword).
		Action(UpdateServerPassword).Undo(UndoUpdateServerPassword).
		Action(BuildRotationResult).Undo(UndoBuildRotationResult).
		RegisterTo(registry)
}

func (p *Provider) rotateDedicated(ctx context.Context, assoc addon.AddonAssociation, newSecret string) (*addon.RotationResult, error) {
	p.Log.Info("rotating dedicated RabbitMQ credential", "assoc", assoc.ID)

	rc := &rotateCapture{}
	registry := saga.NewRegistry()
	if err := RegisterRotateDedicatedSaga(registry, p.Fw, rc); err != nil {
		return nil, fmt.Errorf("registering rotate saga: %w", err)
	}

	executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
	if err := executor.Start("rotate-dedicated-rabbitmq").
		Input("assocentity", assoc.Entity).
		Input("rotatenewpassword", newSecret).
		Execute(ctx); err != nil {
		return nil, err
	}
	if rc.Result == nil {
		return nil, fmt.Errorf("rotation saga completed but no result was captured")
	}
	return rc.Result, nil
}
