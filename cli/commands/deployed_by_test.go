package commands

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

func TestDeployedByJSON(t *testing.T) {
	t.Run("nobody recorded omits the key", func(t *testing.T) {
		assert.Nil(t, deployedByOf(&deployment_v1alpha.DeploymentInfo{}))
		assert.Equal(t, "-", formatUser(&deployment_v1alpha.DeploymentInfo{}))
	})

	t.Run("cloud user", func(t *testing.T) {
		dep := &deployment_v1alpha.DeploymentInfo{}
		dep.SetDeployedBySubject("usr-ada")
		dep.SetDeployedByAuthMethod("jwt")
		dep.SetDeployedByEmail("ada@example.com")
		dep.SetDeployedByName("Ada Lovelace")

		out, err := json.Marshal(deployedByOf(dep))
		require.NoError(t, err)
		assert.JSONEq(t, `{"subject":"usr-ada","auth_method":"jwt","email":"ada@example.com","name":"Ada Lovelace","display":"Ada Lovelace"}`, string(out))
		assert.Equal(t, "Ada Lovelace", formatUser(dep))
	})

	t.Run("ci deploy", func(t *testing.T) {
		dep := &deployment_v1alpha.DeploymentInfo{}
		dep.SetDeployedBySubject("repo:acme/web:ref:refs/heads/main")
		dep.SetDeployedByAuthMethod("oidc")

		out, err := json.Marshal(deployedByOf(dep))
		require.NoError(t, err)
		assert.JSONEq(t, `{"subject":"repo:acme/web:ref:refs/heads/main","auth_method":"oidc","display":"github:acme/web@main"}`, string(out))
	})
}
