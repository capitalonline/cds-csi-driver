package vmwaredisk

import (
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"os"
)

// PluginFolder defines the location of disk plugin
const (
	driverName      = "vmware.disk.csi.cds.net"
	csiVersion      = "1.0.0"
	TopologyZoneKey = "topology.kubernetes.io/zone"
	uuidPath        = "/sys/devices/virtual/dmi/id/product_uuid"
)

func NewDriver(driverName, endpoint string) *DiskDriver {
	// fetch vm uuid
	nodeId, err := os.ReadFile(uuidPath)
	if err != nil {
		log.Fatal("failed to read product_uuid file: %+v", err)
	}

	d := &DiskDriver{}
	d.endpoint = endpoint
	d.csiDriver = csicommon.NewCSIDriver(driverName, common.GetVersion().Version, string(nodeId))
	d.idServer = NewIdentityServer(d)
	d.nodeServer = NewNodeServer(d)
	d.controllerServer = NewControllerServer(d)
	return d
}

func (d *DiskDriver) Run() {
	log.Infof("Starting csi-plugin Driver: %v version: %v", driverName, csiVersion)
	s := csicommon.NewNonBlockingGRPCServer()
	s.Start(d.endpoint, d.idServer, d.controllerServer, d.nodeServer)
	s.Wait()
}
