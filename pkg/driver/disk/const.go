package disk

const (
	sunRPCFile        = "/etc/modprobe.d/sunrpc.conf"
	createVolumeRoot  = "/nas_volume/create"
	deleteVolumeRoot  = "/nas_volume/delete"
	publishVolumeRoot = "/nas_volume/publish"
	mountPointMode    = 0777
	SharedEnable      = "Share"
	// High disk type
	DiskHigh = "disk_high"
	// common disk type
	DiskCommon = "disk_common"

	DefaultFsType = "ext4"
	DiskStatusInUse = "in_use"
)

