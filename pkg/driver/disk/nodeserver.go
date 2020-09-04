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
)

// storing staging disk
var diskStagingMap = map[string]string{}

// storing unstaging disk
var diskUnstagingMap = map[string]string{}

// storing publishing disk
var diskPublishingMap = map[string]string{}

// storing unpublishing disk
var diskUnpublishingMap = map[string]string{}

// storing formatted disk
var diskFormattedMap = map[string]string{}

func NewNodeServer(d *DiskDriver) *NodeServer {
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

	// Disk Metric enable config
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
		log.Errorf("NodePublishVolume:: step 1, req.VolumeID cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume:: step 1, req.VolumeID cant not be empty")
	}

	if stagingTargetPath == "" {
		log.Errorf("NodePublishVolume: step 1, req.StagingTargetPath cant not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume: step 1, req.StagingTargetPath cant not be empty")
	}
	if podPath == "" {
		log.Errorf("NodePublishVolume:: step 1, req.Targetpath can not be empty")
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume:: step 1, req.Targetpath can not be empty")
	}

	// Step 2: check podPath
	if !utils.FileExisted(podPath) {
		if err := utils.CreateDir(podPath, mountPointMode); err != nil {
			log.Errorf("NodePublishVolume:: step 3, req.TargetPath(podPath): %s is not exist, but unable to create it, err is: %s", podPath, err.Error())
			return nil, fmt.Errorf("NodePublishVolume:: step 3, req.TargetPath(podPath): %s is not exist, but unable to create it, err is: %s", podPath, err.Error())
		}

		log.Infof("NodePublishVolume:: step 3, req.TargetPath(podPath): %s is not exist, and create it succeed!", podPath)
	}

	if utils.Mounted(podPath) {
		log.Warnf("NodePublishVolume:: step 3, req.TargetPath(podPath): %s has been mounted, return directly", podPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Step 3: bind stagingTargetPath to pod directory
	if value, ok := diskPublishingMap[podPath]; ok {
		if value == "publishing" {
			log.Warnf("NodePublishVolume: volumeID: %s is in publishing, please wait", volumeID)
			return nil, fmt.Errorf("NodePublishVolume: volumeID: %s is in publishing, please wait", volumeID)
		} else if value == "error" {
			log.Errorf("NodePublishVolume: volumeID: %s publishing process error", volumeID)
			return nil, fmt.Errorf("NodePublishVolume: volumeID: %s publishing process error", volumeID)
		} else if value == stagingTargetPath {
			log.Errorf("NodePublishVolume: volumeID: %s has been published to stagingTargetPath: %s", volumeID, stagingTargetPath)
			return nil, fmt.Errorf("NodePublishVolume: volumeID: %s has been published to stagingTargetPath: %s", volumeID, stagingTargetPath)
		}

		log.Warnf("NodePublishVolume: volumeID: %s has been published to stagingTargetPath: %s, and still publish to another stagingTargetPath: %s", volumeID, value, stagingTargetPath)
	}

	// check if device mounted to node global, if not mount it
	if _, ok := diskFormattedMap[volumeID]; ok {
		log.Warnf("NodePublishVolume: formatted")
		log.Warnf("NodePublishVolume: diskStagingMap is: %+v", diskStagingMap)
		if value, ok := diskStagingMap[stagingTargetPath]; ok {
			log.Warnf("NodePublishVolume: value is: %s", value)
			if value != stagingTargetPath {
				log.Warnf("NodePublishVolume: diskID: %s staged to stagingTargetPath: %s, unstage firstly", volumeID, value)
				err := unMountDiskDeviceFromNodeGlobalPath(volumeID, value)
				if err != nil {
					log.Errorf("NodePublishVolume: diskID: %s staged to stagingTargetPath: %s, unstage error", volumeID, value)
					return nil, fmt.Errorf("NodePublishVolume: diskID: %s staged to stagingTargetPath: %s, unstage error", volumeID, value)
				}
				log.Warnf("NodePublishVolume: diskID: %s staged to stagingTargetPath: %s, unstage succeed, then to stage to stagingTargetPath: %s", volumeID, value, stagingTargetPath)

				res, err := findDiskByVolumeID(volumeID)
				if err != nil {
					log.Errorf("NodePublishVolume: find disk uuid failed, err is: %s", err)
					return nil, err
				}
				if res.Data.DiskSlice == nil {
					log.Errorf("NodePublishVolume: findDiskByVolumeID res is nil")
					return nil, fmt.Errorf("NodeStageVolume: findDiskByVolumeID res is nil")
				}

				if res.Data.DiskSlice[0].Uuid == "" {
					log.Errorf("NodePublishVolume: findDeviceNameByVolumeID res uuid is null")
					return nil, fmt.Errorf("NodePublishVolume: findDeviceNameByVolumeID res uuid is null")
				}

				deviceName, err := findDeviceNameByUuid(res.Data.DiskSlice[0].Uuid)
				if err != nil {
					log.Errorf("NodePublishVolume: findDeviceNameByUuid error, err is: %s", err.Error())
					return nil, err
				}

				diskPublishingMap[podPath] = "publishing"
				err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(stagingTargetPath, "\n"), diskVol["fsType"])
				if err != nil {
					diskStagingMap[stagingTargetPath] = "error"
					log.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
					return nil, fmt.Errorf("NodePublishVolume: mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
				}
			}
		}
	}

	diskPublishingMap[podPath] = "publishing"
	err := bindMountGlobalPathToPodPath(volumeID, stagingTargetPath, podPath)
	if err != nil {
		diskPublishingMap[podPath] = "error"
		log.Errorf("NodePublishVolume:: step 4, bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
		return nil, fmt.Errorf("NodePublishVolume:: step 4, bindMountGlobalPathToPodPath failed, err is: %s", err.Error())
	}

	diskPublishingMap[podPath] = stagingTargetPath
	log.Infof("NodePublishVolume:: Successfully!")

	return &csi.NodePublishVolumeResponse{}, nil
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
	if value, ok := diskUnpublishingMap[targetPath]; ok {
		if value == "unPublishing" {
			log.Warnf("NodeUnpublishVolume: targetPath: %s is in unPublishing, please wait", targetPath)
			return nil, fmt.Errorf("NodePublishVolume: targetPath: %s is in unPublishing, please wait", targetPath)
		} else if value == "error" {
			log.Errorf("NodeUnpublishVolume: targetPath: %s unPublishing process error", targetPath)
			return nil, fmt.Errorf("NodeUnpublishVolume: targetPath: %s unPublishing process error", targetPath)
		} else if value == "ok" {
			log.Warnf("NodeUnpublishVolume: targetPath: %s unPublishing process succeed, return context", targetPath)
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}

	}

	diskUnpublishingMap[targetPath] = "unPublishing"
	err := unBindMountGlobalPathFromPodPath(targetPath)
	if err != nil {
		diskUnpublishingMap[targetPath] = "error"
		log.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
		return nil, fmt.Errorf("NodeUnpublishVolume:: step 2, unmount req.TargetPath(podPath): %s failed, err is: %s", targetPath, err.Error())
	}

	diskUnpublishingMap[targetPath] = "ok"
	delete(diskPublishingMap, targetPath)
	log.Infof("NodeUnpublishVolume:: Successfully!")

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// mount deviceName to node's global path and format disk by fstype
func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	// Step 1: get necessary params
	diskID := req.VolumeId
	targetGlobalPath := req.GetStagingTargetPath()

	if diskID == "" || targetGlobalPath == "" {
		log.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
		return nil, fmt.Errorf("NodeStageVolume: [diskID/NodeStageVolume] cant not be empty")
	}

	log.Infof("NodeStageVolume: starting to stage volumeID: %s, targetGlobalPath is: %s", diskID, targetGlobalPath)

	// Step 2: check disk formatted or not, disk staged or not
	if value, ok := diskStagingMap[targetGlobalPath]; ok {
		if value == "staging" {
			log.Warnf("NodeStageVolume: diskID: %s is in staging, please wait", diskID)
			return nil, fmt.Errorf("NodeStageVolume: diskID: %s is in staging, please wait", diskID)
		} else if value == "error" {
			log.Errorf("NodeStageVolume: diskID: %s staging process error, please deal with it manual", diskID)
			return nil, fmt.Errorf("NodeStageVolume: diskID: %s staging process error, please deal with it manual", diskID)
		} else if value == targetGlobalPath {
			log.Warnf("NodeStageVolume: diskID: %s has been staged to targetGlobalPath: %s", diskID, targetGlobalPath)
			return &csi.NodeStageVolumeResponse{}, nil
		}

		log.Warnf("NodeStageVolume: diskID: %s has been staged to targetGlobalPath: %s, and still going to staged to another targetGlobalPath: %s", diskID, value, targetGlobalPath)
	}

	// Step 2: format disk
	// Step 2-1: get deviceName
	res, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("NodeStageVolume: find disk uuid failed, err is: %s", err)
		return nil, err
	}
	if res.Data.DiskSlice == nil {
		log.Errorf("NodeStageVolume: findDiskByVolumeID res is nil")
		return nil, fmt.Errorf("NodeStageVolume: findDiskByVolumeID res is nil")
	}

	if res.Data.DiskSlice[0].Uuid == "" {
		log.Errorf("NodeStageVolume: findDeviceNameByVolumeID res uuid is null")
		return nil, fmt.Errorf("NodeStageVolume: findDeviceNameByVolumeID res uuid is null")
	}

	deviceName, err := findDeviceNameByUuid(res.Data.DiskSlice[0].Uuid)
	if err != nil {
		log.Errorf("NodeStageVolume: findDeviceNameByUuid error, err is: %s", err.Error())
		return nil, err
	}

	log.Infof("NodeStageVolume: findDeviceNameByVolumeID succeed, deviceName is: %s", deviceName)

	// Step 2-2: format disk
	diskVol := req.GetVolumeContext()
	fsType := diskVol["fsType"]

	diskStagingMap[targetGlobalPath] = "staging"
	if _, ok := diskFormattedMap[diskID]; ok {
		log.Warnf("NodeStageVolume: diskID: %s had been formatted, ignore multi format", diskID)
	} else {
		if err = formatDiskDevice(diskID, deviceName, fsType); err != nil {
			diskStagingMap[targetGlobalPath] = "error"
			log.Errorf("NodeStageVolume: format deviceName: %s failed, err is: %s", deviceName, err.Error())
			return nil, err
		}
		log.Infof("NodeStageVolume: Step 1: formatDiskDevice successfully!")
	}

	// Step 3: mount disk to node's global path
	// Step 3-1: check targetGlobalPath
	log.Infof("NodeStageVolume: targetGlobalPath exist flag: %t", utils.FileExisted(targetGlobalPath))
	if !utils.FileExisted(targetGlobalPath) {
		if err = utils.CreateDir(targetGlobalPath, mountPointMode); err != nil {
			diskStagingMap[targetGlobalPath] = "error"
			log.Errorf("NodeStageVolume: Step 1, targetGlobalPath is not exist, but unable to create it, err is: %s", err.Error())
			return nil, fmt.Errorf("NodeStageVolume: Step 1, targetGlobalPath is not exist, but unable to create it, err is: %s", err.Error())
		}
		log.Infof("NodeStageVolume: Step 1, targetGlobalPath: %s is not exist, and create succeed", targetGlobalPath)
	}

	// Step 3-2: mount deviceName to node's global path
	err = mountDiskDeviceToNodeGlobalPath(strings.TrimSuffix(deviceName, "\n"), strings.TrimSuffix(targetGlobalPath, "\n"), fsType)

	if err != nil {
		diskStagingMap[targetGlobalPath] = "error"
		log.Errorf("NodeStageVolume: Step 2, mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
		return nil, fmt.Errorf("NodeStageVolume: Step 2, mountDeviceToNodeGlobalPath failed, err is: %s", err.Error())
	}

	diskStagingMap[targetGlobalPath] = targetGlobalPath
	log.Infof("NodeStageVolume: Step 2, mountDiskDeviceToNodeGlobalPath: %s successfully!", targetGlobalPath)

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

	// Step 2: umount disk device from node global path
	if value, ok := diskUnstagingMap[unStagingPath]; ok {
		if value == "unstaing" {
			log.Warnf("NodeUnstageVolume: volumeID: %s in is unstaging process, please wait", volumeID)
			return nil, fmt.Errorf("NodeUnstageVolume: volumeID: %s in is unstaging process, please wait", volumeID)
		} else if value == "error" {
			log.Errorf("NodeUnstageVolume: volumeID: %s unstaging process error", volumeID)
			return nil, fmt.Errorf("NodeUnstageVolume: volumeID: %s unstaging process error", volumeID)
		} else if value == "ok" {
			log.Warnf("NodeUnstageVolume: volumeID: %s unstaging process succeed, return context", volumeID)
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
	}

	diskUnstagingMap[unStagingPath] = "unstaging"
	err := unMountDiskDeviceFromNodeGlobalPath(volumeID, unStagingPath)
	if err != nil {
		diskUnstagingMap[unStagingPath] = "error"
		log.Errorf("NodeUnstageVolume: step 2, unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", err)
		return nil, fmt.Errorf("NodeUnstageVolume: step 2, unMountDiskDeviceFromNodeGlobalPath failed, err is: %s", err)
	}

	delete(diskUnstagingMap, unStagingPath)
	delete(diskStagingMap, unStagingPath)
	log.Infof("NodeUnstageVolume: Successfully!")

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
		log.Errorf("findDeviceNameByUuid: cmdScan: %s failed, err is: %s", cmdScan, err)
		return "", err
	}

	// log.Infof("findDeviceNameByUuid: deviceNameStr : %s", deviceNameStr)
	// get device such as /dev/sda
	deviceNameStr = strings.ReplaceAll(deviceNameStr, "\n", " ")
	log.Infof("findDeviceNameByUuid: deviceNameStr : %s", deviceNameStr)
	deviceNameUuid := map[string]string{}
	for _, deviceName := range strings.Split(deviceNameStr, " ") {
		if deviceName == "" {
			continue
		}
		cmdGetUid := fmt.Sprintf("/lib/udev/scsi_id -g -u %s", deviceName)
		// log.Infof("findDeviceNameByUuid: cmdGetUid is: %s", cmdGetUid)
		uuidStr, err := utils.RunCommand(cmdGetUid)
		if err != nil {
			log.Errorf("findDeviceNameByUuid: get deviceName: %s uuid failed, err is: %s", deviceName, err)
			return "", err
		}

		deviceNameUuid[strings.TrimSpace(strings.TrimPrefix(uuidStr, "3"))] = deviceName
	}

	log.Infof("findDeviceNameByUuid: deviceNameUuid is: %+v", deviceNameUuid)

	// compare
	if _, ok := deviceNameUuid[diskUuidFormat]; !ok {
		log.Errorf("findDeviceNameByUuid: diskUuid: %s is not exist", diskUuidFormat)
		return "", fmt.Errorf("findDeviceNameByUuid: diskUuid: %s is not exist", diskUuidFormat)
	}

	deviceName := deviceNameUuid[diskUuidFormat]
	if deviceName == "" {
		log.Errorf("findDeviceNameByUuid: diskUuid: %s, deviceName is empty", diskUuidFormat)
		return "", fmt.Errorf("findDeviceNameByUuid: diskUuid: %s, deviceName is empty", diskUuidFormat)
	}

	log.Infof("findDeviceNameByUuid: successfully, diskUuid: %s, deviceName is: %s", diskUuidFormat, deviceNameUuid[diskUuidFormat])

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

func mountDiskDeviceToNodeGlobalPath(deviceName, targetGlobalPath, fstype string) error {
	log.Infof("mountDiskDeviceToNodeGlobalPath: deviceName is: %s, targetGlobalPath is: %s, fstype is: %s", deviceName, targetGlobalPath, fstype)

	cmd := fmt.Sprintf("mount -t %s %s %s", fstype, deviceName, targetGlobalPath)
	if _, err := utils.RunCommand(cmd); err != nil {
		log.Errorf("mountDiskDeviceToNodeGlobalPath: err is: %s", err)
		return err
	}

	log.Infof("mountDiskDeviceToNodeGlobalPath: Successfully!")

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

func formatDiskDevice(diskId, deviceName, fsType string) error {
	log.Infof("formatDiskDevice: deviceName is: %s, fsType is: %s", deviceName, fsType)

	// Step 1: check deviceName(disk) is scannable
	scanDeviceCmd := fmt.Sprintf("fdisk -l | grep %s | grep -v grep", deviceName)
	if _, err := utils.RunCommand(scanDeviceCmd); err != nil {
		log.Error("formatDiskDevice: scanDeviceCmd: %s failed, err is: %s", scanDeviceCmd, err.Error())
		return err
	}

	// Step 2: format deviceName(disk)
	var formatDeviceCmd string
	if fsType == DefaultFsTypeExt4 {
		formatDeviceCmd = fmt.Sprintf("mkfs.ext4 %s", deviceName)
	} else if fsType == FsTypeExt3 {
		formatDeviceCmd = fmt.Sprintf("mkfs.ext3 %s", deviceName)
	} else if fsType == FsTypeXfs {
		formatDeviceCmd = fmt.Sprintf("mkfs.xfs %s", deviceName)
	} else {
		log.Error("formatDiskDevice: fsType not support, should be [ext4/ext3/ext2/xfs]")
		return fmt.Errorf("formatDiskDevice: fsType not support, should be [ext4/ext3/ext2/xfs]")
	}

	if _, err := utils.RunCommand(formatDeviceCmd); err != nil {
		log.Error("formatDiskDevice: formatDeviceCmd: %s failed, err is: %s", formatDeviceCmd, err.Error())
		return err
	}

	diskFormattedMap[diskId] = "formatted"
	// need to inform database to update disk format status

	log.Infof("formatDiskDevice: Successfully!")

	return nil
}