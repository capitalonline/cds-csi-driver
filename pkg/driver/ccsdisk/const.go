package ccsdisk

const (
	HighDisk         = "high_disk"
	SsdDisk          = "ssd_disk"
	mountPointMode   = 0777
	DefaultFsTypeXfs = "xfs"
	FsTypeExt4       = "ext4"
	FsTypeExt3       = "ext3"

	defaultVolumeRecordConfigMap = "record-volume-info"

	MinDiskSize = 10
	MaxDiskSize = 4000

	MaxDiskCount = 14

	// disk state
	diskProcessingStateByTask = "doing"
	diskOKStateByTask         = "normal"
	diskErrorStateByTask      = "error"
	diskOKState               = "ok"
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
