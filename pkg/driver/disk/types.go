package disk

import (
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
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
	Type             string `json:"type"`
	FsType           string `json:"fsType"`
	ReadOnly         bool   `json:"readOnly"`
	Encrypted        bool   `json:"encrypted"`
	RegionID         string `json:"regionId"`
	ClusterID		 string `json:"clusterID"`

	//ZoneID           string `json:"zoneId"`
	//KMSKeyID         string `json:"kmsKeyId"`
	//PerformanceLevel string `json:"performanceLevel"`
	//ResourceGroupID  string `json:"resourceGroupId"`
	//DiskTags         string `json:"diskTags"`
}

