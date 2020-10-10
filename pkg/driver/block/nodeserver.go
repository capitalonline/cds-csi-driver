package block

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

	cdsBlock "github.com/capitalonline/cck-sdk-go/pkg/cck/block"
)

// storing staging block
var blockStagingMap = map[string]string{}

// storing unstaging block
var blockUnstagingMap = map[string]string{}

// storing publishing block
var blockPublishingMap = map[string]string{}

// storing published block
var blockPublishedMap = map[string]string{}

// storing unpublishing block
var blockUnpublishingMap = map[string]string{}

// storing formatted block
var blockFormattedMap = map[string]string{}

func NewNodeServer(d *BlockDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
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

	// block Metric enable config
	nodeSvcCap := []*csi.NodeServiceCapability{nodeCap}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nodeSvcCap,
	}, nil
}

// bind mount node's global path to pod directory
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

		log.Infof("NodePublishVolume:: req.TargetPath(podPath): %s is not exist, and create it succeed!", podPath)
	}

	if utils.Mounted(podPath) {
		log.Warnf("NodePublishVolume:: req.TargetPath(podPath): %s has been mounted, return directly", podPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Step 3: bind stagingTargetPath to pod directory
	if value, ok := blockPublishingMap[podPath]; ok {
		if value == "publishing" {
			log.Warnf("NodePublishVolume: volumeID: %s is in publishing, please wait", volumeID)
			if _, ok := blockPublishedMap[podPath]; ok {
				log.Infof("NodePublishVolume: volumeID: %s publishing process succeed, return context", volumeID)
				return &csi.NodePublishVolumeResponse{}, nil
			}
			return nil, fmt.Errorf("NodePublishVolume: volumeID: %s publishing process error", volumeID)
		}
	}

	// check if device mounted to node global, if not mount it
	blockPublishingMap[podPath] = "publishing"
	defer delete(blockPublishingMap, podPath)

	res, err := cdsBlock.FindBlockByVolumeID(&cdsBlock.FindBlockByVolumeIDArgs{
		VolumeID: volumeID,
	})
	if err != nil {
		log.Errorf("NodePublishVolume: cdsBlock.findBlockByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("NodePublishVolume: cdsBlock.findBlockByVolumeID error, err is: %s", err)
	}
	if res.Data.BlockSlice == nil {
		log.Errorf("NodePublishVolume: findBlockByVolumeID res is nil")
		return nil, fmt.Errorf("NodeStageVolume: findBlockByVolumeID res is nil")
	}

	if res.Data.BlockSlice[0].Uuid == "" {
		log.Errorf("NodePublishVolume: findDeviceNameByVolumeID res uuid is null")
		return nil, fmt.Errorf("NodePublishVolume: findDeviceNameByVolumeID res uuid is null")
	}

	// check if block formatted or not, return error if not formatted
	if _, ok := blockFormattedMap[volumeID]; ok || res.Data.BlockSlice[0].IsFormat == 1 {
		deviceName, err := findDeviceNameByUuid(res.Data.BlockSlice[0].Uuid)
		if err != nil {
			log.Errorf("NodePublishVolume: findDeviceNameByUuid error, err is: %s", err.Error())
			return nil, err
		}

		// staging record exist and record staging path is equal to new staging path
		if ok := utils.Mounted(stagingTargetPath); ok {
			log.Warnf("NodePublishVolume: blockID: %s has been staged to stagingTargetPath: %s, direct to bind mount", volumeID, stagingTargetPath)
			err = bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
			if err != nil {
				log.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
				return nil, fmt.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
			}

			log.Infof("NodePublishVolume:: Successfully!")
			return &csi.NodePublishVolumeResponse{}, nil
		}

		// staging record not exist or record staging path is different from new staging path
		// need staging and bind mount two steps
		log.Warnf("NodePublishVolume: blockID: %s formatted, need staging and bind mount two steps", volumeID)

		// first staging
		err = mountBlockDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(stagingTargetPath, "\n"), diskVol["fsType"])
		if err != nil {
			log.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
			return nil, fmt.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
		}

		// second bind mount
		err = bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
		if err != nil {
			log.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
			return nil, fmt.Errorf("NodePublishVolume:: bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
		}

		// blockPublishingMap[podPath] = stagingTargetPath
		blockPublishedMap[podPath] = podPath
		log.Infof("NodePublishVolume:: Successfully!")
		return &csi.NodePublishVolumeResponse{}, nil
	}

	log.Errorf("NodePublishVolume: blockID: %s is not formatted, cant mount and bing mount", volumeID)
	return nil, fmt.Errorf("NodePublishVolume: blockID: %s is not formatted, cant mount and bing mount", volumeID)
}

// unbind mount node's global path from pod directory
func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume:: starting to unbind mount volumeID: %s, targetPath(podPath): %s", req.VolumeId, req.TargetPath)

	// Step 1: check targetPath
	targetPath := req.GetTargetPath()
	if !utils.FileExisted(targetPath) {
		log.Error("NodeUnpublishVolume:: step 1, req.TargetPath(podPath) is not exist")
		return nil, fmt.Errorf("NodeUnpublishVolume:: step 1, req.TargetPath(podPath) is not exist")
	}

	// Step 2: unbind mount targetPath
	if value, ok := blockUnpublishingMap[targetPath]; ok && value == "unPublishing" {
		log.Warnf("NodeUnpublishVolume: targetPath: %s is in unPublishing, please wait", targetPath)
		return nil, fmt.Errorf("NodeUnpublishVolume: targetPath: %s is in unPublishing, please wait", targetPath)
	}

	if ok := utils.Mounted(targetPath); !ok {
		log.Warnf("NodeUnpublishVolume: targetPath: %s had been unmounted, return context", targetPath)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	blockUnpublishingMap[targetPath] = "unPublishing"
	defer delete(blockUnpublishingMap, targetPath)

	err := unBindMountGlobalPathFromPodPath(targetPath)
	if err != nil {
		log.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
		return nil, fmt.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
	}

	log.Infof("NodeUnpublishVolume:: Successfully!")

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// mount deviceName to node's global path and format block by fstype
func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	// Step 1: get necessary params
	blockID := req.VolumeId
	targetGlobalPath := req.GetStagingTargetPath()

	if blockID == "" || targetGlobalPath == "" {
		log.Errorf("NodeStageVolume: [blockID/NodeStageVolume] cant not be empty")
		return nil, fmt.Errorf("NodeStageVolume: [blockID/NodeStageVolume] cant not be empty")
	}

	log.Infof("NodeStageVolume: starting to stage volumeID: %s, targetGlobalPath is: %s", blockID, targetGlobalPath)

	// Step 2: check block formatted or not, block staged or not
	if value, ok := blockStagingMap[targetGlobalPath]; ok && value == "staging" {
		log.Warnf("NodeStageVolume: blockID: %s is in staging, please wait", blockID)
		return nil, fmt.Errorf("NodeStageVolume: blockID: %s is in staging, please wait", blockID)
	}

	blockStagingMap[targetGlobalPath] = "staging"
	defer delete(blockStagingMap, targetGlobalPath)

	// Step 2: format block
	// Step 2-1: get deviceName
	res, err := findBlockByVolumeID(blockID)
	if err != nil {
		log.Errorf("NodeStageVolume: find block uuid failed, err is: %s", err)
		return nil, err
	}

	if res.Data.BlockSlice == nil {
		log.Errorf("NodeStageVolume: findBlockByVolumeID res is nil")
		return nil, fmt.Errorf("NodeStageVolume: findBlockByVolumeID res is nil")
	}

	if res.Data.BlockSlice[0].Uuid == "" {
		log.Errorf("NodeStageVolume: findDeviceNameByVolumeID res uuid is null")
		return nil, fmt.Errorf("NodeStageVolume: findDeviceNameByVolumeID res uuid is null")
	}

	deviceName, err := findDeviceNameByUuid("nqn.2019-06.suzaku.org:ssd_pool.g" + res.Data.BlockSlice[0].Uuid)
	if err != nil {
		log.Errorf("NodeStageVolume: findDeviceNameByUuid error, err is: %s", err.Error())
		return nil, err
	}

	log.Infof("NodeStageVolume: findDeviceNameByVolumeID succeed, deviceName is: %s", deviceName)

	// Step 2-2: format block
	diskVol := req.GetVolumeContext()
	fsType := diskVol["fsType"]

	if _, ok := blockFormattedMap[blockID]; ok || res.Data.BlockSlice[0].IsFormat == 1 {
		log.Warnf("NodeStageVolume: blockID: %s had been formatted, ignore multi format", blockID)
	} else {
		if err = formatBlockDevice(blockID, deviceName, fsType); err != nil {
			blockStagingMap[targetGlobalPath] = "error"
			log.Errorf("NodeStageVolume: format deviceName: %s failed, err is: %s", deviceName, err.Error())
			return nil, err
		}
		log.Infof("NodeStageVolume: Step 1: formatBlockDevice successfully!")
	}

	// Step 3: mount block to node's global path
	// Step 3-1: check targetGlobalPath
	log.Infof("NodeStageVolume: targetGlobalPath exist flag: %t", utils.FileExisted(targetGlobalPath))
	if !utils.FileExisted(targetGlobalPath) {
		if err = utils.CreateDir(targetGlobalPath, mountPointMode); err != nil {
			blockStagingMap[targetGlobalPath] = "error"
			log.Errorf("NodeStageVolume: Step 1, targetGlobalPath is not exist, but unable to create it, err is: %s", err.Error())
			return nil, fmt.Errorf("NodeStageVolume: Step 1, targetGlobalPath is not exist, but unable to create it, err is: %s", err.Error())
		}
		log.Infof("NodeStageVolume: Step 1, targetGlobalPath: %s is not exist, and create succeed", targetGlobalPath)
	}

	// Step 3-2: mount deviceName to node's global path
	if ok := utils.Mounted(targetGlobalPath); ok {
		log.Warnf("NodeStageVolume: targetGlobalPath: %s had been mounted, return context", targetGlobalPath)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	err = mountBlockDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(targetGlobalPath, "\n"), fsType)
	if err != nil {
		blockStagingMap[targetGlobalPath] = "error"
		log.Errorf("NodeStageVolume: Step 2, mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
		return nil, fmt.Errorf("NodeStageVolume: Step 2, mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
	}

	log.Infof("NodeStageVolume: Successfully!")

	return &csi.NodeStageVolumeResponse{}, nil
}

// unmount deviceName from node's global path
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

	// Step 2: umount block device from node global path
	if value, ok := blockUnstagingMap[unStagingPath]; ok && value == "unstaing" {
		log.Warnf("NodeUnstageVolume: volumeID: %s in is unstaging process, please wait", volumeID)
		return nil, fmt.Errorf("NodeUnstageVolume: volumeID: %s in is unstaging process, please wait", volumeID)
	}

	if !utils.FileExisted(unStagingPath) {
		delete(blockStagingMap, unStagingPath)
		log.Warnf("NodeUnstageVolume: unStagingPath is not exist, should must be already unmount, retrun directly")
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	blockUnstagingMap[unStagingPath] = "unstaging"
	defer delete(blockUnstagingMap, unStagingPath)

	err := unMountBlockDeviceFromNodeGlobalPath(volumeID, unStagingPath)
	if err != nil {
		blockUnstagingMap[unStagingPath] = "error"
		log.Errorf("NodeUnstageVolume: step 2, unMountBlockDeviceFromNodeGlobalPath failed, err is: %s", err)
		return nil, fmt.Errorf("NodeUnstageVolume: step 2, unMountBlockDeviceFromNodeGlobalPath failed, err is: %s", err)
	}

	log.Infof("NodeUnstageVolume: Successfully!")

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func findDeviceNameByUuid(blockUuid string) (string, error) {
	log.Infof("findDeviceNameByUuid: blockUuid is: %s", blockUuid)

	// diskUuidFormat := strings.ReplaceAll(blockUuid, "-", "")
	cmdScan := "ls -lrt /sys/block/ | grep nvme | awk '{print $9}'"
	deviceNameStr, err := utils.RunCommand(cmdScan)
	if err != nil {
		log.Errorf("findDeviceNameByUuid: cmdScan: %s failed, err is: %s", cmdScan, err)
		return "", err
	}

	log.Infof("findDeviceNameByUuid: deviceNameStr : %s", deviceNameStr)
	// get device such as /dev/sda
	deviceNameStr = strings.ReplaceAll(deviceNameStr, "\n", " ")
	log.Infof("findDeviceNameByUuid: deviceNameStr : %s", deviceNameStr)
	deviceNameUuid := map[string]string{}
	for _, deviceName := range strings.Split(deviceNameStr, " ") {
		if deviceName == "" {
			continue
		}
		cmdGetUid := fmt.Sprintf("cat /sys/block/%s/device/subsysnqn", deviceName)
		log.Infof("findDeviceNameByUuid: cmdGetUid is: %s", cmdGetUid)
		uuidStr, err := utils.RunCommand(cmdGetUid)
		if err != nil {
			log.Errorf("findDeviceNameByUuid: get deviceName: %s uuid failed, err is: %s", deviceName, err)
			return "", err
		}

		deviceNameUuid[uuidStr] = deviceName
	}

	log.Infof("findDeviceNameByUuid: deviceNameUuid is: %+v", deviceNameUuid)

	// compare
	if _, ok := deviceNameUuid[blockUuid]; !ok {
		log.Errorf("findDeviceNameByUuid: blockUuid: %s is not exist", blockUuid)
		return "", fmt.Errorf("findDeviceNameByUuid: blockUuid: %s is not exist", blockUuid)
	}

	deviceName := deviceNameUuid[blockUuid]
	if deviceName == "" {
		log.Errorf("findDeviceNameByUuid: blockUuid: %s, deviceName is empty", blockUuid)
		return "", fmt.Errorf("findDeviceNameByUuid: blockUuid: %s, deviceName is empty", blockUuid)
	}

	log.Infof("findDeviceNameByUuid: successfully, blockUuid: %s, deviceName is: %s", blockUuid, deviceNameUuid[blockUuid])

	return deviceName, nil
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

func mountBlockDeviceToNodeGlobalPath(deviceName, targetGlobalPath, fstype string) error {
	log.Infof("mountBlockDeviceToNodeGlobalPath: deviceName is: %s, targetGlobalPath is: %s, fstype is: %s", deviceName, targetGlobalPath, fstype)

	cmd := fmt.Sprintf("mount -t %s %s %s", fstype, deviceName, targetGlobalPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		log.Errorf("mountBlockDeviceToNodeGlobalPath: err is: %s", err)
		return err
	}

	log.Infof("mountBlockDeviceToNodeGlobalPath: Successfully!")

	return nil
}

func unMountBlockDeviceFromNodeGlobalPath(volumeID, unStagingPath string) error {
	log.Infof("unMountDeviceFromNodeGlobalPath: volumeID is: %s, unStagingPath: %s", volumeID, unStagingPath)

	cmdUnstagingPath := fmt.Sprintf("umount %s", unStagingPath)
	if _, err := utils.RunCommand(cmdUnstagingPath); err != nil {
		if strings.Contains(err.Error(), "target is busy") || strings.Contains(err.Error(), "device is busy") {
			log.Warnf("unMountBlockDeviceFromNodeGlobalPath: unStagingPath is busy(occupied by another process)")

			cmdKillProcess := fmt.Sprintf("fuser -m -k %s", unStagingPath)
			if _, err = utils.RunCommand(cmdKillProcess); err != nil {
				log.Errorf("unMountBlockDeviceFromNodeGlobalPath: unStagingPath is busy, but kill occupied process failed, err is: %s", err.Error())
				return fmt.Errorf("unMountBlockDeviceFromNodeGlobalPath: unStagingPath is busy, but kill occupied process failed, err is: %s", err.Error())
			}

			log.Warnf("unMountBlockDeviceFromNodeGlobalPath: unStagingPath is busy and kill occupied process succeed!")

			if _, err = utils.RunCommand(cmdUnstagingPath); err != nil {
				log.Errorf("unMountBlockDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed(again), err is: %s", unStagingPath, err.Error())
				return fmt.Errorf("unMountBlockDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed(again), err is: %s", unStagingPath, err.Error())
			}

			log.Infof("unMountBlockDeviceFromNodeGlobalPath: Successfully!")
			return nil
		}

		log.Errorf("unMountBlockDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed, err is: %s", unStagingPath, err.Error())
		return fmt.Errorf("unMountBlockDeviceFromNodeGlobalPath: unmount device from req.StagingTargetPath:%s failed, err is: %s", unStagingPath, err.Error())
	}

	log.Infof("unMountBlockDeviceFromNodeGlobalPath: Successfully!")

	return nil

}

func formatBlockDevice(blockID, deviceName, fsType string) error {
	log.Infof("formatBlockDevice: deviceName is: %s, fsType is: %s", deviceName, fsType)

	// Step 1: check deviceName(block) is scannable
	scanDeviceCmd := fmt.Sprintf("fdisk -l | grep %s | grep -v grep", deviceName)
	if _, err := utils.RunCommand(scanDeviceCmd); err != nil {
		log.Errorf("formatBlockDevice: scanDeviceCmd: %s failed, err is: %s", scanDeviceCmd, err.Error())
		return err
	}

	// Step 2: format deviceName(block)
	var formatDeviceCmd string
	if fsType == DefaultFsTypeXfs {
		formatDeviceCmd = fmt.Sprintf("mkfs.xfs %s", deviceName)
	} else if fsType == FsTypeExt3 {
		formatDeviceCmd = fmt.Sprintf("mkfs.ext3 %s", deviceName)
	} else if fsType == FsTypeExt4 {
		formatDeviceCmd = fmt.Sprintf("mkfs.ext4 %s", deviceName)
	} else {
		log.Errorf("formatBlockDevice: fsType not support, should be [ext4/ext3/xfs]")
		return fmt.Errorf("formatBlockDevice: fsType not support, should be [ext4/ext3/xfs]")
	}

	if out, err := utils.RunCommand(formatDeviceCmd); err != nil {
		if strings.Contains(out, "existing filesystem"){
			blockFormattedMap[blockID] = "formatted"
			log.Warnf("formatBlockDevice: deviceName: %s had been formatted, avoid multi formatting, return directly", deviceName)
			log.Infof("formatBlockDevice: Successfully!")
			return nil
		}
		log.Errorf("formatBlockDevice: formatDeviceCmd: %s failed, err is: %s", formatDeviceCmd, err.Error())
		return err
	}

	// storing formatted block
	blockFormattedMap[blockID] = "formatted"
	res, err := cdsBlock.UpdateBlockFormatFlag(&cdsBlock.UpdateBlockFormatFlagArgs{
		BlockID:  blockID,
		IsFormat: 1,
	})
	if err != nil || res.Code != "Success" {
		log.Errorf("formatBlockDevice: update  database error, err is: %s", err)
		return err
	}

	log.Infof("formatBlockDevice: Successfully!")

	return nil
}

