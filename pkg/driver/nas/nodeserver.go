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

type NodeServer struct {
	*csicommon.DefaultNodeServer
}

type MountOptions struct {
	MountPath string
	Server    string
	Path      string
	Vers      string
	Mode      string
	ModeType  string
	Options   string
}

func NewNodeServer(d *NasDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
	}
}

func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume:: starting mount nas volume %s with: %v", req.VolumeId, req)
	var opts, err = parseMountOptions(req)
	if err != nil {
		return nil, fmt.Errorf("nas, failed to parse mount options %+v: %s", opts, err)
	}

	// directly return if the target mountPath has been mounted
	if utils.Mounted(opts.MountPath) {
		log.Infof("nas, mount point %s has existed, ignore", opts.MountPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	optimizeNasSetting()

	if err := mountNasVolume(opts, req.VolumeId); err != nil {
		log.Errorf("nas, mount nfs error: %s", err.Error())
		return nil, fmt.Errorf("nas, mount nfs error: %s" + err.Error())
	}

	changeNasMode(opts)

	if !utils.Mounted(opts.MountPath) {
		log.Errorf("nas, mount check failed after the mount: %s", err.Error())
		return nil, fmt.Errorf("nas, mount check failed after the mount:" + opts.MountPath)
	}

	log.Infof("NodePublishVolume:: volume %s mount successfully on mount point: %s", req.VolumeId, opts.MountPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume:: starting Umount Nas Volume %s at path %s", req.VolumeId, req.TargetPath)

	// skip the unmount if the path is not mounted
	mountPoint := req.TargetPath
	if !utils.Mounted(mountPoint) {
		log.Infof("nas, unmount mountpoint not found, skipping: %s", mountPoint)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// unmount the volume, use force umount on network not reachable or no other pod used
	unmoutCmd := fmt.Sprintf("umount %s", mountPoint)
	if nfsServer := parseServerHost(mountPoint); nfsServer != "" {
		networkUnReachable := false
		noOtherPodUsed := false
		if !utils.ServerReachable(nfsServer, nasPortNumber, dialTimeout) {
			log.Warnf("nas, connecting to server: %s failed, unmount to %s", nfsServer, mountPoint)
			networkUnReachable = true
		}
		if testIfNoOtherNasUser(nfsServer, mountPoint) {
			log.Warnf("nas, there are other pods using the nas server %s, %s", nfsServer, mountPoint)
			noOtherPodUsed = true
		}
		if networkUnReachable || noOtherPodUsed {
			unmoutCmd = fmt.Sprintf("umount -f %s", mountPoint)
		}

	}
	if _, err := utils.RunCommand(unmoutCmd); err != nil {
		return nil, fmt.Errorf("nas, Umount nfs Fail: %s", err.Error())
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
