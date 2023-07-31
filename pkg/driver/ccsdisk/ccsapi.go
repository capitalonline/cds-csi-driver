package ccsdisk

import (
	"fmt"
	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/ccsdisk"
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"strconv"
	"time"
)

func createDisk(req *csi.CreateVolumeRequest, diskVolume *DiskVolumeArgs) (*cdsDisk.CreateDiskResponse, error) {
	log.Infof("createDisk[%s]: diskInfo=%+v", req.GetName(), diskVolume)

	diskIops, _ := strconv.Atoi(diskVolume.Iops)
	diskSize := int(req.CapacityRange.RequiredBytes / (1024 * 1024 * 1024))

	// create disk
	res, err := cdsDisk.CreateDisk(&cdsDisk.CreateDiskArgs{
		RegionID:    diskVolume.SiteID,
		DiskType:    diskVolume.StorageType,
		Size:        diskSize,
		Iops:        diskIops,
		ClusterName: diskVolume.ZoneID,
	})

	if err != nil {
		log.Errorf("createDisk: cdsDisk.CreateDisk api error, err is: %s", err.Error())
		return nil, err
	}

	return res, nil
}

func deleteDisk(diskID string) (*cdsDisk.DeleteDiskResponse, error) {
	res, err := cdsDisk.DeleteDisk(&cdsDisk.DeleteDiskArgs{
		VolumeID: diskID,
	})

	if err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk api error, err is: %s", err)
		return nil, err
	}

	log.Infof("deleteDisk: cdsDisk.DeleteDisk task creation succeed, diskID is: %s", diskID)

	return res, nil
}

func attachDisk(diskID, nodeID string) (string, error) {
	res, err := cdsDisk.AttachDisk(&cdsDisk.AttachDiskArgs{
		VolumeID: diskID,
		NodeID:   nodeID,
	})

	if err != nil {
		log.Errorf("attachDisk: cdsDisk.attachDisk api error, err is: %s", err)
		return "", err
	}

	log.Infof("attachDisk: cdsDisk.attachDisk task creation succeed, taskID is: %s", res.TaskID)

	return res.TaskID, nil
}

func detachDisk(diskID, nodeID string) (string, error) {
	res, err := cdsDisk.DetachDisk(&cdsDisk.DetachDiskArgs{
		VolumeID: diskID,
		NodeID:   nodeID,
	})

	if err != nil {
		log.Errorf("detachDisk: cdsDisk.detachDisk api error, err is: %s", err)
		return "", err
	}

	log.Infof("detachDisk: cdsDisk.detachDisk task creation succeed, taskID is: %s", res.TaskID)

	return res.TaskID, nil
}

func getDiskInfo(diskId string) (*cdsDisk.DiskInfoResponse, error) {
	diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
	if err != nil {
		return nil, fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
	}
	log.Infof("[%s] disk info: %+v", diskId, diskInfo)

	if diskInfo.Data.VolumeId == "" {
		return nil, fmt.Errorf("failed to fetch %s: volumeId is null", diskId)
	}

	return diskInfo, nil
}

func getDiskCountByNodeId(nodeId string) (*cdsDisk.DiskCountResponse, error) {
	diskCount, err := cdsDisk.GetDiskCount(&cdsDisk.DiskCountArgs{NodeID: nodeId})
	if err != nil {
		return nil, fmt.Errorf("[%s] task api error, err is: %s", nodeId, err)
	}
	log.Infof("[%s] disk count: %+v", nodeId, diskCount)

	return diskCount, nil
}

func checkCreateDiskState(diskId string) error {
	for i := 1; i < 120; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		if diskInfo.Data.IsValid == 1 && diskInfo.Data.Status == diskOKState {
			return nil
		}

		switch diskInfo.Data.TaskStatus {
		case diskProcessingStateByTask:
			log.Infof("disk:%s is cteating, sleep 3s", diskId)
			time.Sleep(3 * time.Second)
		case diskErrorStateByTask:
			return fmt.Errorf("[%s] task failed, disk info: %+v", diskId, diskInfo)
		default:
			log.Infof("disk:%s is cteating, sleep 3s, disk info: %+v", diskId, diskInfo)
			time.Sleep(3 * time.Second)
		}
	}

	return fmt.Errorf("task time out, running more than 6 minutes")
}

func checkDeleteDiskState(diskId string) error {
	for i := 1; i < 60; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		if diskInfo.Data.IsValid == 0 && diskInfo.Data.Status == diskDeletedState {
			return nil
		}

		switch diskInfo.Data.TaskStatus {
		case diskProcessingStateByTask:
			log.Infof("disk:%s is deleting, sleep 3s", diskId)
			time.Sleep(3 * time.Second)
		case diskErrorStateByTask:
			return fmt.Errorf("[%s] task failed, disk info: %+v", diskId, diskInfo)
		default:
			log.Infof("disk:%s is deleting, sleep 3s, disk info: %+v", diskId, diskInfo)
			time.Sleep(3 * time.Second)
		}
	}

	return fmt.Errorf("task time out, running more than 6 minutes")
}

func checkAttachDiskState(diskId string) error {
	for i := 1; i < 120; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		if diskInfo.Data.IsValid == 1 && diskInfo.Data.Mounted == 1 {
			return nil
		}

		switch diskInfo.Data.TaskStatus {
		case diskProcessingStateByTask:
			log.Infof("disk:%s is attaching, sleep 3s", diskId)
			time.Sleep(3 * time.Second)
		case diskErrorStateByTask:
			return fmt.Errorf("[%s] task failed, disk info: %+v", diskId, diskInfo)
		default:
			log.Infof("disk:%s is attaching, sleep 3s, disk info: %+v", diskId, diskInfo)
			time.Sleep(3 * time.Second)
		}
	}

	return fmt.Errorf("task time out, running more than 6 minutes")
}

func checkDetachDiskState(diskId string) error {
	for i := 1; i < 120; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		if diskInfo.Data.IsValid == 1 && diskInfo.Data.Mounted == 0 {
			return nil
		}

		switch diskInfo.Data.TaskStatus {
		case diskProcessingStateByTask:
			log.Infof("disk:%s is detaching, sleep 3s", diskId)
			time.Sleep(3 * time.Second)
		case diskErrorStateByTask:
			return fmt.Errorf("[%s] task failed, disk info: %+v", diskId, diskInfo)
		default:
			log.Infof("disk:%s is detaching, sleep 3s, disk info: %+v", diskId, diskInfo)
			time.Sleep(3 * time.Second)
		}
	}

	return fmt.Errorf("task time out, running more than 6 minutes")
}
