package nas

import (
	"github.com/kubernetes-csi/drivers/pkg/csi-common"

	"github.com/capitalonline/cds-csi-driver/pkg/common"
)

type NasDriver struct {
	csiDriver        *csicommon.CSIDriver
	endpoint         string
	idServer         *IdentityServer
	nodeServer       *NodeServer
	controllerServer *ControllerServer
}

func NewDriver(driverName, nodeId, endpoint string) *NasDriver {
	d := &NasDriver{}
	d.endpoint = endpoint

	d.csiDriver = csicommon.NewCSIDriver(driverName, common.GetVersion().Version, nodeId)
	d.idServer = NewIdentityServer(d)
	d.nodeServer = NewNodeServer(d)
	d.controllerServer = NewControllerServer(d)
	return d
}

func (d *NasDriver) Run() {
	s := csicommon.NewNonBlockingGRPCServer()
	s.Start(d.endpoint, d.idServer, d.controllerServer, d.nodeServer)
	s.Wait()
}
