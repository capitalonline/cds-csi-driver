package disk

import (
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
