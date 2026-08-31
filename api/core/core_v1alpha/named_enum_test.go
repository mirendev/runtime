package core_v1alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func TestNamedEnumSharesTypeWhilePreservingFieldEncoding(t *testing.T) {
	inline := Disks{Provider: DiskProviderLocal}
	inlineAttr, ok := entity.New(inline.Encode()).Get(DisksProviderId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, inlineAttr.Value.Kind())
	assert.Equal(t, DisksProviderLocalId, inlineAttr.Value.Id())

	component := ConfigSpecServicesDisks{Provider: DiskProviderLocal}
	componentAttr, ok := entity.New(component.Encode()).Get(ConfigSpecServicesDisksProviderId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, componentAttr.Value.Kind())
	assert.Equal(t, ConfigSpecServicesDisksProviderLocalId, componentAttr.Value.Id())

	var decodedInline Disks
	decodedInline.Decode(entity.New(entity.Ref(DisksProviderId, DisksProviderLocalId)))
	var decodedComponent ConfigSpecServicesDisks
	decodedComponent.Decode(entity.New(entity.Ref(
		ConfigSpecServicesDisksProviderId,
		ConfigSpecServicesDisksProviderLocalId,
	)))

	assert.Equal(t, DiskProviderLocal, decodedInline.Provider)
	assert.Equal(t, decodedInline.Provider, decodedComponent.Provider)
}
