package ext4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"miren.dev/runtime/pkg/units"
)

// ReadExt4SuperBlock reads and returns ext4 super block from given device.
func ReadExt4SuperBlock(devicePath string) (*SuperBlock, error) {
	dev, err := os.OpenFile(devicePath, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer dev.Close()

	return ReadExt4SuperBlockReadSeeker(dev)
}

// ReadExt4SuperBlockReadSeeker reads and returns ext4 super block given
// an io.ReadSeeker.
//
// The layout on disk is as follows:
// | Group 0 padding     | - 1024 bytes
// | ext4 SuperBlock     | - 1 block
// | Group Descriptors   | - many blocks
// | Reserved GDT Blocks | - many blocks
// | Data Block Bitmap   | - 1 block
// | inode Bitmap        | - 1 block
// | inode Table         | - many blocks
// | Data Blocks         | - many blocks
//
// More details can be found here https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout
//
// Our goal is to skip the Group 0 padding, read and return the ext4 SuperBlock
func ReadExt4SuperBlockReadSeeker(rsc io.ReadSeeker) (*SuperBlock, error) {
	// save current reader position
	currBytePos, err := rsc.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	if _, err := rsc.Seek(1024, io.SeekCurrent); err != nil {
		return nil, err
	}
	var sb SuperBlock
	if err := binary.Read(rsc, binary.LittleEndian, &sb); err != nil {
		return nil, err
	}

	// reset the reader to initial position
	if _, err := rsc.Seek(currBytePos, io.SeekStart); err != nil {
		return nil, err
	}

	if sb.Magic != SuperBlockMagic {
		return nil, errors.New("not an ext4 file system")
	}
	return &sb, nil
}

// IsDeviceExt4 is will read the device's superblock and determine if it is
// and ext4 superblock.
func IsDeviceExt4(devicePath string) (bool, error) {
	// ReadExt4SuperBlock will check the superblock magic number for us,
	// so we know if no error is returned, this is an ext4 device.
	_, err := ReadExt4SuperBlock(devicePath)
	if err != nil {
		return false, err
	}
	return err == nil, nil
}

// Ext4FileSystemSize reads ext4 superblock and returns the size of the underlying
// ext4 file system and its block size.
func Ext4FileSystemSize(r io.ReadSeeker) (int64, int, error) {
	sb, err := ReadExt4SuperBlockReadSeeker(r)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read ext4 superblock: %w", err)
	}
	blockSize := 1024 * (1 << sb.LogBlockSize)
	fsSize := int64(blockSize) * int64(sb.BlocksCountLow)
	return fsSize, blockSize, nil
}

func Size(sb *SuperBlock) (units.Bytes, int64) {
	blockSize := 1024 * (1 << sb.LogBlockSize)
	fsSize := int64(blockSize) * int64(sb.BlocksCountLow)
	return units.Bytes(fsSize), int64(blockSize)
}

type CompatFeature uint32
type IncompatFeature uint32
type RoCompatFeature uint32

type SuperBlock struct {
	InodesCount          uint32
	BlocksCountLow       uint32
	RootBlocksCountLow   uint32
	FreeBlocksCountLow   uint32
	FreeInodesCount      uint32
	FirstDataBlock       uint32
	LogBlockSize         uint32
	LogClusterSize       uint32
	BlocksPerGroup       uint32
	ClustersPerGroup     uint32
	InodesPerGroup       uint32
	Mtime                uint32
	Wtime                uint32
	MountCount           uint16
	MaxMountCount        uint16
	Magic                uint16
	State                uint16
	Errors               uint16
	MinorRevisionLevel   uint16
	LastCheck            uint32
	CheckInterval        uint32
	CreatorOS            uint32
	RevisionLevel        uint32
	DefaultReservedUid   uint16
	DefaultReservedGid   uint16
	FirstInode           uint32
	InodeSize            uint16
	BlockGroupNr         uint16
	FeatureCompat        CompatFeature
	FeatureIncompat      IncompatFeature
	FeatureRoCompat      RoCompatFeature
	UUID                 [16]uint8
	VolumeName           [16]byte
	LastMounted          [64]byte
	AlgorithmUsageBitmap uint32
	PreallocBlocks       uint8
	PreallocDirBlocks    uint8
	ReservedGdtBlocks    uint16
	JournalUUID          [16]uint8
	JournalInum          uint32
	JournalDev           uint32
	LastOrphan           uint32
	HashSeed             [4]uint32
	DefHashVersion       uint8
	JournalBackupType    uint8
	DescSize             uint16
	DefaultMountOpts     uint32
	FirstMetaBg          uint32
	MkfsTime             uint32
	JournalBlocks        [17]uint32
	BlocksCountHigh      uint32
	RBlocksCountHigh     uint32
	FreeBlocksCountHigh  uint32
	MinExtraIsize        uint16
	WantExtraIsize       uint16
	Flags                uint32
	RaidStride           uint16
	MmpInterval          uint16
	MmpBlock             uint64
	RaidStripeWidth      uint32
	LogGroupsPerFlex     uint8
	ChecksumType         uint8
	ReservedPad          uint16
	KbytesWritten        uint64
	SnapshotInum         uint32
	SnapshotID           uint32
	SnapshotRBlocksCount uint64
	SnapshotList         uint32
	ErrorCount           uint32
	FirstErrorTime       uint32
	FirstErrorInode      uint32
	FirstErrorBlock      uint64
	FirstErrorFunc       [32]uint8
	FirstErrorLine       uint32
	LastErrorTime        uint32
	LastErrorInode       uint32
	LastErrorLine        uint32
	LastErrorBlock       uint64
	LastErrorFunc        [32]uint8
	MountOpts            [64]uint8
	UserQuotaInum        uint32
	GroupQuotaInum       uint32
	OverheadBlocks       uint32
	BackupBgs            [2]uint32
	EncryptAlgos         [4]uint8
	EncryptPwSalt        [16]uint8
	LpfInode             uint32
	ProjectQuotaInum     uint32
	ChecksumSeed         uint32
	WtimeHigh            uint8
	MtimeHigh            uint8
	MkfsTimeHigh         uint8
	LastcheckHigh        uint8
	FirstErrorTimeHigh   uint8
	LastErrorTimeHigh    uint8
	Pad                  [2]uint8
	Reserved             [96]uint32
	Checksum             uint32
}

const SuperBlockMagic uint16 = 0xef53
