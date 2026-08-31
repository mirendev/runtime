package core_v1alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func TestNamedEnumSharesTypeAndCanonicalWrites(t *testing.T) {
	inline := Disks{Provider: DiskProviderLocal}
	inlineAttr, ok := entity.New(inline.Encode()).Get(DisksProviderId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, inlineAttr.Value.Kind())
	assert.Equal(t, DiskProviderLocalMemberId, inlineAttr.Value.Id())

	component := ConfigSpecServicesDisks{Provider: DiskProviderLocal}
	componentAttr, ok := entity.New(component.Encode()).Get(ConfigSpecServicesDisksProviderId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, componentAttr.Value.Kind())
	assert.Equal(t, DiskProviderLocalMemberId, componentAttr.Value.Id())

	var decodedInline Disks
	decodedInline.Decode(entity.New(entity.Ref(DisksProviderId, DisksProviderLocalId)))
	var decodedComponent ConfigSpecServicesDisks
	// The component-specific ID remains a read alias for entities written by an
	// older runtime.
	decodedComponent.Decode(entity.New(entity.Ref(
		ConfigSpecServicesDisksProviderId,
		ConfigSpecServicesDisksProviderLocalId,
	)))

	assert.Equal(t, DiskProviderLocal, decodedInline.Provider)
	assert.Equal(t, decodedInline.Provider, decodedComponent.Provider)

	// Both fields also understand the canonical member identity.
	decodedInline.Decode(entity.New(entity.Ref(DisksProviderId, DiskProviderLocalMemberId)))
	decodedComponent.Decode(entity.New(entity.Ref(ConfigSpecServicesDisksProviderId, DiskProviderLocalMemberId)))
	assert.Equal(t, DiskProviderLocal, decodedInline.Provider)
	assert.Equal(t, DiskProviderLocal, decodedComponent.Provider)
}

func TestNamedEnumPreservesExistingCoreMemberIDs(t *testing.T) {
	for _, tt := range []struct {
		name     string
		memberID entity.Id
		want     DiskProvider
	}{
		{"miren", "dev.miren.core/provider.miren", DiskProviderMiren},
		{"local", "dev.miren.core/provider.local", DiskProviderLocal},
		{"sqlite", "dev.miren.core/provider.sqlite", DiskProviderSqlite},
	} {
		t.Run("disk_provider_"+tt.name, func(t *testing.T) {
			var disk Disks
			disk.Decode(entity.New(entity.Ref(DisksProviderId, tt.memberID)))
			assert.Equal(t, tt.want, disk.Provider)

			encoded, ok := entity.New(disk.Encode()).Get(DisksProviderId)
			require.True(t, ok)
			assert.Equal(t, tt.memberID, encoded.Value.Id())
		})
	}

	for _, tt := range []struct {
		name     string
		memberID entity.Id
		want     PortProtocol
	}{
		{"tcp", "dev.miren.core/protocol.tcp", PortProtocolTcp},
		{"udp", "dev.miren.core/protocol.udp", PortProtocolUdp},
	} {
		t.Run("port_protocol_"+tt.name, func(t *testing.T) {
			var port Ports
			port.Decode(entity.New(entity.Ref(PortsProtocolId, tt.memberID)))
			assert.Equal(t, tt.want, port.Protocol)

			encoded, ok := entity.New(port.Encode()).Get(PortsProtocolId)
			require.True(t, ok)
			assert.Equal(t, tt.memberID, encoded.Value.Id())
		})
	}
}
