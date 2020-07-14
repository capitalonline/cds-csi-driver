package disk

import (
	"context"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"

	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/disk"
)

func NewNodeServer(d *DiskDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
	}
}

func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume:: starting mount disk volume with req: %+v", req)

	// Step 1: check necessary params
	if req.GetTargetPath() == "" {
		log.Errorf("NodePublishVolume:: req.Targetpath can not be empty")
		return nil, fmt.Errorf("NodePublishVolume:: req.Targetpath can not be empty")
	}
	if req.VolumeId == "" {
		log.Errorf("NodePublishVolume:: Volume ID must be provided")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume:: Volume ID must be provided")
	}
	if req.VolumeCapability == nil {
		log.Errorf("NodePublishVolume:: Volume Capability must be provided")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume:: Volume Capability must be provided")
	}

	// Step 2: find device name that volumeID is mounted
	deviceName, err := findDeviceNameByVolumeID(req.VolumeId)

	if err != nil {
		log.Errorf("NodePublishVolume:: findDeviceNameByVolumeID failed, err is: %s", err.Error())
	}

	// Step 3: check targetPath
	targetPath := req.GetTargetPath()
	if !utils.FileExisted(targetPath) {
		if err = utils.CreateDir(targetPath, mountPointMode); err != nil {
			log.Errorf("NodePublishVolume:: req.TargetPath: %s is not exist, but unable to create it, err is: %s", targetPath, err.Error())
			return nil, fmt.Errorf("NodePublishVolume:: req.TargetPath: %s is not exist, but unable to create it, err is: %s", targetPath, err.Error())
		}

		log.Warnf("NodePublishVolume:: req.TargetPath: %s is not exist, and create it succeed!", targetPath)
	}

	if utils.Mounted(targetPath) {
		log.Warnf("NodePublishVolume:: req.TargetPath: %s has been mounted, return directly", targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Step 4: mount device to pod path
	err = mountDeviceToPodPath(req.VolumeId, deviceName, targetPath)

	if err != nil {
		log.Errorf("NodePublishVolume:: mountDeviceToPodPath failed, err is: %s", err)
		return nil, fmt.Errorf("NodePublishVolume:: mountDeviceToPodPath failed, err is: %s", err)
	}

	// Step 5: re-check mount status
	actualMountPath, err := isDeviceMounted(deviceName)
	if err != nil {
		log.Errorf("NodePublishVolume:: re-check mount point error, err is: %s", err.Error())
		return nil, fmt.Errorf("NodePublishVolume:: re-check mount point error, err is: %s", err.Error())
	}

	if actualMountPath != targetPath {
		log.Error("NodePublishVolume:: re-check mount point failed, actualMountPath: %s != req.TargetPath: %s", actualMountPath, targetPath)
		return nil, fmt.Errorf("NodePublishVolume:: re-check mount point failed, actualMountPath: %s != req.TargetPath: %s", actualMountPath, targetPath)
	}

	log.Infof("NodePublishVolume:: mount volumeID: %s deviceName: %s to targetPath: %s successfully!", req.VolumeId, deviceName, targetPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume:: starting Umount disk Volume %s at path %s", req.VolumeId, req.TargetPath)

	// Step 1: check targetPath
	targetPath := req.GetTargetPath()

	if !utils.FileExisted(targetPath) {
		log.Error("NodeUnpublishVolume:: req.TargetPath is not exist")
		return nil, fmt.Errorf("NodeUnpublishVolume:: req.TargetPath is not exist")
	}

	// Step 2: unmount disk from podPath
	err := unMountDeviceFromPodPath(req.VolumeId, targetPath)
	if err != nil {
		log.Errorf("NodeUnpublishVolume:: unmount volumeID: %s from pod targetPath: %s failed, err is: %s", req.VolumeId, targetPath, err)
	}

	log.Infof("NodeUnpublishVolume:: successfully, unmount volumeID: %s from pod targetPath: %s", req.VolumeId, targetPath)

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

func findDeviceNameByVolumeID(volumeID string) (string, error) {
	log.Infof("findDeviceNameByVolumeID: volumeID is: %s", volumeID)

	res, err := cdsDisk.FindDeviceNameByVolumeID(&cdsDisk.FindDeviceNameByVolumeIDArgs{
		VolumeID: volumeID,
	})

	if err != nil {
		log.Errorf("findDeviceNameByVolumeID: cdsDisk.FindDeviceNameByVolumeID api error, err is: %s", err)
		return "", err
	}

	log.Infof("findDeviceNameByVolumeID: successfully!")

	return res.Data.DeviceName, nil
}

func mountDeviceToPodPath(volumeID, deviceName, targetPath string) error {
	log.Infof("mountDeviceToPodPath: volumeID: %s, deviceName:%s, targetPath:%s", volumeID, deviceName, targetPath)

	cmd := fmt.Sprintf("mount %s %s", deviceName, targetPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		log.Errorf("mountDeviceToPodPath: volumeID:%s, mount deviceName: %s to targetPath: %s failed, err is: %s", volumeID, deviceName, targetPath)
		return fmt.Errorf("mountDeviceToPodPath: volumeID:%s, mount deviceName: %s to targetPath: %s failed, err is: %s", volumeID, deviceName, targetPath)
	}

	log.Infof("mountDeviceToPodPath: Successfully, volumeID:%s, mount deviceName: %s to targetPath: %s ", volumeID, deviceName, targetPath)

	return nil

}

func unMountDeviceFromPodPath(volumeID, targetPath string) error {
	log.Infof("unMountDeviceFromPodPath: volumeID: %s, targetPath:%s", volumeID, targetPath)

	if err := utils.Unmount(targetPath); err != nil {
		if strings.Contains(err.Error(), "target is busy") || strings.Contains(err.Error(), "device is busy") {

			log.Warnf("unMountDeviceFromPodPath: targetPath is busy(occupied by another process)")
			cmdKillProcess := fmt.Sprintf("fuser -m -k %s", targetPath)

			if _, err = utils.RunCommand(cmdKillProcess); err != nil {
				log.Errorf("unMountDeviceFromPodPath: targetPath is busy, but kill occupied process failed, err is: %s", err.Error())
				return fmt.Errorf("unMountDeviceFromPodPath: targetPath is busy, but kill occupied process failed, err is: %s", err.Error())
			}

			log.Warnf("unMountDeviceFromPodPath: targetPath is busy and kill occupied process succeed!")

			if err = utils.Unmount(targetPath); err != nil {
				log.Errorf("unMountDeviceFromPodPath: unmount volumeID:%s from req.TargetPath:%s failed(again), err is: %s", volumeID, targetPath, err.Error())
				return fmt.Errorf("unMountDeviceFromPodPath: unmount volumeID:%s from req.TargetPath:%s failed(again), err is: %s", volumeID, targetPath, err.Error())
			}

			log.Infof("unMountDeviceFromPodPath: unmount volumeID:%s from req.TargetPath:%s succeed!", volumeID, targetPath)

		}

		log.Errorf("unMountDeviceFromPodPath: unmount volumeID:%s from req.TargetPath:%s failed, err is: %s", volumeID, targetPath, err.Error())

	}

	log.Infof("unMountDeviceFromPodPath: Successfully!")

	return nil

}

func isDeviceMounted(deviceName string) (string, error) {
	cmd := fmt.Sprintf("mount | grep %s | grep -v grep | awk '{print $3}'", deviceName)
	out, err := utils.RunCommand(cmd)

	if err != nil {
		log.Errorf("isDeviceMounted: utils.RunCommand failed, err is: %s", err.Error())
		return "", err
	}

	log.Infof("isDeviceMounted: successfully!")

	return out, nil
}