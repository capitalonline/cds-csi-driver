package nas

import (
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"os"
)

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
	os.MkdirAll(createVolumeRoot, mountPointMode)
	os.Mkdir(deleteVolumeRoot, mountPointMode)
	os.MkdirAll(publishVolumeRoot, mountPointMode)
	s.Start(d.endpoint, d.idServer, d.controllerServer, d.nodeServer)
	s.Wait()
}
