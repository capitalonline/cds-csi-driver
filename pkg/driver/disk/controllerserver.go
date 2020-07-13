package disk

import (
	"context"
	"fmt"
	"strings"
	"time"

	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/disk"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"
)

// the map of req.Name and csi.Volume
var createdVolumeMap = map[string]*csi.Volume{}

// the map of diskId and pvName
// diskId and pvName is not same under csi plugin
var diskIDPVMap = map[string]string{}

func NewControllerServer(d *DiskDriver) *ControllerServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes client: %v", err)
	}

	return &ControllerServer{
		Client:                  clientset,
		DefaultControllerServer: csicommon.NewDefaultControllerServer(d.csiDriver),
	}
}

// to create disk
func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Infof("CreateVolume: Starting CreateVolume, req.name is:%s, req is:%v", req.Name, req)

	// Step 1: check critical params
	if req.Name == "" {
		log.Errorf("CreateVolume: Volume Name cannot be empty")
		return nil, status.Error(codes.InvalidArgument, "Volume Name cannot be empty")
	}
	if req.VolumeCapabilities == nil {
		log.Errorf("CreateVolume: Volume Capabilities cannot be empty")
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities cannot be empty")
	}
	if req.GetCapacityRange() == nil {
		log.Errorf("CreateVolume: Capacity cannot be empty")
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: req.name is: %s, Capacity cannot be empty", req.Name)
	}
	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	diskRequestGB := req.CapacityRange.RequiredBytes / (1024 * 1024 * 1024)
	if diskRequestGB < 100 {
		log.Error("CreateVolume: pvc require disk size should > 100 GB, input is: %d", diskRequestGB)
		return nil, fmt.Errorf("CreateVolume: pvc require disk size should > 100 GB, input is: %d", diskRequestGB)
	}

	// Step 2: check if the pvName created in this volume
	if value, ok := createdVolumeMap[req.Name]; ok {
		log.Infof("CreateVolume: volume already be created pvName: %s, VolumeId: %s, volumeContext: %v", req.Name, value.VolumeId, value.VolumeContext)
		return &csi.CreateVolumeResponse{Volume: value}, nil
	}

	// Step 3: parse DiskVolume params
	diskVol, err := parseDiskVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input: %v, with error: %v", req.Name, err)
		return nil, status.Errorf(codes.InvalidArgument, "Invalid parameters from input: %v, with error: %v", req.Name, err)
	}

	// Step 4: create disk
	diskVolumeID, err := createDisk(diskVol.FsType, diskVol.Type, diskVol.RegionID, diskVol.ClusterID, int(diskRequestGB), diskVol.ReadOnly)
	if err != nil {
		log.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
		return nil, fmt.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
	}

	// Step 5: generate return volume
	tmpVol := &csi.Volume{
		VolumeId:      diskVolumeID,
		CapacityBytes: int64(volSizeBytes),
		VolumeContext: req.GetParameters(),
		AccessibleTopology: []*csi.Topology{
			{
				Segments: map[string]string{
					TopologyRegionKey: diskVol.RegionID,
				},
			},
		},
	}

	// Step 6: store
	// store diskId and pvName
	diskIDPVMap[diskVolumeID] = req.Name
	// store req.Name and csi.Volume
	createdVolumeMap[req.Name] = tmpVol
	log.Infof("CreateVolume: store [createdVolumeMap] and [diskIDPVMap] succeed")

	log.Infof("CreateVolume: createDisk successfully, diskVolumeID is: %s", diskVolumeID)
	return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
}

// to delete disk
func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume: Starting deleting volume, req.VolumeID is: %s, req is: %v", req.VolumeId, req)

	// Step 1: check params
	if req.VolumeId == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}

	// Step 2: find disk by volumeID
	disk, err := findDiskByVolumeID(req.VolumeId)
	if err != nil {
		log.Errorf("DeleteVolume: findDiskByVolumeID error, err is: %s", err.Error())
	}

	nodeID := disk.Data.InstanceID
	log.Infof("DeleteVolume: findDiskByVolumeID succeed, instanceID is: %s", nodeID)

	// Step 3: check if disk detached from node, if not detach it firstly
	if disk != nil {
		if disk.Data.Status == DiskStatusInUse {
			log.Errorf("DeleteVolume: disk [in use], cant delete volumeID: %s from instanceID: %s", req.VolumeId, nodeID)
			return nil, fmt.Errorf("DeleteVolume: disk [in use], cant delete volumeID: %s from instanceID: %s", req.VolumeId, nodeID)
		} else if disk.Data.Status == DiskStatusInIdle {
			log.Infof("DeleteVolume: disk is in [idle], then to delete directly!")
		} else {
			log.Warnf("DeleteVolume: disk status is not [in_use] or [idle], need to detach and then to delete it")
			err := detachDisk(req.VolumeId, nodeID)
			if err != nil {
				log.Errorf("DeleteVolume: detach disk: %s from node: %s with error, err is: %s", req.VolumeId, nodeID, err.Error())
				return nil, err
			}
			log.Warnf("DeleteVolume: detach disk: %s from node: %s successfully! Then to delete it!", req.VolumeId, nodeID)
		}
	}

	// Step 4: delete disk
	if err := deleteDisk(req.VolumeId); err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
	}

	log.Infof("DeleteVolume: delete disk successfully!")

	// Step 5: delete diskIDPVMap and createdVolumeMap
	if value, ok := diskIDPVMap[req.VolumeId]; ok {
		delete(createdVolumeMap, value)
		delete(diskIDPVMap, req.VolumeId)
	}

	log.Infof("DeleteVolume: delete [createdVolumeMap] and [diskIDPVMap] succeed!")

	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	log.Infof("ControllerPublishVolume: starting attach disk: %s to node: %s", req.VolumeId, req.NodeId)

	// default is false
	isSharedDisk := DiskShareDefault
	if value, ok := req.VolumeContext[SharedEnable]; ok {
		value = strings.ToLower(value)
		if value == "true" {
			isSharedDisk = true
		}
	}

	if req.VolumeId == "" || req.NodeId == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
	}

	err := attachDisk(req.VolumeId, req.NodeId, isSharedDisk)
	if err != nil {
		log.Errorf("ControllerPublishVolume: attach disk:%s to node: %s with error, err is: %s", req.VolumeId, req.NodeId, err.Error())
		return nil, err
	}

	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", req.VolumeId, req.NodeId)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {

	log.Infof("ControllerUnpublishVolume: starting detach disk: %s from node: %s", req.VolumeId, req.NodeId)

	if req.VolumeId == "" || req.NodeId == "" {
		log.Errorf("ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
	}

	err := detachDisk(req.VolumeId, req.NodeId)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: detach disk: %s from node: %s with error, err is: %s", req.VolumeId, req.NodeId, err.Error())
		return nil, err
	}

	log.Infof("ControllerUnpublishVolume: Successfully detach disk: %s from node: %s", req.VolumeId, req.NodeId)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (c ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, nil
}

func createDisk(diskFsType, diskType, diskClusterID, diskRegionID string, diskRequestGB int, diskReadOnly bool) (string, error) {

	log.Infof("createDisk: diskFstype: %s, diskType: %s, diskClusterID: %s, diskRegionID: %s, diskRequestGB: %d, diskReadOnly: %t", diskFsType, diskType, diskClusterID, diskRegionID, diskRequestGB, diskReadOnly)

	// create disk
	res, err := cdsDisk.CreateDisk(&cdsDisk.CreateDiskArgs{
		ClusterID: diskClusterID,
		RegionID:  diskRegionID,
		Fstype:    diskFsType,
		Type:      diskType,
		ReadOnly:  diskReadOnly,
	})

	if err != nil {
		log.Errorf("createDisk: cdsDisk.CreateDisk api error, err is: %s", err.Error())
		return "", err
	}

	// check task status
	taskID := res.TaskID
	log.Infof("createDisk: cdsDisk.CreateDisk task creation succeed, taskID is: %s", res.TaskID)

	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createDisk: cdsDisk.CreateDisk task result failed, err is: %s", err)
		return "", err
	}

	log.Infof("createDisk: successfully!")

	return res.Data.VolumeID, nil
}

func deleteDisk(diskID string) error {
	log.Infof("deleteDisk: diskID is:%s", diskID)

	res, err := cdsDisk.DeleteDisk(&cdsDisk.DeleteDiskArgs{
		DiskID: diskID,
	})

	if err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk api error, err is: %s", err)
		return err
	}

	log.Infof("deleteDisk: cdsDisk.DeleteDisk task creation succeed, taskID is: %s", res.TaskID)

	if err := describeTaskStatus(res.TaskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		return err
	}

	log.Infof("deleteDisk: Successfully!")

	return nil
}

func attachDisk(volumeID, nodeID string, isShareDisk bool) error {
	// attach disk to node
	log.Infof("attachDisk: volumeID: %s, nodeID: %s, isShareDisk: %t", volumeID, nodeID, isShareDisk)

	res, err := cdsDisk.AttachDisk(&cdsDisk.AttachDiskArgs{
		VolumeID: volumeID,
		NodeID:   nodeID,
	})

	if err != nil {
		log.Errorf("attachDisk: cdsDisk.attachDisk api error, err is: %s", err)
		return err
	}

	log.Infof("attachDisk: cdsDisk.attachDisk task creation succeed, taskID is: %s", res.TaskID)

	if err := describeTaskStatus(res.TaskID); err != nil {
		log.Errorf("attachDisk: cdsDisk.attachDisk task result failed, err is: %s", err)
		return err
	}

	log.Infof("attachDisk: Successfully!")

	return nil
}

func detachDisk(volumeID, nodeID string) error {
	// to detach disk from node
	log.Infof("detachDisk: volumeID: %s, nodeID: %s", volumeID, nodeID)

	res, err := cdsDisk.DetachDisk(&cdsDisk.DetachDiskArgs{
		VolumeID: volumeID,
		NodeID:   nodeID,
	})

	if err != nil {
		log.Errorf("detachDisk: cdsDisk.detachDisk api error, err is: %s", err)
		return err
	}

	log.Infof("detachDisk: cdsDisk.detachDisk task creation succeed, taskID is: %s", res.TaskID)

	if err := describeTaskStatus(res.TaskID); err != nil {
		log.Errorf("detachDisk: cdsDisk.detachDisk task result failed, err is: %s", err)
		return err
	}

	log.Infof("detachDisk: Successfully!")

	return nil
}

func findDiskByVolumeID(volumeID string) (*cdsDisk.FindDiskByVolumeIDResponse, error) {

	log.Infof("findDiskByVolumeID: volumeID is: %s", volumeID)

	res, err := cdsDisk.FindDiskByVolumeID(&cdsDisk.FindDiskByVolumeIDArgs{
		VolumeID: volumeID,
	})

	if err != nil || res != nil {
		log.Errorf("findDiskByVolumeID: cdsDisk.FindDiskByVolumeID [api error] or [res is nil], err is: %s", err)
		return nil, err
	}

	log.Infof("findDiskByVolumeID: Successfully!")

	return res, nil
}

func describeTaskStatus(taskID string) error {

	log.Infof("describeTaskStatus: taskID is: %s", taskID)

	for i := 1; i < 120; i++ {

		res, err := cdsDisk.DescribeTaskStatus(taskID)

		if err != nil {
			log.Errorf("task api error, err is: %s", err)
			return fmt.Errorf("apiError")
		}

		if res.Data.Status == "finish" {
			log.Infof("task succeed")
			return nil
		} else if res.Data.Status == "doing" {
			log.Infof("task:%s is running, sleep 10s", taskID)
			time.Sleep(10 * time.Second)
		} else if res.Data.Status == "error" {
			log.Infof("task error")
			return fmt.Errorf("taskError")
		}
	}

	return fmt.Errorf("task time out, running more than 20 minutes")
}
