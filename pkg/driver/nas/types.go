package nas

import (
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"k8s.io/client-go/kubernetes"
)

type NfsServer struct {
	Address string
	Path    string
}

type NasDriver struct {
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

type NfsOpts struct {
	Servers  string
	Server   string
	Path     string
	Vers     string
	Mode     string
	ModeType string
	Options  string
	Strategy string
}

type PublishOptions struct {
	NfsOpts
	NodePublishPath string
	AllowSharePath  bool
}

type VolumeCreateSubpathOptions struct {
	NfsOpts
	VolumeAs string
}

type DeleteVolumeSubpathOptions struct {
	Server          string
	Path            string
	Vers            string
	ArchiveOnDelete bool
}
