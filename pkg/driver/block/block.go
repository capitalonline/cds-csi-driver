package block

import (
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
)

// PluginFolder defines the location of disk plugin
const (
	driverName      = "block.csi.cds.net"
	csiVersion      = "1.0.0"
	TopologySiteKey = "topology.kubernetes.io/site"
)

func NewDriver(driverName, nodeId, endpoint string) *BlockDriver {
	b := &BlockDriver{}
	b.endpoint = endpoint
	b.csiDriver = csicommon.NewCSIDriver(driverName, common.GetVersion().Version, nodeId)
	b.idServer = NewIdentityServer(b)
	b.nodeServer = NewNodeServer(b)
	b.controllerServer = NewControllerServer(b)
	return b
}

func (b *BlockDriver) Run() {
	log.Infof("Starting csi-plugin Driver: %v version: %v", driverName, csiVersion)
	s := csicommon.NewNonBlockingGRPCServer()
	s.Start(b.endpoint, b.idServer, b.controllerServer, b.nodeServer)
	s.Wait()
}
