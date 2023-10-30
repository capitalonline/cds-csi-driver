package ebs_disk

import "time"

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
	PatchPVInterval       = 60 * time.Second
)

const (
	StatusEbsMounted = "running"
	StatusEbsError   = "error"
	StatusWaitEbs    = "waiting"
	MinDiskSize      = 24
	EcsMountLimit    = 16
	StatusEcsRunning = "running"
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
