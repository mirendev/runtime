package storage_v1alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
)

func TestFilesystemEnumSpansRefAndStringEncodings(t *testing.T) {
	disk := Disk{Filesystem: DiskFilesystemExt4}
	diskAttr, ok := entity.New(disk.Encode()).Get(DiskFilesystemId)
	require.True(t, ok)
	assert.Equal(t, entity.KindId, diskAttr.Value.Kind())
	assert.Equal(t, DiskFilesystemExt4Id, diskAttr.Value.Id())

	volume := DiskVolume{Filesystem: DiskFilesystemExt4}
	volumeAttr, ok := entity.New(volume.Encode()).Get(DiskVolumeFilesystemId)
	require.True(t, ok)
	assert.Equal(t, entity.KindString, volumeAttr.Value.Kind())
	assert.Equal(t, "ext4", volumeAttr.Value.String())

	var decoded DiskVolume
	decoded.Decode(entity.New(
		entity.Ref(entity.DBId, "disk_volume/test"),
		entity.String(DiskVolumeFilesystemId, "xfs"),
	))
	assert.Equal(t, DiskFilesystemXfs, decoded.Filesystem)
}

func TestFilesystemEnumPreservesPhysicalAttributeSchemaTypes(t *testing.T) {
	store := entity.NewMockStore()
	sb := schema.Builder("storage_named_enum_physical_types", "v1")
	(&Disk{}).InitSchema(sb)
	(&DiskVolume{}).InitSchema(sb)
	require.NoError(t, sb.Apply(t.Context(), store))

	diskSchema, err := store.GetAttributeSchema(t.Context(), DiskFilesystemId)
	require.NoError(t, err)
	assert.Equal(t, entity.TypeRef, diskSchema.Type)

	volumeSchema, err := store.GetAttributeSchema(t.Context(), DiskVolumeFilesystemId)
	require.NoError(t, err)
	assert.Equal(t, entity.TypeStr, volumeSchema.Type)
	require.Len(t, volumeSchema.EnumValues, 3)
	for _, value := range volumeSchema.EnumValues {
		assert.Equal(t, entity.KindString, value.Kind())
	}

	// The natural-model schema also retains Type "string" so an older reader
	// can keep handling the field while newer readers use the enum metadata.
	require.NoError(t, schema.Apply(t.Context(), store))
	cache, err := entity.NewSchemaCache(store)
	require.NoError(t, err)
	kindSchema, err := cache.GetKindSchema(t.Context(), KindDiskVolume)
	require.NoError(t, err)
	field := kindSchema.GetField("filesystem")
	require.NotNil(t, field)
	assert.Equal(t, "string", field.Type)
	assert.Equal(t, "string", field.EnumEncoding)
	assert.Equal(t, "dev.miren.storage/enum.disk_filesystem", field.Enum)
}
