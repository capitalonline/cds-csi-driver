package oss

import (
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"k8s.io/client-go/kubernetes"
)

type OssDriver struct {
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

type OssOpts struct {
	Bucket    string `json:"bucket"`
	URL       string `json:"url"`
	OtherOpts string `json:"otherOpts"`
	AkID      string `json:"akId"`
	AkSecret  string `json:"akSecret"`
	Path      string `json:"path"`
	AuthType  string `json:"authType"`
}

type PublishOptions struct {
	OssOpts
	NodePublishPath string
	AllowSharePath  bool
}
