package storage_v1alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
)

func TestFilesystemEnumWritesCanonicalRefsAndReadsLegacyStrings(t *testing.T) {
	disk := Disk{Filesystem: DiskFilesystemExt4}
	diskAttr, ok := entity.New(disk.Encode()).Get(DiskFilesystemId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, diskAttr.Value.Kind())
	assert.Equal(t, DiskFilesystemExt4Id, diskAttr.Value.Id())

	volume := DiskVolume{Filesystem: DiskFilesystemExt4}
	volumeAttr, ok := entity.New(volume.Encode()).Get(DiskVolumeFilesystemId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, volumeAttr.Value.Kind())
	assert.Equal(t, DiskFilesystemExt4MemberId, volumeAttr.Value.Id())

	var decoded DiskVolume
	decoded.Decode(entity.New(
		entity.Ref(entity.DBId, "disk_volume/test"),
		entity.String(DiskVolumeFilesystemId, "xfs"),
	))
	assert.Equal(t, DiskFilesystemXfs, decoded.Filesystem)
	legacyAttr, ok := entity.New(decoded.Encode()).Get(DiskVolumeFilesystemId)
	require.True(t, ok)
	assert.Equal(t, DiskFilesystemXfsMemberId, legacyAttr.Value.Id())

	decoded.Decode(entity.New(
		entity.Ref(entity.DBId, "disk_volume/test"),
		entity.Ref(DiskVolumeFilesystemId, DiskFilesystemBtrfsMemberId),
	))
	assert.Equal(t, DiskFilesystemBtrfs, decoded.Filesystem)
}

func TestFilesystemEnumInstallsCanonicalMembersWithCompatibleSchemas(t *testing.T) {
	store := entity.NewMockStore()
	sb := schema.Builder("storage_named_enum_physical_types", "v1")
	(&Disk{}).InitSchema(sb)
	(&DiskVolume{}).InitSchema(sb)
	require.NoError(t, sb.Apply(t.Context(), store))
	for _, member := range []entity.Id{
		DiskFilesystemExt4MemberId,
		DiskFilesystemXfsMemberId,
		DiskFilesystemBtrfsMemberId,
	} {
		_, err := store.GetEntity(t.Context(), member)
		require.NoError(t, err, "canonical enum member %s was not installed", member)
	}

	diskSchema, err := store.GetAttributeSchema(t.Context(), DiskFilesystemId)
	require.NoError(t, err)
	assert.Equal(t, entity.TypeEnum, diskSchema.Type)
	assert.Equal(t, entity.TypeRef, diskSchema.ElemType)

	volumeSchema, err := store.GetAttributeSchema(t.Context(), DiskVolumeFilesystemId)
	require.NoError(t, err)
	assert.Equal(t, entity.TypeEnum, volumeSchema.Type)
	assert.Equal(t, entity.TypeRef, volumeSchema.ElemType)
	require.Len(t, volumeSchema.EnumValues, 3)

	// The natural model writes the canonical identity while retaining the old
	// string as a read alias and convergence source.
	require.NoError(t, schema.Apply(t.Context(), store))
	cache, err := entity.NewSchemaCache(store)
	require.NoError(t, err)
	kindSchema, err := cache.GetKindSchema(t.Context(), KindDiskVolume)
	require.NoError(t, err)
	field := kindSchema.GetField("filesystem")
	require.NotNil(t, field)
	assert.Equal(t, "enum", field.Type)
	assert.Equal(t, "ref", field.EnumEncoding)
	assert.Equal(t, "string", field.EnumLegacyEncoding)
	assert.Equal(t, "dev.miren.storage/enum.disk_filesystem", field.Enum)
	canonical, ok := field.EnumValue("ext4")
	require.True(t, ok)
	assert.Equal(t, DiskFilesystemExt4MemberId, canonical.Id())
	assert.Equal(t, DiskFilesystemExt4MemberId, field.EnumValues["ext4"])
	member, ok := field.EnumMember(entity.StringValue("ext4"))
	require.True(t, ok)
	assert.Equal(t, "ext4", member)
}

func TestFilesystemEnumConvergencePlanRewritesLegacyString(t *testing.T) {
	plan, err := schema.ConvergencePlan()
	require.NoError(t, err)

	found := false
	for _, rule := range plan.Rules {
		if rule.Attribute == DiskVolumeFilesystemId &&
			rule.From.Equal(entity.StringValue("ext4")) &&
			rule.To.Equal(entity.RefValue(DiskFilesystemExt4MemberId)) {
			found = true
			break
		}
	}
	assert.True(t, found, "encoded enum aliases should produce a convergence rule")
}
