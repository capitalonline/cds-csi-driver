package nas

import (
	"context"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func NewNodeServer(d *NasDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
	}
}

func (n *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	nodeCap := &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
			},
		},
	}

	// Disk Metric enable config
	nodeSvcCap := []*csi.NodeServiceCapability{nodeCap}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nodeSvcCap,
	}, nil
}

func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume:: starting mount nas volume with req: %+v", req)
	var opts, err = parsePublishOptions(req)

	log.Debugf("NodePublishVolume:: parsed PublishOptions options: %+v", opts)

	if err != nil {
		return nil, fmt.Errorf("nas, failed to parse mount options %+v: %s", opts, err)
	}

	// directly return if the target mountPath has been mounted
	if utils.Mounted(opts.NodePublishPath) {
		log.Warnf("NodePublishVolume:: nas, mount point %s has existed, ignore", opts.NodePublishPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	optimizeNasSetting()

	// try to create the mount point, in case it is not created
	if err = utils.CreateDir(opts.NodePublishPath, mountPointMode); err != nil {
		return nil, fmt.Errorf("NodePublishVolume:: nas, unable to create directory: %s", opts.NodePublishPath)
	}

	// mount the nas server path to the node published directory
	if err := mountNasVolume(opts, req.VolumeId); err != nil {
		return nil, fmt.Errorf("NodePublishVolume:: nas, mount nfs error: %s", err.Error())
	}

	// changes the mode of the published directory
	changeNasMode(opts)

	// check if the directory is mounted
	if !utils.Mounted(opts.NodePublishPath) {
		return nil, fmt.Errorf("NodePublishVolume:: nas, mount check failed after the mount: %s", opts.NodePublishPath)
	}

	log.Infof("NodePublishVolume:: volume %s mount successfully on mount point: %s", req.VolumeId, opts.NodePublishPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume:: starting Umount Nas Volume %s at path %s", req.VolumeId, req.TargetPath)

	// skip the unmount if the path is not mounted
	mountPoint := req.TargetPath
	if !utils.Mounted(mountPoint) {
		log.Warnf("NodeUnpublishVolume:: nas, unmount mountpoint not found, skipping: %s", mountPoint)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// unmount the volume, use force umount on network not reachable or no other pod used
	unmoutCmd := fmt.Sprintf("umount %s", mountPoint)
	if _, err := utils.RunCommand(unmoutCmd); err != nil {
		return nil, fmt.Errorf("NodeUnpublishVolume:: nas, Umount nfs fail: %s", err.Error())
	}

	log.Infof("NodeUnpublishVolume:: Unmount nas Successfully on: %s", mountPoint)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeStageVolume(context.Context, *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n *NodeServer) NodeUnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeGetVolumeStats used for csi metrics
func (ns *NodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	var err error
	targetPath := req.GetVolumePath()
	if targetPath == "" {
		err = fmt.Errorf("NodeGetVolumeStats targetpath %v is empty", targetPath)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return utils.GetMetrics(targetPath)
}