package nas

import (
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"k8s.io/client-go/kubernetes"
)

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
	Server   string
	Path     string
	Vers     string
	Mode     string
	ModeType string
	Options  string
}

type PublishOptions struct {
	NfsOpts
	NodePublishPath string
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
