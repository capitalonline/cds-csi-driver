package disk

const (
	HighDisk          = "high_disk"
	SsdDisk           = "ssd_disk"
	mountPointMode    = 0777
	DefaultFsTypeExt4 = "ext4"
	FsTypeExt3        = "ext3"
	FsTypeExt2        = "ext2"
	FsTypeXfs         = "xfs"
	StatusInMounted   = "mounted"
	StatusInOK        = "ok"
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
