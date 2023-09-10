package oss

import (
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
)

func NewDriver(driverName, nodeId, endpoint string) *OssDriver {
	d := &OssDriver{}
	d.endpoint = endpoint
	d.csiDriver = csicommon.NewCSIDriver(driverName, common.GetVersion().Version, nodeId)
	d.idServer = NewIdentityServer(d)
	d.nodeServer = NewNodeServer(d)
	return d
}

func (d *OssDriver) Run() {
	s := csicommon.NewNonBlockingGRPCServer()
	s.Start(d.endpoint, d.idServer, nil, d.nodeServer)
	s.Wait()
}
