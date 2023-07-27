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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/exec"
	"strings"

	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/vmwaredisk"
)

const (
	globalMountName      = "globalmount"
	mountedAnnotationKey = "pv.kubernetes.io/mounted-by-node"
)

func NewNodeServer(d *DiskDriver) *NodeServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to create kubernetes config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create kubernetes client: %v", err)
	}

	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
		VolumeLocks:       utils.NewVolumeLocks(),
		KubeClient:        kubeClient,
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

func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume: starting to mount bind stagingTargetPath to pod directory with req: %+v", req)

	volumeID := req.GetVolumeId()
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
	}

	if utils.Mounted(podPath) {
		log.Warnf("NodePublishVolume[%s]: targetPath(podPath): %s has been mounted, skip this", volumeID, podPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if acquired := n.VolumeLocks.TryAcquire(volumeID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, volumeID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, volumeID)
	}
	defer n.VolumeLocks.Release(volumeID)

	res, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{
		VolumeID: volumeID,
	})
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: cdsDisk.GetDiskInfo error, err is: %s", volumeID, err)
		return nil, err
	}

	if res.Data.VolumeId == "" {
		log.Errorf("NodePublishVolume[%s]: GetDiskInfo res uuid is null", volumeID)
		return nil, fmt.Errorf("NodePublishVolume[%s]: GetDiskInfo res uuid is null", volumeID)
	}

	deviceName, err := findDeviceNameByUuid(res.Data.VolumeId)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: findDeviceNameByUuid error, err is: %s", volumeID, err.Error())
		return nil, err
	}

	existingFormat, err := getDiskFormat(exec.New(), deviceName)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: failed to get disk format for path %s, error: %v", volumeID, deviceName, err)
		return nil, err
	}

	formatted, err := n.isAlreadyFormatted(stagingTargetPath)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: failed to check format: %+v", volumeID, err)
		return nil, err
	}

	// disk not format
	if existingFormat == "" && !formatted {
		log.Errorf("NodePublishVolume: %s is not format, cant mount and bing mount", volumeID)
		return nil, fmt.Errorf("NodePublishVolume: %s is not formatted, cant mount and bing mount", volumeID)
	}

	if ok := utils.Mounted(stagingTargetPath); !ok {
		log.Infof("NodePublishVolume: %s formatted, need staging and bind mount", volumeID)
		err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(stagingTargetPath, "\n"), diskVolume["fsType"])
		if err != nil {
			log.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
			return nil, err
		}
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
		log.Warnf("NodeUnpublishVolume[%s]: targetPath(podPath) is not exist", req.GetVolumeId())
		return nil, nil
	}

	if acquired := n.VolumeLocks.TryAcquire(req.GetVolumeId()); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, req.GetVolumeId())
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, req.GetVolumeId())
	}
	defer n.VolumeLocks.Release(req.VolumeId)

	if err := unBindMountGlobalPathFromPodPath(targetPath); err != nil {
		log.Errorf("NodeUnpublishVolume[%s]: unmount req.TargetPath(podPath): %s failed, err is: %s", req.GetVolumeId(), targetPath, err.Error())
		return nil, err
	}

	log.Infof("NodeUnpublishVolume: successfully unbound volume %s from %s", req.GetVolumeId(), targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	targetGlobalPath := req.GetStagingTargetPath()

	if volumeID == "" || targetGlobalPath == "" {
		log.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
		return nil, fmt.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
	}

	// volume lock
	if acquired := n.VolumeLocks.TryAcquire(volumeID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, volumeID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, volumeID)
	}
	defer n.VolumeLocks.Release(volumeID)

	log.Infof("NodeStageVolume: starting to stage volumeID: %s, targetGlobalPath is: %s", volumeID, targetGlobalPath)

	res, err := findDiskByVolumeID(volumeID)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: find disk info failed, err is: %s", volumeID, err)
		return nil, err
	}

	if res.Data.VolumeId == "" {
		log.Errorf("NodeStageVolume[%s]: findDeviceNameByVolumeID res uuid is null", volumeID)
		return nil, fmt.Errorf("NodeStageVolume[%s]: findDeviceNameByVolumeID res uuid is null", volumeID)
	}

	deviceName, err := findDeviceNameByUuid(res.Data.VolumeId)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: findDeviceNameByUuid error, err is: %s", volumeID, err.Error())
		return nil, fmt.Errorf("NodeStageVolume[%s]: findDeviceNameByUuid error, err is: %s", volumeID, err.Error())
	}

	log.Debugf("NodeStageVolume[%s]: findDeviceNameByVolumeID succeed, deviceName is: %s", volumeID, deviceName)

	diskVol := req.GetVolumeContext()
	fsType := diskVol["fsType"]

	existingFormat, err := getDiskFormat(exec.New(), deviceName)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: failed to get disk format for path %s, error: %v", volumeID, deviceName, err)
		return nil, err
	}

	formatted, err := n.isAlreadyFormatted(targetGlobalPath)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: failed to check format: %+v", volumeID, err)
		return nil, err
	}

	if existingFormat == "" && !formatted {
		if err = n.formatDiskDevice(targetGlobalPath, volumeID, deviceName, fsType); err != nil {
			log.Errorf("NodeStageVolume[%s]: format deviceName: %s failed, err is: %s", deviceName, err.Error())
			return nil, err
		}

		log.Infof("NodeStageVolume[%s]: formatDiskDevice successfully!", volumeID)
	} else {
		log.Infof("NodeStageVolume[%s]: diskID: %s had been formatted, ignore multi format", volumeID, volumeID)
	}

	if !utils.FileExisted(targetGlobalPath) {
		if err = utils.CreateDir(targetGlobalPath, mountPointMode); err != nil {
			log.Errorf("NodeStageVolume[%s]: targetGlobalPath is not exist, but unable to create it, err is: %s", volumeID, err.Error())
			return nil, err
		}

		log.Infof("NodeStageVolume[%s]: targetGlobalPath: %s is not exist, and create succeed", volumeID, targetGlobalPath)
	}

	if err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(targetGlobalPath, "\n"), fsType); err != nil {
		log.Errorf("NodeStageVolume[%s]: mountDeviceToNodeGlobalPath failed, err is: %s", volumeID, err.Error())
		return nil, err
	}

	log.Infof("NodeStageVolume: successfully mounted volume %s to stagingTargetPath %s", volumeID, targetGlobalPath)

	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	unStagingPath := req.GetStagingTargetPath()

	if volumeID == "" {
		log.Errorf("NodeUnstageVolume, req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume, req.VolumeID cant not be empty")
	}

	if unStagingPath == "" {
		log.Errorf("NodeUnstageVolume[%s], req.StagingTargetPath cant not be empty", volumeID)
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
		log.Warnf("NodeUnstageVolume[%s]: unStagingPath is not exist, should must be already unmount, skip this", volumeID)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	if err := unMountDiskDeviceFromNodeGlobalPath(unStagingPath); err != nil {
		log.Errorf("NodeUnstageVolume[%s]: unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", volumeID, err)
		return nil, err
	}

	log.Infof("NodeUnstageVolume: successfully unmounted volume (%s) from staging path (%s)", volumeID, unStagingPath)

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n *NodeServer) formatDiskDevice(targetGlobalPath, diskId, deviceName, fsType string) error {
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
			log.Warnf("formatDiskDevice: deviceName: %s had been formatted, avoid multi formatting, return directly", deviceName)
			return nil
		}

		return fmt.Errorf("formatDiskDevice: formatDeviceCmd: %s failed, err is: %s", formatDeviceCmd, err.Error())
	}

	// mark format completed
	if err := n.updatePersistentVolumeAnnotation(targetGlobalPath, diskId); err != nil {
		return err
	}

	log.Infof("formatDiskDevice[%s]: Successfully!", diskId)

	return nil
}

func (n NodeServer) updatePersistentVolumeAnnotation(globalMountPath, volumeId string) error {
	strList := strings.Split(globalMountPath, "/")

	pvName := ""
	for i := range strList {
		if strList[i] != globalMountName {
			continue
		}

		if i-1 < 0 {
			return fmt.Errorf("not found pv name from %s", globalMountPath)
		}

		pvName = strList[i-1]
	}

	updateFunc := func() error {
		pv, err := n.KubeClient.CoreV1().PersistentVolumes().Get(pvName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to fetch %s pv: %+v", pvName, err)
		}

		pv.Annotations[mountedAnnotationKey] = volumeId

		if _, err := n.KubeClient.CoreV1().PersistentVolumes().Update(pv); err != nil {
			log.Errorf("failed to update pv %s: %+v", pvName, err)
			return err
		}

		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, updateFunc); err != nil {
		return fmt.Errorf("failed to update pv %s: %+v", pvName, err)
	}

	return nil
}

func (n NodeServer) isAlreadyFormatted(globalMountPath string) (bool, error) {
	strList := strings.Split(globalMountPath, "/")

	pvName := ""
	for i := range strList {
		if strList[i] != globalMountName {
			continue
		}

		if i-1 < 0 {
			return false, fmt.Errorf("not found pv name from %s", globalMountPath)
		}

		pvName = strList[i-1]
	}

	pv, err := n.KubeClient.CoreV1().PersistentVolumes().Get(pvName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to fetch %s pv: %+v", pvName, err)
	}

	if _, ok := pv.Annotations[mountedAnnotationKey]; ok {
		return true, nil
	}

	return false, nil
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

func unMountDiskDeviceFromNodeGlobalPath(unStagingPath string) error {
	cmdUnstagingPath := fmt.Sprintf("umount %s", unStagingPath)
	_, err := utils.RunCommand(cmdUnstagingPath)
	if err == nil {
		return nil
	}

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
