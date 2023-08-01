package ccsdisk

import (
	"context"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"os"
	"strings"
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

	diskId := req.GetVolumeId()
	stagingTargetPath := req.GetStagingTargetPath()
	podPath := req.GetTargetPath()
	diskVolume := req.GetVolumeContext()

	if diskId == "" {
		log.Errorf("NodePublishVolume: req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume: req.VolumeID cant not be empty")
	}

	if stagingTargetPath == "" {
		log.Errorf("NodePublishVolume[%s]: req.StagingTargetPath cant not be empty", diskId)
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume[%s]: req.StagingTargetPath cant not be empty", diskId)
	}

	if podPath == "" {
		log.Errorf("NodePublishVolume[%s]: req.Targetpath can not be empty", diskId)
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume[%s]: req.Targetpath can not be empty", diskId)
	}

	if !utils.FileExisted(podPath) {
		if err := utils.CreateDir(podPath, mountPointMode); err != nil {
			log.Errorf("NodePublishVolume[%s]: req.TargetPath(podPath): %s is not exist, but unable to create it, err is: %s", diskId, podPath, err.Error())
			return nil, err
		}
	}

	if utils.Mounted(podPath) {
		log.Warnf("NodePublishVolume[%s]: targetPath(podPath): %s has been mounted, skip this", diskId, podPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if acquired := n.VolumeLocks.TryAcquire(diskId); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskId)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskId)
	}
	defer n.VolumeLocks.Release(diskId)

	deviceName, err := findDeviceNameByUuid(diskId)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: findDeviceNameByUuid error, err is: %s", diskId, err.Error())
		return nil, err
	}

	formatted, err := n.checkVolumeInfo(diskId)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: failed to check format: %+v", diskId, err)
		return nil, err
	}

	// disk not format
	if !formatted {
		log.Errorf("NodePublishVolume: %s is not format, cant mount and bing mount", diskId)
		return nil, fmt.Errorf("NodePublishVolume: %s is not formatted, cant mount and bing mount", diskId)
	}

	if ok := utils.Mounted(stagingTargetPath); !ok {
		err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(stagingTargetPath, "\n"), diskVolume["fsType"])
		if err != nil {
			log.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
			return nil, err
		}
	}

	err = bindMountGlobalPathToPodPath(diskId, stagingTargetPath, podPath)
	if err != nil {
		log.Errorf("NodePublishVolume[%s]: bindMountGlobalPathToPodPath failed, err is: %s", diskId, err.Error())
		return nil, err
	}

	log.Infof("NodePublishVolume[%s]: successfully mounted stagingPath %s to targetPath %s", diskId, stagingTargetPath, podPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume: starting to unbind mount diskId: %s, targetPath(podPath): %s", req.GetVolumeId(), req.GetTargetPath())

	targetPath := req.GetTargetPath()
	if !utils.FileExisted(targetPath) {
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
	diskId := req.GetVolumeId()
	targetGlobalPath := req.GetStagingTargetPath()

	if diskId == "" || targetGlobalPath == "" {
		log.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
		return nil, fmt.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
	}

	// volume lock
	if acquired := n.VolumeLocks.TryAcquire(diskId); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskId)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskId)
	}
	defer n.VolumeLocks.Release(diskId)

	log.Infof("NodeStageVolume: starting to stage diskId: %s, targetGlobalPath is: %s", diskId, targetGlobalPath)

	if err := scanNodeDiskList(); err != nil {
		log.Errorf("failed to scan node disk list: %+v", err)
		return nil, err
	}

	res, err := getDiskInfo(diskId)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: find disk info failed, err is: %s", diskId, err)
		return nil, err
	}

	deviceName, err := findDeviceNameByUuid(res.Data.VolumeId)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: findDeviceNameByUuid error, err is: %s", diskId, err.Error())
		return nil, fmt.Errorf("NodeStageVolume[%s]: findDeviceNameByUuid error, err is: %s", diskId, err.Error())
	}

	log.Debugf("NodeStageVolume[%s]: findDeviceNameByVolumeID succeed, deviceName is: %s", diskId, deviceName)

	diskVol := req.GetVolumeContext()
	fsType := diskVol["fsType"]

	formatted, err := n.checkVolumeInfo(diskId)
	if err != nil {
		log.Errorf("NodeStageVolume[%s]: failed to check format: %+v", diskId, err)
		return nil, err
	}

	if !formatted {
		if err = n.formatDiskDevice(diskId, deviceName, fsType); err != nil {
			log.Errorf("NodeStageVolume[%s]: format deviceName: %s failed, err is: %s", diskId, deviceName, err.Error())
			return nil, err
		}
	}

	if !utils.FileExisted(targetGlobalPath) {
		if err = utils.CreateDir(targetGlobalPath, mountPointMode); err != nil {
			log.Errorf("NodeStageVolume[%s]: targetGlobalPath is not exist, but unable to create it, err is: %s", diskId, err.Error())
			return nil, err
		}
	}

	if err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(targetGlobalPath, "\n"), fsType); err != nil {
		log.Errorf("NodeStageVolume[%s]: mountDeviceToNodeGlobalPath failed, err is: %s", diskId, err.Error())
		return nil, err
	}

	log.Infof("NodeStageVolume: successfully mounted volume %s to stagingTargetPath %s", diskId, targetGlobalPath)

	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	diskId := req.GetVolumeId()
	unStagingPath := req.GetStagingTargetPath()

	if diskId == "" {
		log.Errorf("NodeUnstageVolume, req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume, req.VolumeID cant not be empty")
	}

	if unStagingPath == "" {
		log.Errorf("NodeUnstageVolume[%s], req.StagingTargetPath cant not be empty", diskId)
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume, req.StagingTargetPath cant not be empty")
	}

	// volume lock
	if acquired := n.VolumeLocks.TryAcquire(diskId); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskId)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskId)
	}
	defer n.VolumeLocks.Release(diskId)

	log.Infof("NodeUnstageVolume[%s]: starting to unstage with req: %+v", diskId, req)

	if !utils.FileExisted(unStagingPath) {
		log.Warnf("NodeUnstageVolume[%s]: unStagingPath is not exist, should must be already unmount, skip this", diskId)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	if err := unMountDiskDeviceFromNodeGlobalPath(unStagingPath); err != nil {
		log.Errorf("NodeUnstageVolume[%s]: unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", diskId, err)
		return nil, err
	}

	log.Infof("NodeUnstageVolume: successfully unmounted volume (%s) from staging path (%s)", diskId, unStagingPath)

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n *NodeServer) formatDiskDevice(diskId, deviceName, fsType string) error {
	scanDeviceCmd := fmt.Sprintf("fdisk -l | grep %s | grep -v grep", deviceName)
	if _, err := utils.RunCommand(scanDeviceCmd); err != nil {
		return fmt.Errorf("formatDiskDevice: scanDeviceCmd: %s failed, err is: %s", scanDeviceCmd, err.Error())
	}

	if out, err := utils.RunCommand(fmt.Sprintf("blkid %s", deviceName)); err == nil {
		if strings.Contains(out, "TYPE") {
			log.Warnf("formatDiskDevice: deviceName: %s had been formatted, avoid multi formatting, return directly", deviceName)
			return nil
		}
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

	log.Infof("%s disk formatted successfully", diskId)

	if err := n.saveVolumeInfo(diskId); err != nil {
		return err
	}

	return nil
}

func findDeviceNameByUuid(diskUuid string) (string, error) {
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

		// check device
		_, diskErr := os.Stat(deviceName)
		if os.IsNotExist(diskErr) {
			log.Warnf("findDeviceNameByUuid: %s not found, skip this", deviceName)
			continue
		}

		cmdGetUid := fmt.Sprintf("/lib/udev/scsi_id -g -u %s", deviceName)
		uuidStr, err := utils.RunCommand(cmdGetUid)
		if err != nil {
			log.Warnf("findDeviceNameByUuid: get deviceName: %s uuid failed, err is: %s", deviceName, err)
			continue
		}

		deviceNameUuid[strings.TrimSpace(strings.TrimPrefix(uuidStr, "3"))] = deviceName
	}

	// compare
	if _, ok := deviceNameUuid[diskUuidFormat]; !ok {
		return "", fmt.Errorf("findDeviceNameByUuid: diskUuid: %s is not exist", diskUuidFormat)
	}

	deviceName := deviceNameUuid[diskUuidFormat]
	if deviceName == "" {
		return "", fmt.Errorf("findDeviceNameByUuid: diskUuid: %s, deviceName is empty", diskUuidFormat)
	}

	return deviceName, nil
}

func bindMountGlobalPathToPodPath(diskId, stagingTargetPath, podPath string) error {
	cmd := fmt.Sprintf("mount --bind %s %s", stagingTargetPath, podPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		return fmt.Errorf("bindMountGlobalPathToPodPath[%s]: bind mount stagingTargetPath: %s to podPath: %s failed, err is: %s", diskId, stagingTargetPath, podPath, err.Error())
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

func mountDiskDeviceToNodeGlobalPath(deviceName, targetGlobalPath, fstype string) error {
	uuid := ""
	cmd := fmt.Sprintf("blkid %s", deviceName)
	if out, err := utils.RunCommand(cmd); err != nil {
		log.Errorf("mountDiskDeviceToNodeGlobalPath: err is: %s", err)
		return err
	} else {
		l := strings.Split(out, " ")
		for _, v := range l {
			if strings.Contains(v, "UUID=") {
				uuid = strings.ReplaceAll(v, "\"", "")
			}
		}
	}
	log.Infof("%s uuid: %s", deviceName, uuid)
	if uuid == "" {
		cmd = fmt.Sprintf("mount -t %s %s %s", fstype, deviceName, targetGlobalPath)
	} else {
		cmd = fmt.Sprintf("mount -t %s %s %s", fstype, uuid, targetGlobalPath)
	}

	if _, err := utils.RunCommand(cmd); err != nil {
		log.Errorf("failed to mount cmd: %s, err is: %s", cmd, err)
		return err
	}
	log.Infof("successfully mount %s to %s, cmd: %s", deviceName, targetGlobalPath, cmd)

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

func (n *NodeServer) saveVolumeInfo(diskId string) error {
	updateFunc := func() error {
		cm, err := n.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(defaultVolumeRecordConfigMap, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to fetch %s config map: %+v", defaultVolumeRecordConfigMap, err)
		}

		cm.Annotations[diskId] = ""

		if _, err = n.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Update(cm); err != nil {
			return fmt.Errorf("failed tp update %s config map by %s : %+v", defaultVolumeRecordConfigMap, diskId, err)
		}

		return nil
	}

	if err := retry.RetryOnConflict(DefaultRetry, updateFunc); err != nil {
		return fmt.Errorf("failed tp update %s config map by %s : %+v", defaultVolumeRecordConfigMap, diskId, err)
	}

	return nil
}

func (n *NodeServer) deleteVolumeInfo(diskId string) error {
	deleteFunc := func() error {
		cm, err := n.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(defaultVolumeRecordConfigMap, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to fetch %s config map: %+v", defaultVolumeRecordConfigMap, err)
		}

		if _, ok := cm.Annotations[diskId]; ok {
			delete(cm.Annotations, diskId)

			if _, err = n.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Update(cm); err != nil {
				return fmt.Errorf("failed tp update %s config map by %s : %+v", defaultVolumeRecordConfigMap, diskId, err)
			}
		}

		return nil
	}

	if err := retry.RetryOnConflict(DefaultRetry, deleteFunc); err != nil {
		return fmt.Errorf("failed tp delete %s config map by %s : %+v", defaultVolumeRecordConfigMap, diskId, err)
	}

	return nil
}

func (n *NodeServer) checkVolumeInfo(diskId string) (bool, error) {
	cm, err := n.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(defaultVolumeRecordConfigMap, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to found %s config map: %+v", defaultVolumeRecordConfigMap, err)
	}

	if cm.Annotations == nil {
		return false, nil
	}

	if _, ok := cm.Annotations[diskId]; ok {
		return true, nil
	}

	return false, nil
}
