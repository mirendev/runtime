package commands

import (
	"testing"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

func TestRollbackVersionIDs(t *testing.T) {
	depInfo := func(versionID, shortID string) *deployment_v1alpha.DeploymentInfo {
		d := &deployment_v1alpha.DeploymentInfo{}
		if versionID != "" {
			d.SetAppVersionId(versionID)
		}
		if shortID != "" {
			d.SetAppVersionShortId(shortID)
		}
		return d
	}

	tests := []struct {
		name        string
		dep         *deployment_v1alpha.DeploymentInfo
		selected    string
		wantDeploy  string
		wantDisplay string
	}{
		{
			// MIR-1579: the server carries the app's addon bindings onto the
			// selected version, which means activating a *derived* version. The
			// health wait has to follow that one — waiting on the selected id
			// would hang until it reported "never became active".
			name:        "derived version is what gets awaited",
			dep:         depInfo("myapp-vDerived", "d3r1v"),
			selected:    "myapp-vOld",
			wantDeploy:  "myapp-vDerived",
			wantDisplay: "d3r1v",
		},
		{
			name:        "verbatim activation awaits the selected version",
			dep:         depInfo("myapp-vOld", "0ld"),
			selected:    "myapp-vOld",
			wantDeploy:  "myapp-vOld",
			wantDisplay: "0ld",
		},
		{
			name:        "full id is displayed when no short id is set",
			dep:         depInfo("myapp-vDerived", ""),
			selected:    "myapp-vOld",
			wantDeploy:  "myapp-vDerived",
			wantDisplay: "myapp-vDerived",
		},
		{
			// An older server may return a deployment naming no version.
			name:        "empty version falls back to the selection",
			dep:         depInfo("", ""),
			selected:    "myapp-vOld",
			wantDeploy:  "myapp-vOld",
			wantDisplay: "myapp-vOld",
		},
		{
			name:        "nil deployment falls back to the selection",
			dep:         nil,
			selected:    "myapp-vOld",
			wantDeploy:  "myapp-vOld",
			wantDisplay: "myapp-vOld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployed, display := rollbackVersionIDs(tt.dep, tt.selected)
			if deployed != tt.wantDeploy {
				t.Errorf("deployed = %q, want %q", deployed, tt.wantDeploy)
			}
			if display != tt.wantDisplay {
				t.Errorf("display = %q, want %q", display, tt.wantDisplay)
			}
		})
	}
}
