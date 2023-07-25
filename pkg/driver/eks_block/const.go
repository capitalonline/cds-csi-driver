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
	DiskFeatureSSD          = "ssd"
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

const (
	OrderHead    = "scsi-0QEMU_QEMU_HARDDISK_drive-scsi0-0-0-"
	ErrorStatus  = "error"
	Staging      = "staging"
	Formatted    = "formatted"
	Publishing   = "publishing"
	UnPublishing = "unPublishing"
	UnStaing     = "UnStaing"
	Ok           = "ok"
)
