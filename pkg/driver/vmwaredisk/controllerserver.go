package vmwaredisk

import (
	"context"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	v12 "k8s.io/api/core/v1"
	"strconv"
	"time"

	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/vmwaredisk"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	diskProcessingState = "processing"
	diskOKState         = "ok"
	diskErrorState      = "error"
	diskDeletedState    = "deleted"
)

func NewControllerServer(d *DiskDriver) *ControllerServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("NewControllerServer: Failed to create kubernetes config: %v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("NewControllerServer: Failed to create kubernetes client: %v", err)
	}

	return &ControllerServer{
		KubeClient:              client,
		DefaultControllerServer: csicommon.NewDefaultControllerServer(d.csiDriver),
		VolumeLocks:             utils.NewVolumeLocks(),
	}
}

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Infof("CreateVolume: Starting CreateVolume, req is:%+v", req)

	diskVolume, err := parseDiskVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, err
	}

	if acquired := c.VolumeLocks.TryAcquire(req.GetName()); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, req.GetName())
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, req.GetName())
	}
	defer c.VolumeLocks.Release(req.GetName())

	createRes, err := createDisk(req, diskVolume)
	if err != nil {
		log.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
		return nil, err
	}

	diskID := createRes.Data.VolumeID
	diskVolume.DiskID = diskID

	if err = checkCreateDiskState(diskID); err != nil {
		log.Errorf("createDisk: getDiskInfo result failed, err is: %s", err.Error())
		return nil, err
	}

	return buildCreateVolumeResponse(req, diskVolume), nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume: Starting deleting volume, req is: %+v", req)

	diskID := req.GetVolumeId()
	if diskID == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}

	if acquired := c.VolumeLocks.TryAcquire(diskID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskID)
	}
	defer c.VolumeLocks.Release(diskID)

	disk, err := getDiskInfo(diskID)
	if err != nil {
		log.Errorf("DeleteVolume[%s]: findDiskByVolumeID error, err is: %s", diskID, err.Error())
		return nil, err
	}

	if disk.Data.IsValid && disk.Data.Mounted {
		log.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s ", diskID)
		return nil, fmt.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s", diskID)
	} else if disk.Data.IsValid && !disk.Data.Mounted {
		log.Debugf("DeleteVolume[%s]: disk is in [idle], then to delete directly!", diskID)
	} else if !disk.Data.IsValid {
		log.Infof("DeleteVolume[%s]: disk had been deleted", diskID)
		return &csi.DeleteVolumeResponse{}, nil
	}

	if _, err := deleteDisk(diskID); err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, err
	}

	if err := checkDeleteDiskState(diskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		return nil, err
	}

	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	diskID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	log.Infof("ControllerPublishVolume: pvName: %s, starting attach diskID: %s to node: %s", req.VolumeId, diskID, nodeID)

	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
	}

	if acquired := c.VolumeLocks.TryAcquire(diskID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskID)
	}
	defer c.VolumeLocks.Release(diskID)

	diskInfo, err := getDiskInfo(diskID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
		return nil, err
	}

	if diskInfo.Data.IsValid && diskInfo.Data.Mounted {
		diskMountedNodeID := diskInfo.Data.NodeID

		if diskMountedNodeID == nodeID {
			log.Warnf("ControllerPublishVolume: diskID: %s had been attached to nodeID: %s", diskID, nodeID)
			return &csi.ControllerPublishVolumeResponse{}, nil
		}

		log.Warnf("ControllerPublishVolume: diskID: %s had been attached to nodeID: %s is different from current nodeID: %s, check node status", diskID, diskMountedNodeID, nodeID)

		// check node status
		nodeStatus, err := describeNodeStatus(ctx, c, diskMountedNodeID)
		if err != nil {
			log.Warnf("ControllerPublishVolume: check nodeStatus error, err is: %s", err)
			return nil, err
		}

		if nodeStatus == "True" {
			log.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
			return nil, nil
		}

		// node is not exist or NotReady status, detach force
		log.Warnf("ControllerPublishVolume: diskMountedNodeID: %s is in [NotRead|Not Exist], detach forcely", diskMountedNodeID)
		if _, err := detachDisk(diskID, nodeID); err != nil {
			log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
			return nil, err
		}

		if err := checkDeleteDiskState(diskID); err != nil {
			log.Errorf("ControllerPublishVolume: failed to delete disk %s, err is: %s", diskID, err)
			return nil, err
		}

		log.Warnf("ControllerPublishVolume: detach diskID: %s from nodeID: %s successfully", diskID, diskMountedNodeID)
	} else if !diskInfo.Data.IsValid {
		log.Errorf("ControllerPublishVolume: diskID: %s was in [deleted|error], cant attach to nodeID", diskID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s was in [deleted|error], cant attach to nodeID", diskID)
	}

	if _, err = attachDisk(diskID, nodeID); err != nil {
		log.Errorf("ControllerPublishVolume: create attach task by %s failed, err is:%s", diskID, err.Error())
		return nil, err
	}

	if err = checkAttachDiskState(diskID); err != nil {
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
		return nil, err
	}

	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	diskID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	log.Infof("ControllerUnpublishVolume: starting detach disk: %s from node: %s", diskID, nodeID)

	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
	}

	if acquired := c.VolumeLocks.TryAcquire(diskID); !acquired {
		log.Errorf(utils.VolumeOperationAlreadyExistsFmt, diskID)
		return nil, status.Errorf(codes.Aborted, utils.VolumeOperationAlreadyExistsFmt, diskID)
	}
	defer c.VolumeLocks.Release(diskID)

	res, err := getDiskInfo(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
		return nil, err
	}

	if res.Data.NodeID != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if _, err = detachDisk(diskID, nodeID); err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	if err = checkDeleteDiskState(diskID); err != nil {
		log.Errorf("ControllerUnpublishVolume: failed to delete %s, err is: %s", diskID, err)
		return nil, err
	}

	log.Infof("ControllerUnpublishVolume: Successfully detach disk: %s from node: %s", diskID, nodeID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (c *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	log.Infof("ValidateVolumeCapabilities: req is: %+v", req)

	for _, capability := range req.VolumeCapabilities {
		if capability.GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: ""}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.VolumeCapabilities,
		},
	}, nil
}

func (c *ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func describeNodeStatus(ctx context.Context, c *ControllerServer, nodeId string) (v12.ConditionStatus, error) {
	var nodeStatus v12.ConditionStatus = ""

	res, err := c.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		log.Errorf("describeNodeStatus: get node list failed, err is: %s", err)
		return "", err
	}

OuterLoop:
	for _, node := range res.Items {
		if node.Spec.ProviderID == nodeId {
			for _, value := range node.Status.Conditions {
				if value.Type == "Ready" {
					nodeStatus = value.Status
					break OuterLoop
				}
			}
		}
	}

	log.Infof("describeNodeStatus: nodeStatus is: %s", nodeStatus)
	return nodeStatus, nil
}

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
	log.Infof("deleteDisk: diskID is:%s", diskID)

	res, err := cdsDisk.DeleteDisk(&cdsDisk.DeleteDiskArgs{
		VolumeID: diskID,
	})

	if err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk api error, err is: %s", err)
		return nil, err
	}

	log.Infof("deleteDisk: cdsDisk.DeleteDisk task creation succeed, taskID is: %s", res.TaskID)

	return res, nil
}

func attachDisk(diskID, nodeID string) (string, error) {
	log.Infof("attachDisk: diskID: %s, nodeID: %s", diskID, nodeID)

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
	log.Infof("detachDisk: diskID=%s, nodeID=%s", diskID, nodeID)

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
	log.Infof("getDiskInfo: diskId is: %s", diskId)

	diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
	if err != nil {
		return nil, fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
	}
	log.Infof("[%s] disk info: %+v", diskId, diskInfo)

	return diskInfo, nil
}

func checkCreateDiskState(diskId string) error {
	log.Infof("checkCreateDiskState: diskId is: %s", diskId)

	for i := 1; i < 120; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		switch diskInfo.Data.Status {
		case diskOKState:
			return nil
		case diskProcessingState:
			time.Sleep(10 * time.Second)
		case diskErrorState:
			return fmt.Errorf("taskError")
		}
	}

	return fmt.Errorf("task time out, running more than 20 minutes")
}

func checkDeleteDiskState(diskId string) error {
	log.Infof("checkDeleteDiskState: diskId is: %s", diskId)

	for i := 1; i < 120; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		if !diskInfo.Data.IsValid && diskInfo.Data.Status == diskDeletedState {
			return nil
		}

		log.Debugf("disk:%s is deleting, sleep 10s", diskId)
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("task time out, running more than 20 minutes")
}

func checkAttachDiskState(diskId string) error {
	log.Infof("checkDeleteDiskState: diskId is: %s", diskId)

	for i := 1; i < 120; i++ {
		diskInfo, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		if diskInfo.Data.IsValid && diskInfo.Data.Mounted {
			return nil
		}

		log.Debugf("disk:%s is attaching, sleep 10s", diskId)
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("task time out, running more than 20 minutes")
}

func buildCreateVolumeResponse(req *csi.CreateVolumeRequest, diskVolume *DiskVolumeArgs) *csi.CreateVolumeResponse {
	volumeContext := map[string]string{}

	volumeContext["fsType"] = diskVolume.FsType
	volumeContext["storageType"] = diskVolume.StorageType
	volumeContext["zoneId"] = diskVolume.ZoneID
	volumeContext["siteID"] = diskVolume.SiteID
	volumeContext["iops"] = diskVolume.Iops

	tmpVol := &csi.Volume{
		VolumeId:      diskVolume.DiskID,
		CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
		VolumeContext: volumeContext,
		AccessibleTopology: []*csi.Topology{
			{
				Segments: map[string]string{
					TopologyZoneKey: diskVolume.ZoneID,
				},
			},
		},
	}

	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskInfo: %+v", req.GetName(), diskVolume)

	return &csi.CreateVolumeResponse{Volume: tmpVol}

}
