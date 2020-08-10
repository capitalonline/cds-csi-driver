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

type IdentityServer struct {
	*csicommon.DefaultIdentityServer
}

// capitalonline disk parameters
type DiskVolumeArgs struct {
	StorageType string `json:"storageType"`
	FsType      string `json:"fsType"`
	ReadOnly    bool   `json:"readOnly"`
	SiteID      string `json:"siteId"`
	ClusterID   string `json:"clusterId"`
	ZoneID  		string `json:"zoneId"`
}
