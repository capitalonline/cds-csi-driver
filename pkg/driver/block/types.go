package block

import (
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"k8s.io/client-go/kubernetes"
)

type BlockDriver struct {
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

// capitalonline disk parameters
type BlockVolumeArgs struct {
	StorageType string `json:"storageType"`
	FsType      string `json:"fsType"`
	SiteID      string `json:"siteId"`
	Iops        string `json:"iops"`
	Bandwidth   string `json:"bandwidth"`
}
