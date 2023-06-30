package ebs_disk

const (
	mountPointMode   = 0777
	DefaultFsTypeXfs = "xfs"
	FsTypeExt4       = "ext4"
	FsTypeExt3       = "ext3"
	StatusInMounted  = "mounted"
	StatusInOK       = "ok"
	StatusInDeleted  = "deleted"
	StatusInError    = "error"
	ebsSsdDisk       = "SSD"

	DriverEbsDiskTypeName = "ebs-disk.csi.cds.net"
)

const (
	StatusEbsMounted = "running"
	StatusEbsError   = "error"
	MinDiskSize      = 24
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
