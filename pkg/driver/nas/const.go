package nas

import "time"

const (
	sunRPCFile        = "/etc/modprobe.d/sunrpc.conf"
	createVolumeRoot  = "/nas_volume/create"
	deleteVolumeRoot  = "/nas_volume/delete"
	publishVolumeRoot = "/nas_volume/publish"
	mountPointMode    = 0777
	defaultV3Opts     = "noresvport,nolock,tcp"
	defaultV4Opts     = "noresvport"
	defaultNFSRoot    = "/nfsshare"
	nasPortNumber     = "2049"
	dialTimeout       = time.Duration(3) * time.Second
	subpathLiteral    = "subpath"
	fileSystemLiteral = "filesystem"
	defaultNfsVersion = "4.0"
	defaultDeleteNas  = "false"
	defaultNfsPath    = "/nfsshare"
	defaultNasUsage   = "1"
	defaultClusterID  = "CDS_CLUSTER_ID"
)
