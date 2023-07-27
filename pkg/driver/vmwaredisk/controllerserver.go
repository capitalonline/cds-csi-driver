package vmwaredisk

import (
	"context"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/common"
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

// the map of req.Name and csi.Volume.
var pvcCreatedMap = map[string]*csi.Volume{}

// the map of diskId and pvName
// diskId and pvName is not same under csi plugin
var diskIdPvMap = map[string]string{}

// the map od diskID and pvName
// storing the disk in creating status
var diskProcessingMap = map[string]string{}

// storing deleting disk
var diskDeletingMap = map[string]string{}

// storing attaching disk
var diskAttachingMap = map[string]string{}

// storing detaching disk
var diskDetachingMap = map[string]string{}

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

	if _, err = getDiskInfo(diskID); err != nil {
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

	diskStatus := disk.Data.Status
	if diskStatus == StatusInMounted {
		log.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s ", diskID)
		return nil, fmt.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s", diskID)
	} else if diskStatus == StatusInOK {
		log.Debugf("DeleteVolume: disk is in [idle], then to delete directly!")
	} else if diskStatus == StatusInDeleted {
		log.Warnf("DeleteVolume: disk had been deleted")
		return &csi.DeleteVolumeResponse{}, nil
	}

	// Step 4: delete disk
	deleteRes, err := deleteDisk(diskID)
	if err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, fmt.Errorf("DeleteVolume: delete disk error, err is: %s", err)
	}

	// store deleting disk status
	diskDeletingMap[diskID] = "deleting"

	taskID := deleteRes.TaskID
	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		diskDeletingMap[diskID] = "error"
		return nil, fmt.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
	}

	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// Step 1: get necessary params
	diskID := req.VolumeId
	nodeID := req.NodeId

	log.Infof("ControllerPublishVolume: pvName: %s, starting attach diskID: %s to node: %s", req.VolumeId, diskID, nodeID)

	// Step 2: check necessary params
	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
	}

	// check attaching status
	if value, ok := diskAttachingMap[diskID]; ok {
		if value == "attaching" {
			log.Warnf("ControllerPublishVolume: diskID: %s is in attaching, please wait", diskID)
			return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s is in attaching, please wait", diskID)
		} else if value == "error" {
			log.Errorf("ControllerPublishVolume: diskID: %s attaching process was error", diskID)
			return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s attaching process was error", diskID)
		}
		log.Warnf("ControllerPublishVolume: diskID: %s attaching process finished, return context", diskID)
		return &csi.ControllerPublishVolumeResponse{}, nil
	}

	// Step 3: check disk status
	res, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
		return nil, fmt.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
	}

	diskStatus := res.Data.Status
	diskMountedNodeID := res.Data.NodeID
	if diskStatus == StatusInMounted {
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

		if nodeStatus != "True" {
			// node is not exist or NotReady status, detach force
			log.Warnf("ControllerPublishVolume: diskMountedNodeID: %s is in [NotRead|Not Exist], detach forcely", diskMountedNodeID)
			taskID, err := detachDisk(diskID)
			if err != nil {
				log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
				return nil, fmt.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
			}

			diskAttachingMap[diskID] = "attaching"
			if err := describeTaskStatus(taskID); err != nil {
				diskAttachingMap[diskID] = "error"
				log.Errorf("ControllerPublishVolume: cdsDisk.detachDisk task result failed, err is: %s", err)
				return nil, err
			}

			log.Warnf("ControllerPublishVolume: detach diskID: %s from nodeID: %s successfully", diskID, diskMountedNodeID)
		} else {
			log.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
			return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
		}
	} else if diskStatus == StatusInDeleted || diskStatus == StatusInError {
		log.Errorf("ControllerPublishVolume: diskID: %s was in [deleted|error], cant attach to nodeID", diskID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s was in [deleted|error], cant attach to nodeID", diskID)
	}

	// Step 4: attach disk to node
	taskID, err := attachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
		return nil, err
	}

	diskAttachingMap[diskID] = "attaching"
	if err = describeTaskStatus(taskID); err != nil {
		diskAttachingMap[diskID] = "error"
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
		return nil, err
	}

	delete(diskAttachingMap, diskID)
	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	// Step 1: get necessary params
	diskID := req.VolumeId
	nodeID := req.NodeId
	log.Infof("ControllerUnpublishVolume: starting detach disk: %s from node: %s", diskID, nodeID)

	// Step 2: check necessary params
	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
	}

	// Step 3: check disk status
	if value, ok := diskDetachingMap[diskID]; ok {
		if value == "detaching" {
			log.Warnf("ControllerUnpublishVolume: diskID: %s is in detaching, please wait", diskID)
			return nil, fmt.Errorf("ControllerUnpublishVolume: diskID: %s is in detaching, please wait", diskID)
		} else if value == "error" {
			log.Errorf("ControllerUnpublishVolume: diskID: %s detaching process is error", diskID)
			return nil, fmt.Errorf("ControllerUnpublishVolume: diskID: %s detaching process is error", diskID)
		}
	}

	res, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
	}

	if res == nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID res is nil")
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID res is nil")
	}

	if res.Data.NodeID != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Step 4: detach disk
	taskID, err := detachDisk(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	diskDetachingMap[diskID] = "detaching"
	if err := describeTaskStatus(taskID); err != nil {
		diskDetachingMap[diskID] = "error"
		log.Errorf("ControllerUnpublishVolume: cdsDisk.detachDisk task result failed, err is: %s", err)
		return nil, err
	}

	delete(diskDetachingMap, diskID)
	//delete(diskAttachingMap, diskID)
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

	res, err := c.Client.CoreV1().Nodes().List(metav1.ListOptions{})
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

func detachDisk(diskID string) (string, error) {
	log.Infof("detachDisk: diskID: %s", diskID)

	res, err := cdsDisk.DetachDisk(&cdsDisk.DetachDiskArgs{
		VolumeID: diskID,
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

	for i := 1; i < 120; i++ {
		res, err := cdsDisk.GetDiskInfo(&cdsDisk.DiskInfoArgs{VolumeID: diskId})
		if err != nil {
			return nil, fmt.Errorf("[%s] task api error, err is: %s", diskId, err)
		}

		switch res.Data.Status {
		case common.FinishState:
			log.Debugf("task succeed")
			return res, nil
		case common.DoingState:
			log.Debugf("disk:%s is running, sleep 10s", diskId)
			time.Sleep(10 * time.Second)
		case common.ErrorState:
			log.Debugf("task error")
			return nil, fmt.Errorf("taskError")
		}
	}

	return nil, fmt.Errorf("task time out, running more than 20 minutes")
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
