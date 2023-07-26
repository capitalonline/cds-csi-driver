package eks_block

import (
	"context"
	"fmt"
	api "github.com/capitalonline/cds-csi-driver/pkg/driver/eks_block/api"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/profile"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"strings"
	"sync"
)

// storing staging disk
var diskStagingMap = new(sync.Map)

// storing formatted disk
var diskFormattedMap = new(sync.Map)

// storing publishing disk
var diskPublishingMap = new(sync.Map)

// storing published disk
var diskPublishedMap = new(sync.Map)

// storing unStaging disk
var diskUnStagingMap = new(sync.Map)

// storing unPublishing disk
var diskUnPublishingMap = new(sync.Map)

func NewNodeServer(d *DiskDriver) *NodeServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes client: %v", err)
	}
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
		Client:            clientset,
	}
}

// NodeGetCapabilities 检查节点存活性接口
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

// NodeStageVolume 1.格式化块，并挂载磁盘到节点的全局路径
func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	diskID := req.VolumeId
	targetGlobalPath := req.GetStagingTargetPath()

	if diskID == "" || targetGlobalPath == "" {
		log.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
		return nil, fmt.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
	}
	if value, ok := diskStagingMap.Load(targetGlobalPath); ok {
		switch value {
		case Staging:
			log.Warnf("NodeStageVolume: diskID: %s is in staging, please wait", diskID)
			return nil, fmt.Errorf("NodeStageVolume: diskID: %s is in staging, please wait", diskID)
		case ErrorStatus:
			log.Errorf("NodeStageVolume: diskID: %s staging process error, please deal with it manual", diskID)
			return nil, fmt.Errorf("NodeStageVolume: diskID: %s staging process error, please deal with it manual", diskID)
		case targetGlobalPath:
			log.Warnf("NodeStageVolume: diskID: %s has been staged to targetGlobalPath: %s", diskID, targetGlobalPath)
			return &csi.NodeStageVolumeResponse{}, nil
		default:
			log.Warnf("NodeStageVolume: diskID: %s has been staged to targetGlobalPath: %s, and still going to staged to another targetGlobalPath: %s", diskID, value, targetGlobalPath)
		}
	}
	res, err := describeBlockInfo(diskID, "")
	if err != nil {
		log.Errorf("NodeStageVolume: find disk uuid failed, err is: %s", err)
		return nil, err
	}
	if res.Data.BlockId == "" || res.Data.Order == 0 {
		log.Errorf("NodeStageVolume: find disk order id failed")
		return nil, fmt.Errorf("NodeStageVolume: find disk order id failed")
	}
	deviceName, err := findDeviceNameByOrderId(fmt.Sprintf("%s%d", OrderHead, res.Data.Order))
	if err != nil {
		log.Errorf("NodeStageVolume: findDeviceNameByUuid error, err is: %s", err.Error())
		return nil, err
	}
	log.Debugf("NodeStageVolume: findDeviceNameByVolumeID succeed, deviceName is: %s", deviceName)
	fsType := req.GetVolumeContext()["fsType"]
	diskStagingMap.Store(targetGlobalPath, Staging)

	if _, ok := diskFormattedMap.Load(diskID); ok || n.GetPvFormat(diskID) {
		log.Warnf("NodeStageVolume: diskID: %s had been formatted, ignore multi format", diskID)
	} else {
		// 3 格式化盘
		if err = formatDiskDevice(diskID, deviceName, fsType); err != nil {
			diskStagingMap.Store(targetGlobalPath, ErrorStatus)
			log.Errorf("NodeStageVolume: format deviceName: %s failed, err is: %s", deviceName, err.Error())
			return nil, err
		}
		err = n.SavePvFormat(diskID)
		if err != nil {
			log.Infof("store pv %s format failed,err=%v", diskID, err)
		}
		log.Debugf("NodeStageVolume: Step 1: formatDiskDevice successfully!")
	}
	log.Debugf("NodeStageVolume: targetGlobalPath exist flag: %t", utils.FileExisted(targetGlobalPath))
	if !utils.FileExisted(targetGlobalPath) {
		if err = utils.CreateDir(targetGlobalPath, mountPointMode); err != nil {
			diskStagingMap.Store(targetGlobalPath, ErrorStatus)
			log.Errorf("NodeStageVolume: Step 1, targetGlobalPath is not exist, but unable to create it, err is: %s", err.Error())
			return nil, fmt.Errorf("NodeStageVolume: Step 1, targetGlobalPath is not exist, but unable to create it, err is: %s", err.Error())
		}
		log.Debugf("NodeStageVolume: Step 1, targetGlobalPath: %s is not exist, and create succeed", targetGlobalPath)
	}
	err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(targetGlobalPath, "\n"), fsType)
	if err != nil {
		diskStagingMap.Store(targetGlobalPath, ErrorStatus)
		log.Errorf("NodeStageVolume: Step 2, mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
		return nil, fmt.Errorf("NodeStageVolume: Step 2, mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
	}
	diskStagingMap.Store(targetGlobalPath, targetGlobalPath)
	log.Debugf("NodeStageVolume: Step 2, mountDiskDeviceToNodeGlobalPath: %s successfully!", targetGlobalPath)

	log.Infof("NodeStageVolume: Successfully!")
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodePublishVolume 2.将挂载节点的全局路径绑定到pod目录
func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume:: starting to mount bind stagingTargetPath to pod directory with req: %+v", req)

	// Step 1: check necessary params
	volumeID := req.VolumeId
	stagingTargetPath := req.GetStagingTargetPath()
	podPath := req.GetTargetPath()
	diskVol := req.GetVolumeContext()

	if volumeID == "" {
		log.Errorf("NodePublishVolume:: req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume:: step 1, req.VolumeID cant not be empty")
	}

	if stagingTargetPath == "" {
		log.Errorf("NodePublishVolume: req.StagingTargetPath cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume: step 1, req.StagingTargetPath cant not be empty")
	}
	if podPath == "" {
		log.Errorf("NodePublishVolume:: req.Targetpath can not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume:: step 1, req.Targetpath can not be empty")
	}

	// Step 2: check podPath
	if !utils.FileExisted(podPath) {
		if err := utils.CreateDir(podPath, mountPointMode); err != nil {
			log.Errorf("NodePublishVolume:: req.TargetPath(podPath): %s is not exist, but unable to create it, err is: %s", podPath, err.Error())
			return nil, fmt.Errorf("NodePublishVolume:: step 3, req.TargetPath(podPath): %s is not exist, but unable to create it, err is: %s", podPath, err.Error())
		}

		log.Debugf("NodePublishVolume:: req.TargetPath(podPath): %s is not exist, and create it succeed!", podPath)
	}

	if utils.Mounted(podPath) {
		log.Warnf("NodePublishVolume:: req.TargetPath(podPath): %s has been mounted, return directly", podPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}
	// Step 3: bind stagingTargetPath to pod directory
	if value, ok := diskPublishingMap.Load(podPath); ok {
		switch value {
		case Publishing:
			log.Warnf("NodePublishVolume: volumeID: %s is in publishing, please wait", volumeID)
			if _, ok := diskPublishedMap.Load(podPath); ok {
				log.Debugf("NodePublishVolume: volumeID: %s publishing process succeed, return context", volumeID)
				return &csi.NodePublishVolumeResponse{}, nil
			}
			return nil, fmt.Errorf("NodePublishVolume: volumeID: %s publishing process error", volumeID)
		case ErrorStatus:
			log.Errorf("NodePublishVolume: volumeID: %s publishing process error", volumeID)
			return nil, fmt.Errorf("NodePublishVolume: volumeID: %s publishing process error", volumeID)
		}
	}
	// check if device mounted to node global, if not mount it
	diskPublishingMap.Store(podPath, Publishing)
	res, err := describeBlockInfo(volumeID, "")
	if err != nil {
		diskPublishingMap.Delete(podPath)
		log.Errorf("NodePublishVolume: cdsDisk.FindDiskByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("NodePublishVolume: cdsDisk.FindDiskByVolumeID error, err is: %s", err)
	}
	if res.Data.BlockId == "" {
		diskPublishingMap.Delete(podPath)
		log.Errorf("NodePublishVolume: findDiskByVolumeID res is nil")
		return nil, fmt.Errorf("NodeStageVolume: findDiskByVolumeID res is nil")
	}

	// check if disk formatted or not, return error if not formatted
	if _, ok := diskFormattedMap.Load(volumeID); ok || n.GetPvFormat(volumeID) {
		deviceName, err := findDeviceNameByOrderId(fmt.Sprintf("%s%d", OrderHead, res.Data.Order))
		if err != nil {
			log.Errorf("NodePublishVolume: findDeviceNameByUuid error, err is: %s", err.Error())
			return nil, err
		}

		// staging record exist and record staging path is equal to new staging path
		if ok := utils.Mounted(stagingTargetPath); ok {
			log.Warnf("NodePublishVolume: diskID: %s has been staged to stagingTargetPath: %s, direct to bind mount", volumeID, stagingTargetPath)
			err = bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
			if err != nil {
				diskPublishingMap.Store(podPath, ErrorStatus)
				log.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
				return nil, fmt.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
			}

			diskPublishingMap.Store(podPath, stagingTargetPath)
			log.Infof("NodePublishVolume:: Successfully!")
			return &csi.NodePublishVolumeResponse{}, nil
		}

		// staging record not exist or record staging path is different from new staging path
		// need staging and bind mount two steps
		log.Warnf("NodePublishVolume: diskID: %s formatted, need staging and bind mount two steps", volumeID)

		// first staging
		err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(stagingTargetPath, "\n"), diskVol["fsType"])
		if err != nil {
			diskPublishingMap.Store(podPath, ErrorStatus)
			log.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
			return nil, fmt.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
		}

		// second bind mount
		err = bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
		if err != nil {
			diskPublishingMap.Store(podPath, ErrorStatus)
			log.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
			return nil, fmt.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
		}
		diskPublishingMap.Delete(diskPublishingMap)
		diskPublishedMap.Store(podPath, podPath)
		log.Infof("NodePublishVolume:: Successfully!")
		return &csi.NodePublishVolumeResponse{}, nil
	}

	log.Errorf("NodePublishVolume: diskID: %s is not formatted, cant mount and bing mount", volumeID)
	return nil, fmt.Errorf("NodePublishVolume: diskID: %s is not formatted, cant mount and bing mount", volumeID)
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume:: starting to unbind mount volumeID: %s, targetPath(podPath): %s", req.VolumeId, req.TargetPath)

	// Step 1: check targetPath
	targetPath := req.GetTargetPath()
	if !utils.FileExisted(targetPath) {
		log.Error("NodeUnpublishVolume:: step 1, req.TargetPath(podPath) is not exist")
		return nil, fmt.Errorf("NodeUnpublishVolume:: step 1, req.TargetPath(podPath) is not exist")
	}

	// Step 2: unbind mount targetPath
	if value, ok := diskUnPublishingMap.Load(targetPath); ok {
		switch value {
		case UnPublishing:
			log.Warnf("NodeUnpublishVolume: targetPath: %s is in unPublishing, please wait", targetPath)
			return nil, fmt.Errorf("NodePublishVolume: targetPath: %s is in unPublishing, please wait", targetPath)
		case ErrorStatus:
			log.Errorf("NodeUnpublishVolume: targetPath: %s unPublishing process error", targetPath)
			return nil, fmt.Errorf("NodeUnpublishVolume: targetPath: %s unPublishing process error", targetPath)
		case Ok:
			log.Warnf("NodeUnpublishVolume: targetPath: %s unPublishing process succeed, return context", targetPath)
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}
	}

	diskUnPublishingMap.Store(targetPath, UnPublishing)
	err := unBindMountGlobalPathFromPodPath(targetPath)
	if err != nil {
		diskUnPublishingMap.Store(targetPath, ErrorStatus)
		log.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
		return nil, fmt.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
	}

	diskUnPublishingMap.Store(targetPath, Ok)
	diskPublishingMap.Delete(targetPath)
	log.Infof("NodeUnpublishVolume:: Successfully!")

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	log.Infof("NodeUnstageVolume: starting to unstage with req: %+v", req)

	// Step 1: get necessary params
	volumeID := req.VolumeId
	unStagingPath := req.GetStagingTargetPath()

	if volumeID == "" {
		log.Errorf("NodeUnstageVolume:: step 1, req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume:: step 1, req.VolumeID cant not be empty")
	}

	if unStagingPath == "" {
		log.Errorf("NodeUnstageVolume: step 1, req.StagingTargetPath cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume: step 1, req.StagingTargetPath cant not be empty")
	}

	// Step 2: umount disk device from node global path
	if value, ok := diskUnStagingMap.Load(unStagingPath); ok {
		switch value {
		case UnStaing:
			log.Warnf("NodeUnstageVolume: volumeID: %s in is unstaging process, please wait", volumeID)
			return nil, fmt.Errorf("NodeUnstageVolume: volumeID: %s in is unstaging process, please wait", volumeID)
		case ErrorStatus:
			log.Errorf("NodeUnstageVolume: volumeID: %s unstaging process error", volumeID)
			return nil, fmt.Errorf("NodeUnstageVolume: volumeID: %s unstaging process error", volumeID)
		case Ok:
			log.Warnf("NodeUnstageVolume: volumeID: %s unstaging process succeed, return context", volumeID)
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
	}

	if !utils.FileExisted(unStagingPath) {
		diskStagingMap.Delete(unStagingPath)
		log.Warnf("NodeUnstageVolume: unStagingPath is not exist, should must be already unmount, retrun directly")
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	diskUnStagingMap.Store(unStagingPath, UnStaing)
	err := unMountDiskDeviceFromNodeGlobalPath(volumeID, unStagingPath)
	if err != nil {
		diskUnStagingMap.Store(unStagingPath, ErrorStatus)
		log.Errorf("NodeUnstageVolume: step 2, unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", err)
		return nil, fmt.Errorf("NodeUnstageVolume: step 2, unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", err)
	}
	diskUnStagingMap.Delete(unStagingPath)
	diskStagingMap.Delete(unStagingPath)
	log.Infof("NodeUnstageVolume: Successfully!")

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func findDeviceNameByOrderId(orderId string) (deviceName string, err error) {
	log.Infof("findDeviceNameByUuid: disk order id is: %s", orderId)
	cmdScan := fmt.Sprintf("ls /dev/disk/by-id/%s -lh", orderId)
	deviceNameStr, err := utils.RunCommand(cmdScan)
	if err != nil {
		log.Errorf("findDeviceNameByUuid: cmdScan: %s failed, err is: %s\n", cmdScan, err)
		return "", err
	}
	l := strings.Split(deviceNameStr, " ")
	if len(l) > 1 {
		temp := l[len(l)-1]
		tempL := strings.Split(temp, "/")
		if len(tempL) > 1 {
			deviceName = "/dev/" + tempL[len(tempL)-1]
			deviceName = strings.ReplaceAll(deviceName, "\n", "")
		}
	}
	return
}

func formatDiskDevice(diskId, deviceName, fsType string) error {
	log.Infof("formatDiskDevice: deviceName is: %s, fsType is: %s", deviceName, fsType)

	// Step 1: check deviceName(disk) is scannable
	scanDeviceCmd := fmt.Sprintf("fdisk -l | grep %s | grep -v grep", deviceName)
	if _, err := utils.RunCommand(scanDeviceCmd); err != nil {
		log.Errorf("formatDiskDevice: scanDeviceCmd: %s failed, err is: %s", scanDeviceCmd, err.Error())
		return err
	}
	// check deviceName is formatted
	if out, err := utils.RunCommand(fmt.Sprintf("blkid %s", deviceName)); err == nil {
		if strings.Contains(out, "TYPE") {
			diskFormattedMap.Store(diskId, Formatted)
			log.Warnf("formatDiskDevice: deviceName: %s had been formatted, avoid multi formatting, return directly", deviceName)
			return nil
		}
	}
	// Step 2: format deviceName(disk)
	var formatDeviceCmd string
	if fsType == DefaultFsTypeXfs {
		formatDeviceCmd = fmt.Sprintf("mkfs.xfs %s", deviceName)
	} else if fsType == FsTypeExt3 {
		formatDeviceCmd = fmt.Sprintf("mkfs.ext3 %s", deviceName)
	} else if fsType == FsTypeExt4 {
		formatDeviceCmd = fmt.Sprintf("mkfs.ext4 %s", deviceName)
	} else {
		log.Errorf("formatDiskDevice: fsType not support, should be [ext4/ext3/xfs]")
		return fmt.Errorf("formatDiskDevice: fsType not support, should be [ext4/ext3/xfs]")
	}

	if out, err := utils.RunCommand(formatDeviceCmd); err != nil {
		if strings.Contains(out, "existing filesystem") {
			diskFormattedMap.Store(diskId, Formatted)
			log.Warnf("formatDiskDevice: deviceName: %s had been formatted, avoid multi formatting, return directly", deviceName)
			// log.Infof("formatDiskDevice: Successfully!")
			return nil
		}
		log.Errorf("formatDiskDevice: formatDeviceCmd: %s failed, err is: %s", formatDeviceCmd, err.Error())
		return err
	}

	// storing formatted disk
	diskFormattedMap.Store(diskId, Formatted)
	log.Infof("formatDiskDevice: Successfully!")
	return nil
}

func mountDiskDeviceToNodeGlobalPath(deviceName, targetGlobalPath, fstype string) error {
	log.Infof("mountDiskDeviceToNodeGlobalPath: deviceName is: %s, targetGlobalPath is: %s, fstype is: %s", deviceName, targetGlobalPath, fstype)
	// 查询uuid
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
		log.Errorf("mountDiskDeviceToNodeGlobalPath: err is: %s", err)
		return err
	}
	log.Infof("mountDiskDeviceToNodeGlobalPath: Successfully!")

	return nil
}

func bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath string) error {
	log.Infof("bindMountGlobalPathToPodPath: volumeID: %s, stagingTargetPath:%s, targetPath:%s", volumeID, stagingTargetPath, podPath)

	cmd := fmt.Sprintf("mount --bind %s %s", stagingTargetPath, podPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		log.Errorf("bindMountGlobalPathToPodPath: volumeID:%s, bind mount stagingTargetPath: %s to podPath: %s failed, err is: %s", volumeID, stagingTargetPath, podPath, err.Error())
		return fmt.Errorf("bindMountGlobalPathToPodPath: volumeID:%s, bind mount stagingTargetPath: %s to podPath: %s failed, err is: %s", volumeID, stagingTargetPath, podPath, err.Error())
	}

	log.Infof("bindMountGlobalPathToPodPath: Successfully!")

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
				log.Errorf("unMountDiskDeviceFromNodeGlobalPath: unStagingPath is busy, but kill occupied process failed, err is: %s", err.Error())
				return fmt.Errorf("unMountDiskDeviceFromNodeGlobalPath: unStagingPath is busy, but kill occupied process failed, err is: %s", err.Error())
			}

			log.Warnf("unMountDiskDeviceFromNodeGlobalPath: unStagingPath is busy and kill occupied process succeed!")

			if _, err = utils.RunCommand(cmdUnstagingPath); err != nil {
				log.Errorf("unMountDiskDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed(again), err is: %s", unStagingPath, err.Error())
				return fmt.Errorf("unMountDiskDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed(again), err is: %s", unStagingPath, err.Error())
			}

			log.Infof("unMountDiskDeviceFromNodeGlobalPath: Successfully!")
			return nil
		}

		log.Errorf("unMountDiskDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed, err is: %s", unStagingPath, err.Error())
		return fmt.Errorf("unMountDiskDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed, err is: %s", unStagingPath, err.Error())
	}

	log.Infof("unMountDiskDeviceFromNodeGlobalPath: Successfully!")

	return nil

}

func unBindMountGlobalPathFromPodPath(targetPath string) error {
	log.Infof("unBindMountGlobalPathFromPodPath: targetPath:%s", targetPath)

	cmdUnBindMount := fmt.Sprintf("umount %s", targetPath)
	if _, err := utils.RunCommand(cmdUnBindMount); err != nil {
		if strings.Contains(err.Error(), "target is busy") || strings.Contains(err.Error(), "device is busy") {
			log.Warnf("unBindMountGlobalPathFromPodPath: unStagingPath is busy(occupied by another process)")

			cmdKillProcess := fmt.Sprintf("fuser -m -k %s", targetPath)
			if _, err = utils.RunCommand(cmdKillProcess); err != nil {
				log.Errorf("unBindMountGlobalPathFromPodPath: targetPath is busy, but kill occupied process failed, err is: %s", err.Error())
				return fmt.Errorf("unBindMountGlobalPathFromPodPath: targetPath is busy, but kill occupied process failed, err is: %s", err.Error())
			}

			log.Warnf("unBindMountGlobalPathFromPodPath: targetPath is busy and kill occupied process succeed!")

			if err = utils.Unmount(targetPath); err != nil {
				log.Errorf("unBindMountGlobalPathFromPodPath: unmount req.TargetPath:%s failed(again), err is: %s", targetPath, err.Error())
				return fmt.Errorf("unBindMountGlobalPathFromPodPath: unmount req.TargetPath:%s failed(again), err is: %s", targetPath, err.Error())
			}

			log.Infof("unBindMountGlobalPathFromPodPath: Successfully!")
			return nil
		}

		log.Errorf("unBindMountGlobalPathFromPodPath: unmount req.TargetPath:%s failed, err is: %s", targetPath, err.Error())
		return fmt.Errorf("unBindMountGlobalPathFromPodPath: unmount req.TargetPath:%s failed, err is: %s", targetPath, err.Error())
	}

	log.Infof("unBindMountGlobalPathFromPodPath: Successfully!")

	return nil

}

// SavePvFormat openapi Format
func (n *NodeServer) SavePvFormat(diskId string) error {
	log.Infof("start SavePvFormat %s", diskId)
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewUpdateBlockFormatRequest()
	request.IsFormat = 1
	request.BlockId = diskId
	resp, err := client.UpdateBlockFormat(request)
	if err != nil {
		return err
	} else {
		if resp.Code == "Success" {
			diskFormattedMap.Store(diskId, Formatted)
		} else {
			return fmt.Errorf("%v", resp.Msg)
		}
	}
	return err
}

// GetPvFormat 查询disk是否Format
func (n *NodeServer) GetPvFormat(diskId string) bool {
	_, ok := diskFormattedMap.Load(diskId)
	if ok {
		return ok
	} else {
		resp, _ := describeBlockInfo(diskId, "")
		if resp.Data.NodeId != "" {
			return resp.Data.IsFormat == 1
		} else {
			return false
		}
	}
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
