package eks_block

const (
	HighDisk         = "high_disk"
	SsdDisk          = "ssd_disk"
	mountPointMode   = 0777
	DefaultFsTypeXfs = "xfs"
	FsTypeExt4       = "ext4"
	FsTypeExt3       = "ext3"
	StatusInMounted  = "mounted"
	StatusInOK       = "ok"
	StatusInDeleted  = "deleted"
	StatusInError    = "error"
)

const (
	DiskFeatureSSD          = "SSD"
	TaskStatusFinish        = "finish"
	TaskStatusDoing         = "doing"
	TaskStatusError         = "error"
	DiskStatusWaiting       = "waiting"
	DiskStatusRunning       = "running"
	DiskStatusError         = "error"
	DiskStatusDeleted       = "deleted"
	DiskStatusBuilding      = "building"
	DiskStatusMounting      = "mounting"
	DiskStatusUnmounting    = "unmounting"
	DiskStatusDeleting      = "deleting"
	DiskStatusMountFailed   = "mount_failed"
	DiskStatusUnmountFailed = "unmount_failed"

	DiskMountLimit = 14
)

var IopsArrayInt64 = []int64{3000, 5000, 7500, 10000}
