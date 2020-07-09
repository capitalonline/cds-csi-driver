package disk

import "github.com/kubernetes-csi/drivers/pkg/csi-common"

func NewNodeServer(d *DiskDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
	}
}

