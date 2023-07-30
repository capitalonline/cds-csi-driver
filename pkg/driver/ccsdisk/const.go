package ccsdisk

const (
	HighDisk         = "high_disk"
	SsdDisk          = "ssd_disk"
	mountPointMode   = 0777
	DefaultFsTypeXfs = "xfs"
	FsTypeExt4       = "ext4"
	FsTypeExt3       = "ext3"

	defaultVolumeRecordConfigMap = "record-volume-info"
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
