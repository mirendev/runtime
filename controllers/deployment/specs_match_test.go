package deployment

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity/types"
)

// structFingerprint recursively builds a string from a type's field names and
// their types, so that adding a field at any depth changes the hash.
func structFingerprint(t reflect.Type) string {
	var walk func(reflect.Type) string
	walk = func(t reflect.Type) string {
		//exhaustive:ignore reflect.Kind has ~27 members; default handles the rest
		switch t.Kind() {
		case reflect.Slice, reflect.Array, reflect.Pointer:
			return t.Kind().String() + "<" + walk(t.Elem()) + ">"
		case reflect.Map:
			return "map<" + walk(t.Key()) + "," + walk(t.Elem()) + ">"
		case reflect.Struct:
			var parts []string
			for f := range t.Fields() {
				parts = append(parts, f.Name+":"+walk(f.Type))
			}
			return "struct{" + strings.Join(parts, ",") + "}"
		default:
			return t.String()
		}
	}
	h := sha256.Sum256([]byte(walk(t)))
	return fmt.Sprintf("%x", h[:8])
}

// TestSpecsMatchCoversAllFields is a tripwire that fires when fields are added
// to or removed from SandboxSpec or any of its nested structs. When this test
// fails, update specsMatch to handle the new field (or explicitly skip it),
// then update the expected hash here.
func TestSpecsMatchCoversAllFields(t *testing.T) {
	assert.Equal(t, "be8d0ed7171ec698", structFingerprint(reflect.TypeFor[compute_v1alpha.SandboxSpec]()),
		"SandboxSpec struct tree changed — update specsMatch and this hash")
}

func baseSpec() *compute_v1alpha.SandboxSpec {
	return &compute_v1alpha.SandboxSpec{
		Container: []compute_v1alpha.SandboxSpecContainer{
			{
				Name:    "web",
				Image:   "app:v1",
				Command: "./start",
				Env:     []string{"FOO=bar"},
				Port: []compute_v1alpha.SandboxSpecContainerPort{
					{Port: 8080, Name: "http"},
				},
			},
		},
	}
}

func TestSpecsMatch(t *testing.T) {
	tests := []struct {
		name       string
		modify     func(a, b *compute_v1alpha.SandboxSpec)
		wantMatch  bool
		wantReason string // substring to check in reason when !wantMatch
	}{
		{
			name:      "identical specs match",
			modify:    func(a, b *compute_v1alpha.SandboxSpec) {},
			wantMatch: true,
		},
		{
			name: "different images do not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				b.Container[0].Image = "app:v2"
			},
			wantMatch:  false,
			wantReason: "image mismatch",
		},
		{
			name: "different args do not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Container[0].Args = []string{"serve", "--port", "3000"}
				b.Container[0].Args = []string{"serve", "--port", "4000"}
			},
			wantMatch:  false,
			wantReason: "args mismatch",
		},
		{
			name: "shutdown timeout change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Container[0].ShutdownTimeout = "5s"
				b.Container[0].ShutdownTimeout = "30s"
			},
			wantMatch:  false,
			wantReason: "shutdown timeout mismatch",
		},
		{
			name: "different versions are ignored",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Version = "ver-1"
				b.Version = "ver-2"
			},
			wantMatch: true,
		},

		// Volume tests — these capture the bug in MIR-944
		{
			name: "adding a volume does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/data", SizeGb: 10},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "removing a volume does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/data", SizeGb: 10},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "same volumes match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				vol := compute_v1alpha.SandboxSpecVolume{
					Name: "data", Provider: "miren", MountPath: "/miren/data", SizeGb: 5,
				}
				a.Volume = []compute_v1alpha.SandboxSpecVolume{vol}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{vol}
			},
			wantMatch: true,
		},
		{
			name: "volume size change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/miren/data", SizeGb: 5},
				}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/miren/data", SizeGb: 10},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "volume provider change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/data", SizeGb: 10},
				}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/data", SizeGb: 10},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "volume mount path change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/data"},
				}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/mnt/data"},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "volume read-only change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/data"},
				}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "local", MountPath: "/data", ReadOnly: true},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "volume owner change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/data"},
				}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/data", Owner: "keep"},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "volume label change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/data", Labels: types.Labels{{Key: "env", Value: "prod"}}},
				}
				b.Volume = []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/data", Labels: types.Labels{{Key: "env", Value: "staging"}}},
				}
			},
			wantMatch:  false,
			wantReason: "volume",
		},
		{
			name: "multiple volumes same match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				vols := []compute_v1alpha.SandboxSpecVolume{
					{Name: "data", Provider: "miren", MountPath: "/miren/data", SizeGb: 5},
					{Name: "cache", Provider: "local", MountPath: "/cache", SizeGb: 1},
				}
				a.Volume = vols
				b.Volume = vols
			},
			wantMatch: true,
		},

		// Container mount tests — also part of MIR-944
		{
			name: "adding a container mount does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				b.Container[0].Mount = []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "data", Destination: "/data"},
				}
			},
			wantMatch:  false,
			wantReason: "mount",
		},
		{
			name: "removing a container mount does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Container[0].Mount = []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "data", Destination: "/data"},
				}
			},
			wantMatch:  false,
			wantReason: "mount",
		},
		{
			name: "same container mounts match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				mnt := []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "data", Destination: "/data"},
				}
				a.Container[0].Mount = mnt
				b.Container[0].Mount = mnt
			},
			wantMatch: true,
		},
		{
			name: "container mount destination change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Container[0].Mount = []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "data", Destination: "/data"},
				}
				b.Container[0].Mount = []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "data", Destination: "/mnt/data"},
				}
			},
			wantMatch:  false,
			wantReason: "mount",
		},
		{
			name: "container mount source change does not match",
			modify: func(a, b *compute_v1alpha.SandboxSpec) {
				a.Container[0].Mount = []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "data", Destination: "/data"},
				}
				b.Container[0].Mount = []compute_v1alpha.SandboxSpecContainerMount{
					{Source: "cache", Destination: "/data"},
				}
			},
			wantMatch:  false,
			wantReason: "mount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := baseSpec()
			b := baseSpec()
			tt.modify(a, b)

			reason, match := specsMatch(a, b)
			assert.Equal(t, tt.wantMatch, match, "specsMatch result")
			if !tt.wantMatch {
				assert.Contains(t, reason, tt.wantReason, "mismatch reason")
			} else {
				assert.Empty(t, reason, "reason should be empty on match")
			}
		})
	}
}

// A pool whose spec differs only in restart policy must not be reused: the
// policy decides whether a vanished container is rebooted or the sandbox is
// retired, which is the difference between a task run executing once and
// executing twice.
func TestSpecsMatchDetectsRestartPolicyChange(t *testing.T) {
	a := baseSpec()
	b := baseSpec()
	b.RestartPolicy = compute_v1alpha.SandboxSpecNEVER

	reason, ok := specsMatch(a, b)
	assert.False(t, ok)
	assert.Contains(t, reason, "restart policy")
}

// Stdin and Tty are fixed by containerd when the task is created and cannot be
// added to a running container, so a spec that differs in either needs new
// containers rather than a reused pool.
func TestSpecsMatchDetectsStdinAndTtyChanges(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		a := baseSpec()
		b := baseSpec()
		b.Container[0].Stdin = true

		reason, ok := specsMatch(a, b)
		assert.False(t, ok)
		assert.Contains(t, reason, "stdin")
	})

	t.Run("tty", func(t *testing.T) {
		a := baseSpec()
		b := baseSpec()
		b.Container[0].Tty = true

		reason, ok := specsMatch(a, b)
		assert.False(t, ok)
		assert.Contains(t, reason, "tty")
	})
}
