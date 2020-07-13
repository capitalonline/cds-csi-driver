package disk

import (
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
)

// PluginFolder defines the location of disk plugin
const (
	driverName      = "disk.csi.cds.net"
	csiVersion      = "1.0.0"
	TopologyRegionKey = "topology." + driverName + "/region"
)

func NewDriver(driverName, nodeId, endpoint string) *DiskDriver {
	d := &DiskDriver{}
	d.endpoint = endpoint
	d.csiDriver = csicommon.NewCSIDriver(driverName, common.GetVersion().Version, nodeId)
	d.idServer = NewIdentityServer(d)
	d.nodeServer = NewNodeServer(d)
	return d
}

func (d *DiskDriver) Run() {
	log.Infof("Starting csi-plugin Driver: %v version: %v", driverName, csiVersion)
	s := csicommon.NewNonBlockingGRPCServer()
	s.Start(d.endpoint, d.idServer, nil, d.nodeServer)
	s.Wait()
}