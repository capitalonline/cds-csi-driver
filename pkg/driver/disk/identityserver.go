package disk

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
)

var (
	volumeCap = []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}

	controllerCap = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
	}
)

func NewIdentityServer(d *DiskDriver) *IdentityServer {
	d.csiDriver.AddVolumeCapabilityAccessModes(volumeCap)
	d.csiDriver.AddControllerServiceCapabilities(controllerCap)
	return &IdentityServer{
		DefaultIdentityServer: csicommon.NewDefaultIdentityServer(d.csiDriver),
	}
}
