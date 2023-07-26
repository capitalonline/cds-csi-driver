package vmwaredisk

import (
	"context"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/utils/exec"
	"strings"

	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/disk"
)

const (
	diskFormattedState = "formatted"
	reqSuccessState    = "Success"
)

func NewNodeServer(d *DiskDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
		VolumeLocks:       utils.NewVolumeLocks(),
	}
}

func (n *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	nodeCap := &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
			},
		},
	}

	// Disk Metric enable config
	nodeSvcCap := []*csi.NodeServiceCapability{nodeCap}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nodeSvcCap,
	}, nil
}

// bind mount node's global path to pod directory
func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume: starting to mount bind stagingTargetPath to pod directory with req: %+v", req)

	// Step 1: check necessary params
	volumeID := req.VolumeId
	stagingTargetPath := req.GetStagingTargetPath()
	podPath := req.GetTargetPath()
	diskVolume := req.GetVolumeContext()

	if volumeID == "" {
		log.Errorf("NodePublishVolume: req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume: req.VolumeID cant not be empty")
	}

	if stagingTargetPath == "" {
		log.Errorf("NodePublishVolume[%s]: req.StagingTargetPath cant not be empty", volumeID)
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume[%s]: req.StagingTargetPath cant not be empty", volumeID)
	}

	if podPath == "" {
		log.Errorf("NodePublishVolume[%s]: req.Targetpath can not be empty", volumeID)
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume[%s]: req.Targetpath can not be empty", volumeID)
	}

	if !utils.FileExisted(podPath) {
		if err := utils.CreateDir(podPath, mountPointMode); err != nil {
			log.Errorf("NodePublishVolume[%s]: req.TargetPath(podPath): %s is not exist, but unable to create it, err is: %s", volumeID, podPath, err.Error())
			return nil, err
		}

		log.Debugf("NodePublishVolume[%s]: req.TargetPath(podPath): %s is not exist, and create it succeed!", volumeID, podPath)
	}

	if utils.Mounted(podPath) {
		log.Warnf("NodePublishVolume[%s]: req.TargetPath(podPath): %s has been mounted, return directly", volumeID, podPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if acquired := n.VolumeLocks.TryAcquire(volumeID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, volumeID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, volumeID)
	}
	defer n.VolumeLocks.Release(volumeID)

	res, err := cdsDisk.FindDiskByVolumeID(&cdsDisk.FindDiskByVolumeIDArgs{
		VolumeID: volumeID,
	})
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: cdsDisk.FindDiskByVolumeID error, err is: %s", volumeID, err)
		return nil, err
	}

	if res.Data.VolumeId == "" {
		log.Errorf("NodePublishVolume[%s]: findDeviceNameByVolumeID res uuid is null", volumeID)
		return nil, fmt.Errorf("NodePublishVolume[%s]: findDeviceNameByVolumeID res uuid is null", volumeID)
	}

	deviceName, err := findDeviceNameByUuid(res.Data.VolumeId)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: findDeviceNameByUuid error, err is: %s", volumeID, err.Error())
		return nil, err
	}

	existingFormat, err := GetDiskFormat(exec.New(), deviceName)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: failed to get disk format for path %s, error: %v", diskID, deviceName, err)
		return nil, fmt.Errorf("NodeStageVolume[%s]: failed to get disk format for path %s, error: %v", diskID, deviceName, err)
	}

	// disk not format
	if existingFormat == "" {
		log.Errorf("NodePublishVolume: %s is not formatted, cant mount and bing mount", volumeID)
		return nil, fmt.Errorf("NodePublishVolume: %s is not formatted, cant mount and bing mount", volumeID)
	}

	// staging record exist and record staging path is equal to new staging path
	if ok := utils.Mounted(stagingTargetPath); ok {
		log.Infof("NodePublishVolume: %s has been staged to stagingTargetPath: %s, direct to bind mount", volumeID, stagingTargetPath)
		err = bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
		if err != nil {
			log.Errorf("NodePublishVolume[%s]: bindMountGlobalPathToPodPath failed, err is: %s", volumeID, err.Error())
			return nil, err
		}
		log.Infof("NodePublishVolume[%s]: successfully mounted stagingPath %s to targetPath %s", volumeID, stagingTargetPath, podPath)

		return &csi.NodePublishVolumeResponse{}, nil
	}

	// staging record not exist or record staging path is different from new staging path
	// need staging and bind mount two steps
	log.Infof("NodePublishVolume: %s formatted, need staging and bind mount", volumeID)

	err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(stagingTargetPath, "\n"), diskVolume["fsType"])
	if err != nil {
		log.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
		return nil, err
	}

	err = bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: bindMountGlobalPathToPodPath failed, err is: %s", volumeID, err.Error())
		return nil, err
	}

	log.Infof("NodePublishVolume[%s]: successfully mounted stagingPath %s to targetPath %s", volumeID, stagingTargetPath, podPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume: starting to unbind mount volumeID: %s, targetPath(podPath): %s", req.GetVolumeId(), req.GetTargetPath())

	targetPath := req.GetTargetPath()
	if !utils.FileExisted(targetPath) {
		log.Errorf("NodeUnpublishVolume[%s]: req.TargetPath(podPath) is not exist", req.GetVolumeId())
		return nil, fmt.Errorf("NodeUnpublishVolume[%s]: req.TargetPath(podPath) is not exist", req.GetVolumeId())
	}

	if acquired := n.VolumeLocks.TryAcquire(req.GetVolumeId()); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, req.GetVolumeId())
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, req.GetVolumeId())
	}
	defer n.VolumeLocks.Release(req.VolumeId)

	if err := unBindMountGlobalPathFromPodPath(targetPath); err != nil {
		log.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
		return nil, err
	}

	log.Infof("NodeUnpublishVolume: successfully unbound volume %s from %s", req.GetVolumeId(), targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	diskID := req.VolumeId
	targetGlobalPath := req.GetStagingTargetPath()

	if diskID == "" || targetGlobalPath == "" {
		log.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
		return nil, fmt.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
	}

	// volume lock
	if acquired := n.VolumeLocks.TryAcquire(diskID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskID)
	}
	defer n.VolumeLocks.Release(diskID)

	log.Infof("NodeStageVolume: starting to stage volumeID: %s, targetGlobalPath is: %s", diskID, targetGlobalPath)

	res, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: find disk uuid failed, err is: %s", diskID, err)
		return nil, err
	}

	if res.Data.VolumeId == "" {
		log.Errorf("NodeStageVolume[%s]: findDeviceNameByVolumeID res uuid is null", diskID)
		return nil, fmt.Errorf("NodeStageVolume[%s]: findDeviceNameByVolumeID res uuid is null", diskID)
	}

	deviceName, err := findDeviceNameByUuid(res.Data.VolumeId)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: findDeviceNameByUuid error, err is: %s", diskID, err.Error())
		return nil, fmt.Errorf("NodeStageVolume[%s]: findDeviceNameByUuid error, err is: %s", diskID, err.Error())
	}

	log.Debugf("NodeStageVolume[%s]: findDeviceNameByVolumeID succeed, deviceName is: %s", diskID, deviceName)

	diskVol := req.GetVolumeContext()
	fsType := diskVol["fsType"]

	existingFormat, err := GetDiskFormat(exec.New(), deviceName)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: failed to get disk format for path %s, error: %v", diskID, deviceName, err)
		return nil, fmt.Errorf("NodeStageVolume[%s]: failed to get disk format for path %s, error: %v", diskID, deviceName, err)
	}

	if existingFormat == "" {
		if err = formatDiskDevice(diskID, deviceName, fsType); err != nil {
			return nil, fmt.Errorf("NodeStageVolume[%s]: format deviceName: %s failed, err is: %s", deviceName, err.Error())
		}
		log.Infof("NodeStageVolume[%s]: formatDiskDevice successfully!", diskID)
	} else {
		log.Infof("NodeStageVolume[%s]: diskID: %s had been formatted, ignore multi format", diskID, diskID)
	}

	log.Debugf("NodeStageVolume[%s]: targetGlobalPath exist flag: %t", diskID, utils.FileExisted(targetGlobalPath))
	if !utils.FileExisted(targetGlobalPath) {
		if err = utils.CreateDir(targetGlobalPath, mountPointMode); err != nil {
			log.Errorf("NodeStageVolume[%s]: targetGlobalPath is not exist, but unable to create it, err is: %s", diskID, err.Error())
			return nil, fmt.Errorf("NodeStageVolume[%s]: targetGlobalPath is not exist, but unable to create it, err is: %s", diskID, err.Error())
		}
		log.Infof("NodeStageVolume[%s]: targetGlobalPath: %s is not exist, and create succeed", diskID, targetGlobalPath)
	}

	err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(targetGlobalPath, "\n"), fsType)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: mountDeviceToNodeGlobalPath failed, err is: %s", diskID, err.Error())
		return nil, fmt.Errorf("NodeStageVolume[%s]: mountDeviceToNodeGlobalPath failed, err is: %s", diskID, err.Error())
	}

	log.Infof("NodeStageVolume: successfully mounted volume %s to stagingTargetPath %s", diskID, targetGlobalPath)

	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	volumeID := req.VolumeId
	unStagingPath := req.GetStagingTargetPath()

	if volumeID == "" {
		log.Errorf("NodeUnstageVolume, req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume, req.VolumeID cant not be empty")
	}

	if unStagingPath == "" {
		log.Errorf("NodeUnstageVolume, req.StagingTargetPath cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume, req.StagingTargetPath cant not be empty")
	}

	// volume lock
	if acquired := n.VolumeLocks.TryAcquire(volumeID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, volumeID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, volumeID)
	}
	defer n.VolumeLocks.Release(volumeID)

	log.Infof("NodeUnstageVolume[%s]: starting to unstage with req: %+v", volumeID, req)

	if !utils.FileExisted(unStagingPath) {
		log.Warnf("NodeUnstageVolume[%s]: unStagingPath is not exist, should must be already unmount, retrun directly", volumeID)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	if err := unMountDiskDeviceFromNodeGlobalPath(volumeID, unStagingPath); err != nil {
		return nil, fmt.Errorf("NodeUnstageVolume: unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", err)
	}

	log.Infof("NodeUnstageVolume: successfully unmounted volume (%s) from staging path (%s)", req.GetVolumeId(), unStagingPath)

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func findDeviceNameByUuid(diskUuid string) (string, error) {
	log.Infof("findDeviceNameByUuid: diskUuid is: %s", diskUuid)

	diskUuidFormat := strings.ReplaceAll(diskUuid, "-", "")
	cmdScan := "ls /dev/sd[a-z]"
	deviceNameStr, err := utils.RunCommand(cmdScan)
	if err != nil {
		return "", fmt.Errorf("findDeviceNameByUuid: cmdScan: %s failed, err is: %s", cmdScan, err)
	}

	// get device such as /dev/sda
	deviceNameStr = strings.ReplaceAll(deviceNameStr, "\n", " ")
	deviceNameUuid := map[string]string{}
	for _, deviceName := range strings.Split(deviceNameStr, " ") {
		if deviceName == "" {
			continue
		}

		cmdGetUid := fmt.Sprintf("/usr/lib/udev/scsi_id -g -u %s", deviceName)
		uuidStr, err := utils.RunCommand(cmdGetUid)
		if err != nil {
			return "", fmt.Errorf("findDeviceNameByUuid: get deviceName: %s uuid failed, err is: %s", deviceName, err)
		}

		deviceNameUuid[strings.TrimSpace(strings.TrimPrefix(uuidStr, "3"))] = deviceName
	}

	log.Infof("findDeviceNameByUuid: deviceNameUuid is: %+v", deviceNameUuid)

	// compare
	if _, ok := deviceNameUuid[diskUuidFormat]; !ok {
		return "", fmt.Errorf("findDeviceNameByUuid: diskUuid: %s is not exist", diskUuidFormat)
	}

	deviceName := deviceNameUuid[diskUuidFormat]
	if deviceName == "" {
		return "", fmt.Errorf("findDeviceNameByUuid: diskUuid: %s, deviceName is empty", diskUuidFormat)
	}

	log.Infof("findDeviceNameByUuid: successfully, diskUuid: %s, deviceName is: %s", diskUuidFormat, deviceNameUuid[diskUuidFormat])

	return deviceName, nil
}

func bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath string) error {
	cmd := fmt.Sprintf("mount --bind %s %s", stagingTargetPath, podPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		return fmt.Errorf("bindMountGlobalPathToPodPath[%s]: bind mount stagingTargetPath: %s to podPath: %s failed, err is: %s", volumeID, stagingTargetPath, podPath, err.Error())
	}
	log.Infof("bindMountGlobalPathToPodPath[%s]: Successfully!", volumeID)

	return nil
}

func unBindMountGlobalPathFromPodPath(targetPath string) error {
	cmdUnBindMount := fmt.Sprintf("umount %s", targetPath)

	_, err := utils.RunCommand(cmdUnBindMount)
	if err == nil {
		log.Infof("unBindMountGlobalPathFromPodPath[%s]: Successfully!", targetPath)
		return nil
	}

	if strings.Contains(err.Error(), "target is busy") || strings.Contains(err.Error(), "device is busy") {
		log.Warnf("unBindMountGlobalPathFromPodPath: unStagingPath is busy(occupied by another process)")

		cmdKillProcess := fmt.Sprintf("fuser -m -k %s", targetPath)
		if _, err = utils.RunCommand(cmdKillProcess); err != nil {
			return fmt.Errorf("unBindMountGlobalPathFromPodPath: targetPath is busy, but kill occupied process failed, err is: %s", err.Error())
		}

		log.Warnf("unBindMountGlobalPathFromPodPath: targetPath is busy and kill occupied process successed!")

		if err = utils.Unmount(targetPath); err != nil {
			return fmt.Errorf("unBindMountGlobalPathFromPodPath: unmount req.TargetPath:%s failed(again), err is: %s", targetPath, err.Error())
		}

		log.Infof("unBindMountGlobalPathFromPodPath[%s]: Successfully!", targetPath)
		return nil
	}

	return fmt.Errorf("unBindMountGlobalPathFromPodPath: unmount req.TargetPath:%s failed, err is: %s", targetPath, err.Error())
}

func mountDiskDeviceToNodeGlobalPath(deviceName, targetGlobalPath, fsType string) error {
	cmd := fmt.Sprintf("mount -t %s %s %s", fsType, deviceName, targetGlobalPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		return fmt.Errorf("mountDiskDeviceToNodeGlobalPath: err is: %s", err)
	}
	log.Infof("mountDiskDeviceToNodeGlobalPath[%s]: Successfully!", targetGlobalPath)

	return nil
}

func unMountDiskDeviceFromNodeGlobalPath(volumeID, unStagingPath string) error {
	log.Infof("unMountDeviceFromNodeGlobalPath: volumeID is: %s, unStagingPath: %s", volumeID, unStagingPath)

	cmdUnstagingPath := fmt.Sprintf("umount %s", unStagingPath)
	if _, err := utils.RunCommand(cmdUnstagingPath); err != nil {
		if strings.Contains(err.Error(), "target is busy") || strings.Contains(err.Error(), "device is busy") {
			log.Warnf("unMountDiskDeviceFromNodeGlobalPath: unStagingPath is busy(occupied by another process)")

			cmdKillProcess := fmt.Sprintf("fuser -m -k %s", unStagingPath)
			if _, err = utils.RunCommand(cmdKillProcess); err != nil {
				return fmt.Errorf("unMountDiskDeviceFromNodeGlobalPath: unStagingPath is busy, but kill occupied process failed, err is: %s", err.Error())
			}
			log.Warnf("unMountDiskDeviceFromNodeGlobalPath: unStagingPath is busy and kill occupied process succeed!")

			if _, err = utils.RunCommand(cmdUnstagingPath); err != nil {
				return fmt.Errorf("unMountDiskDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed(again), err is: %s", unStagingPath, err.Error())
			}
			log.Infof("unMountDiskDeviceFromNodeGlobalPath: Successfully!")

			return nil
		}

		return fmt.Errorf("unMountDiskDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed, err is: %s", unStagingPath, err.Error())
	}
	log.Infof("unMountDiskDeviceFromNodeGlobalPath: Successfully!")

	return nil
}

func formatDiskDevice(diskId, deviceName, fsType string) error {
	log.Infof("formatDiskDevice: deviceName is: %s, fsType is: %s", deviceName, fsType)

	scanDeviceCmd := fmt.Sprintf("fdisk -l | grep %s | grep -v grep", deviceName)
	if _, err := utils.RunCommand(scanDeviceCmd); err != nil {
		return fmt.Errorf("formatDiskDevice: scanDeviceCmd: %s failed, err is: %s", scanDeviceCmd, err.Error())
	}

	var formatDeviceCmd string
	switch fsType {
	case DefaultFsTypeXfs:
		formatDeviceCmd = fmt.Sprintf("mkfs.xfs %s", deviceName)
	case FsTypeExt3:
		formatDeviceCmd = fmt.Sprintf("mkfs.ext3 %s", deviceName)
	case FsTypeExt4:
		formatDeviceCmd = fmt.Sprintf("mkfs.ext4 %s", deviceName)
	default:
		return fmt.Errorf("formatDiskDevice: fsType not support, should be [ext4/ext3/xfs]")
	}

	if out, err := utils.RunCommand(formatDeviceCmd); err != nil {
		if strings.Contains(out, "existing filesystem") {
			diskFormattedMap[diskId] = diskFormattedState
			log.Warnf("formatDiskDevice: deviceName: %s had been formatted, avoid multi formatting, return directly", deviceName)
			return nil
		}

		return fmt.Errorf("formatDiskDevice: formatDeviceCmd: %s failed, err is: %s", formatDeviceCmd, err.Error())
	}

	diskFormattedMap[diskId] = diskFormattedState
	if res, err := cdsDisk.UpdateBlockFormatFlag(&cdsDisk.UpdateBlockFormatFlagArgs{
		BlockID:  diskId,
		IsFormat: 1,
	}); err != nil || res.Code != reqSuccessState {
		return fmt.Errorf("formatDiskDevice: update  database error, err is: %s", err)
	}
	log.Infof("formatDiskDevice: Successfully!")

	return nil
}
