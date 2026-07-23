package rabbitmq_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/rabbitmq"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/testserver"
)

// TestRabbitMQ_Rotation_Integration provisions a real RabbitMQ, rotates the
// `miren` user's password via rabbitmqctl inside the container, and proves the
// rotation at the auth layer: rabbitmqctl authenticate_user accepts the new
// password and rejects the old one, and the new value is recorded on the server
// entity. RabbitMQ has no AMQP-level password change and no client library here,
// so this exercises the exec-based path end to end.
func TestRabbitMQ_Rotation_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if err := diskio.EnsureLoopDevices(slog.Default()); err != nil {
		t.Skip("skipping integration test: loop devices not available:", err)
	}

	require.NoError(t, testserver.TestServer(t))
	time.Sleep(5 * time.Second)

	ctx := t.Context()
	log := testutils.TestDebugLogger(t)

	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	require.NoError(t, err)

	client, err := rs.Connect("localhost:8443", "entities")
	require.NoError(t, err)

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(log, eac)
	fw := addon.NewProviderFramework(log, ec, eac, saga.NewMemoryStorage())

	// RabbitMQ rotation runs rabbitmqctl inside the container, so the framework
	// needs an exec client (the coordinator wires this at startup; here we do it
	// by hand against the same exec service).
	execConn, err := rs.Connect("localhost:8443", "dev.miren.runtime/exec")
	require.NoError(t, err)
	fw.Exec = exec_v1alpha.NewSandboxExecClient(execConn)

	provider := rabbitmq.NewProvider(fw)

	prov, err := provider.Provision(ctx, addon.App{Name: "rmqrot-app"}, addon.Variant{Name: "small"})
	require.NoError(t, err)
	require.NotNil(t, prov)

	provEnv := make(map[string]string)
	for _, v := range prov.EnvVars {
		provEnv[v.Key] = v.Value
	}
	oldPassword := provEnv["RABBITMQ_PASSWORD"]
	require.NotEmpty(t, oldPassword)

	assocID, err := ec.Create(ctx, idgen.GenNS("addon-assoc"), &addon_v1alpha.AddonAssociation{
		Variant: "small",
		Status:  "active",
	})
	require.NoError(t, err)
	require.NoError(t, ec.Patch(ctx, assocID, 0, prov.Attrs...))
	resp, err := eac.Get(ctx, assocID.String())
	require.NoError(t, err)
	rawAssoc := resp.Entity().Entity()

	var data addon_v1alpha.RabbitmqDedicatedData
	data.Decode(rawAssoc)
	require.NotEmpty(t, data.RabbitmqServer)

	var serverBefore addon_v1alpha.RabbitmqServer
	require.NoError(t, ec.GetById(ctx, data.RabbitmqServer, &serverBefore))
	require.Equal(t, oldPassword, serverBefore.Password)
	poolID := serverBefore.SandboxPool
	require.NotEmpty(t, poolID)

	// authOK reports whether the miren user authenticates with the given password
	// (rabbitmqctl authenticate_user exits non-zero on a bad password).
	authOK := func(password string) bool {
		return fw.ExecInPool(ctx, poolID, "rabbitmqctl", "authenticate_user", "miren", password) == nil
	}

	// Wait until the Erlang node is up and accepts the provisioned credential;
	// rabbitmqctl becomes usable a little after the AMQP port binds.
	require.Eventually(t, func() bool {
		return authOK(oldPassword)
	}, 120*time.Second, 5*time.Second, "rabbitmq should accept the provisioned credential")

	const newSecret = "rotated-rmq-secret"
	res, err := provider.RotateCredential(ctx,
		addon.AddonAssociation{ID: assocID, Variant: "small", Entity: rawAssoc}, "user", newSecret)
	require.NoError(t, err)
	require.NotNil(t, res)

	rotEnv := make(map[string]string)
	for _, v := range res.EnvVars {
		rotEnv[v.Key] = v.Value
	}
	assert.Equal(t, newSecret, rotEnv["RABBITMQ_PASSWORD"], "result should carry the new password")

	var serverAfter addon_v1alpha.RabbitmqServer
	require.NoError(t, ec.GetById(ctx, data.RabbitmqServer, &serverAfter))
	assert.Equal(t, newSecret, serverAfter.Password, "entity should record the new password")

	// Auth-layer proof: the new password works, the old one is rejected.
	assert.True(t, authOK(newSecret), "new password should authenticate")
	assert.False(t, authOK(oldPassword), "old password should be rejected")
}
