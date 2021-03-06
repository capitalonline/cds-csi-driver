package block

const (
	HighDisk         = "high_disk"
	Ssd              = "ssd"
	mountPointMode   = 0777
	DefaultFsTypeXfs = "xfs"
	FsTypeExt4       = "ext4"
	FsTypeExt3       = "ext3"
	StatusInMounted  = "mounted"
	StatusInOK       = "ok"
	StatusInDeleted  = "deleted"
	StatusInError    = "error"
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
