package vmwaredisk

import (
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"k8s.io/client-go/kubernetes"
)

type DiskDriver struct {
	csiDriver        *csicommon.CSIDriver
	endpoint         string
	idServer         *IdentityServer
	nodeServer       *NodeServer
	controllerServer *ControllerServer
}

type NodeServer struct {
	*csicommon.DefaultNodeServer

	// A map storing all volumes with ongoing operations so that additional operations
	// for that same volume (as defined by VolumeID) return an Aborted error
	VolumeLocks *utils.VolumeLocks

	KubeClient *kubernetes.Clientset
}

type ControllerServer struct {
	*csicommon.DefaultControllerServer
	Client *kubernetes.Clientset
}

type DiskVolumeArgs struct {
	StorageType string `json:"storageType"`
	FsType      string `json:"fsType"`
	SiteID      string `json:"siteId"`
	ZoneID      string `json:"zoneId"`
	Iops        string `json:"iops"`
}
