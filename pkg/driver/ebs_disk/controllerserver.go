package ebs_disk

import (
	"context"
	"encoding/json"
	"fmt"
	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/eks/ebs"
	"github.com/container-storage-interface/spec/lib/go/csi"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v12 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
	"sync"
	"time"
)

// the map of req.Name and csi.Volume.
var pvcCreatedMap = new(sync.Map)

// the map of diskId and pvName
// diskId and pvName is not same under csi plugin
// var diskIdPvMap = map[string]string{}
var diskIdPvMap = new(sync.Map)

// the map od diskID and pvName
// storing the disk in creating status
// var diskProcessingMap = map[string]string{}
var diskProcessingMap = new(sync.Map)

// storing deleting disk
// var diskDeletingMap = map[string]string{}
var diskDeletingMap = new(sync.Map)

// storing attaching disk
// var diskAttachingMap = map[string]string{}
var diskAttachingMap = new(sync.Map)

// storing detaching disk
var diskDetachingMap = new(sync.Map)

// storing expanding disk
// var diskExpandingMap = map[string]string{}
var diskExpandingMap = new(sync.Map)

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

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	//resp, err := eks.CreateBlock("", "")
	//fmt.Println(resp, err)
	log.Infof("CreateVolume: Starting CreateVolume, req is:%+v", req)

	pvName := req.Name
	// Step 1: check the pvc(req.Name) is created or not. return pv directly if created, else do creation
	if v, ok := pvcCreatedMap.Load(pvName); ok {
		value, _ := v.(*csi.Volume)
		log.Warnf("CreateVolume: volume has been created, pvName: %s, volumeContext: %v, return directly", pvName, value.VolumeContext)
		return &csi.CreateVolumeResponse{Volume: value}, nil
	}

	// Step 2: check critical params
	if req.Parameters == nil {
		log.Errorf("CreateVolume: SC-Config (req.Parameters) cannot be empty")
		return nil, status.Error(codes.InvalidArgument, "SC-Config (req.Parameters) cannot be empty")

	}
	if pvName == "" {
		log.Errorf("CreateVolume: pv Name (req.Name) cannot be empty")
		return nil, status.Error(codes.InvalidArgument, "pv Name (req.Name) cannot be empty")
	}

	if req.VolumeCapabilities == nil {
		log.Errorf("CreateVolume: req.VolumeCapabilities cannot be empty")
		return nil, status.Error(codes.InvalidArgument, "req.VolumeCapabilities cannot be empty")
	}

	if req.GetCapacityRange() == nil {
		log.Errorf("CreateVolume: Capacity cannot be empty")
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: Capacity cannot be empty", req.Name)
	}

	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	diskRequestGB := req.CapacityRange.RequiredBytes / (1024 * 1024 * 1024)

	log.Infof("CreateVolume: diskRequestGB is: %d", diskRequestGB)

	diskVol, err := parseDiskVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: error parameters from input, err is: %s", err.Error())
	}

	// Step 4: create disk
	// check if disk is in creating first
	//if value, ok := diskProcessingMap[pvName]; ok {
	if v, ok := diskProcessingMap.Load(pvName); ok {
		value, _ := v.(string)
		if value == "creating" {
			log.Warnf("CreateVolume: Disk Volume(%s)'s is in creating, please wait", pvName)

			if tmp, ok := pvcCreatedMap.Load(pvName); ok {
				tmpVol, _ := tmp.(*csi.Volume)
				log.Warnf("CreateVolume: Disk Volume(%s)'s diskID: %s creating process finished, return context", pvName, value)
				return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
			}

			return nil, fmt.Errorf("CreateVolume: Disk Volume(%s) is in creating, please wait", pvName)
		}

		log.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)

		return nil, fmt.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
	}

	// do request to create ebs disk
	// diskName, diskType, diskSiteID, diskZoneID string, diskSize, diskIops int
	createRes, err := createEbsDisk(pvName, diskVol.StorageType, diskVol.AzId, int(diskRequestGB), 0)
	if err != nil {
		log.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
		return nil, fmt.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
	}

	diskIdSet := createRes.Data.DiskIdSet
	if len(diskIdSet) != 1 {
		return nil, fmt.Errorf("invalid diskIdSet:%s", diskIdSet)
	}
	diskID := diskIdSet[0]
	taskID := createRes.Data.EventId

	//diskProcessingMap[pvName] = "creating"
	diskProcessingMap.Store(pvName, "creating")

	// check create ebs disk event
	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		//diskProcessingMap[pvName] = "error"
		diskProcessingMap.Store(pvName, "error")
		return nil, err
	}

	// clean creating disk
	//delete(diskProcessingMap, pvName)
	diskProcessingMap.Delete(pvName)

	// Step 5: generate return volume context for /csi.v1.Controller/ControllerPublishVolume GRPC
	volumeContext := map[string]string{
		"fsType":      diskVol.FsType,
		"storageType": diskVol.StorageType,
		"azId":        diskVol.AzId,
	}

	tmpVol := &csi.Volume{
		VolumeId:      diskID,
		CapacityBytes: int64(volSizeBytes),
		VolumeContext: volumeContext,
		AccessibleTopology: []*csi.Topology{
			{
				Segments: map[string]string{
					TopologyZoneKey: "",
				},
			},
		},
	}
	diskIdPvMap.Store(diskID, pvName)
	//diskIdPvMap[diskID] = pvName

	pvcCreatedMap.Store(pvName, tmpVol)

	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskID is: %s", pvName, diskID)

	return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume: Starting deleting volume, req is: %+v", req)

	diskID := req.VolumeId
	if diskID == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}

	//if value, ok := diskDeletingMap[diskID]; ok {
	if v, ok := diskDeletingMap.Load(diskID); ok {
		value, _ := v.(string)
		if value == "deleting" || value == "detaching" {
			log.Warnf("DeleteVolume: diskID: %s is in [deleting|detaching] status, please wait", diskID)
			return nil, fmt.Errorf("DeleteVolume: diskID: %s is in [deleting|detaching] status, please wait", diskID)
		}

		log.Errorf("DeleteVolume: diskID: %s has been deleted but failed, please manual deal with it", diskID)
		return nil, fmt.Errorf("DeleteVolume: diskID: %s has been deleted but failed, please manual deal with it", diskID)
	}

	// Step 2: find disk by volumeID
	disk, err := findDiskByVolumeID(req.VolumeId)
	if err != nil {
		log.Errorf("DeleteVolume: findDiskByVolumeID error, err is: %s", err.Error())
		return nil, err
	}

	if disk != nil && disk.Code == "InvalidParameter" && strings.Contains(disk.Message, "云盘信息不存在") {
		log.Warnf("DeleteVolume: disk had been deleted by InvalidParameter")
		return &csi.DeleteVolumeResponse{}, nil
	}

	switch disk.Data.DiskInfo.Status {
	case "running":
		return nil, fmt.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s", diskID)
	case "mounting", "unmounting", "updating", "creating_snapshot", "recovering":
		return nil, fmt.Errorf("DeleteVolume: disk's status is %s ,can't delete volumeID: %s", disk.Data.DiskInfo.Status, diskID)
	}
	// Step 4: delete disk
	deleteRes, err := deleteDisk(diskID)
	if err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, fmt.Errorf("DeleteVolume: delete disk error, err is: %s", err)
	}

	// store deleting disk status
	//diskDeletingMap[diskID] = "deleting"
	diskDeletingMap.Store(diskID, "deleting")

	taskID := deleteRes.Data.EventId
	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		//diskDeletingMap[diskID] = "error"
		diskDeletingMap.Store(diskID, "error")
		return nil, fmt.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
	}

	// Step 5: delete pvcCreatedMap
	//if pvName, ok := diskIdPvMap[diskID]; ok {
	//	delete(pvcCreatedMap, pvName)
	//}
	if pvName, ok := diskIdPvMap.Load(diskID); ok {
		name, _ := pvName.(string)
		pvcCreatedMap.Delete(name)
	}

	// Step 6: clear diskDeletingMap and diskIdPvMap
	//delete(diskIdPvMap, diskID)
	diskIdPvMap.Delete(diskID)

	//delete(diskDeletingMap, diskID)
	diskDeletingMap.Delete(diskID)

	// log.Infof("DeleteVolume: clean [diskIdPvMap] and [pvcMap] and [diskDeletingMap] succeed!")
	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	diskID := req.VolumeId
	nodeID := req.NodeId

	log.Infof("ControllerPublishVolume: pvName: %s, starting attach diskID: %s to node: %s", req.VolumeId, diskID, nodeID)

	// Step 2: check necessary params
	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
	}

	// check attaching status
	//if value, ok := diskAttachingMap[diskID]; ok {
	if v, ok := diskAttachingMap.Load(diskID); ok {
		value, _ := v.(string)
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

	diskStatus := res.Data.DiskInfo.Status
	diskMountedNodeID := res.Data.DiskInfo.EcsId
	if diskStatus == StatusEbsMounted {
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

			//diskAttachingMap[diskID] = "attaching"
			diskAttachingMap.Store(diskID, "attaching")
			if err := describeTaskStatus(taskID); err != nil {
				//diskAttachingMap[diskID] = "error"
				diskAttachingMap.Store(diskID, "error")
				log.Errorf("ControllerPublishVolume: cdsDisk.detachDisk task result failed, err is: %s", err)
				return nil, err
			}

			log.Warnf("ControllerPublishVolume: detach diskID: %s from nodeID: %s successfully", diskID, diskMountedNodeID)
		} else {
			log.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
			return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
		}
	} else if diskStatus == StatusEbsError {
		log.Errorf("ControllerPublishVolume: diskID: %s was in [deleted|error], cant attach to nodeID", diskID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s was in [deleted|error], cant attach to nodeID", diskID)
	}

	// Step 4: attach disk to node
	taskID, err := attachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
		return nil, err
	}

	//diskAttachingMap[diskID] = "attaching"
	diskAttachingMap.Store(diskID, "attaching")
	if err = describeTaskStatus(taskID); err != nil {
		//diskAttachingMap[diskID] = "error"
		diskAttachingMap.Store(diskID, "error")
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
		return nil, err
	}

	//delete(diskAttachingMap, diskID)
	diskAttachingMap.Delete(diskID)
	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// ControllerUnpublishVolume detach
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
	//if value, ok := diskDetachingMap[diskID]; ok {
	if v, ok := diskDetachingMap.Load(diskID); ok {
		value, _ := v.(string)
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

	if res.Data.DiskInfo.EcsId != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Step 4: detach disk
	taskID, err := detachDisk(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	//diskDetachingMap[diskID] = "detaching"
	diskDetachingMap.Store(diskID, "detaching")
	if err := describeTaskStatus(taskID); err != nil {
		//diskDetachingMap[diskID] = "error"
		diskDetachingMap.Store(diskID, "error")
		log.Errorf("ControllerUnpublishVolume: cdsDisk.detachDisk task result failed, err is: %s", err)
		return nil, err
	}

	//delete(diskDetachingMap, diskID)
	diskDetachingMap.Delete(diskID)
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

func (c *ControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest,
) (*csi.ControllerExpandVolumeResponse, error) {
	log.Infof("ControllerExpandVolume:: Starting expand disk with: %v", req)

	// check resize conditions
	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	requestGB := int((volSizeBytes + 1024*1024*1024 - 1) / (1024 * 1024 * 1024))
	diskID := req.VolumeId

	//if value, ok := diskExpandingMap[diskID]; ok {
	if v, ok := diskExpandingMap.Load(diskID); ok {
		value, _ := v.(string)
		if value == "creating" {
			log.Warnf("CreateVolume: Disk Volume(%s)'s is in creating, please wait", diskID)
			return nil, fmt.Errorf("ControllerExpandVolume: disk Volume(%s) is in expanding, please wait", diskID)
		}
	}

	res, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("ControllerExpandVolume:: find disk(%s) with error: %s", diskID, err.Error())
		return nil, fmt.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
	}

	if res == nil {
		log.Errorf("ControllerExpandVolume: expand disk find disk not exist: %s", diskID)
		return nil, status.Error(codes.Internal, "expand disk find disk not exist "+diskID)
	}

	diskSize := res.Data.DiskInfo.Size
	if requestGB == diskSize {
		log.Infof("ControllerExpandVolume:: expect size is same with current: %s, size: %dGi", req.VolumeId, requestGB)
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: volSizeBytes, NodeExpansionRequired: true}, nil
	}
	if requestGB < diskSize {
		log.Infof("ControllerExpandVolume:: expect size is less than current: %d, expected: %d, disk: %s", diskSize, requestGB, req.VolumeId)
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: volSizeBytes, NodeExpansionRequired: true}, nil
	}

	// Check if autoSnapshot is enabled

	// do resize
	resizeRes, err := expandEbsDisk(diskID, diskSize)
	if err != nil {
		log.Errorf("ControllerExpandVolume:: resize got error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "resize disk %s get error: %s", diskID, err.Error())
	}

	taskId := resizeRes.Data.EventId
	//diskExpandingMap[diskID] = "expanding"
	diskExpandingMap.Store(diskID, "expanding")

	// check create ebs disk event
	err = describeTaskStatus(taskId)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		//diskProcessingMap[diskID] = "error"
		diskProcessingMap.Store(diskID, "error")
		return nil, err
	}

	// clean creating disk
	//delete(diskProcessingMap, diskID)
	diskProcessingMap.Delete(diskID)

	checkDisk, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("ControllerExpandVolume:: find disk failed with error: %+v", err)
		return nil, status.Errorf(codes.Internal, "resize disk %s get error: %s", diskID, err.Error())
	}

	if requestGB != checkDisk.Data.DiskInfo.Size {
		log.Infof("ControllerExpandVolume:: resize disk err with excepted size: %vGB, actual size: %vGB", requestGB, checkDisk.Data.DiskInfo.Size)
		return nil, status.Errorf(codes.Internal, "resize disk err with excepted size: %vGB, actual size: %vGB", requestGB, checkDisk.Data.DiskInfo.Size)
	}

	log.Infof("ControllerExpandVolume:: Success to resize volume: %s from %dG to %dG", req.VolumeId, diskSize, requestGB)
	return &csi.ControllerExpandVolumeResponse{CapacityBytes: volSizeBytes, NodeExpansionRequired: true}, nil
}

func createEbsDisk(diskName, diskType, diskZoneID string, diskSize, diskIops int) (*cdsDisk.CreateEbsResp, error) {
	log.Infof("createDisk: diskName: %s, diskType: %s,  diskZoneID: %s, diskSize: %d, diskIops: %d", diskName, diskType, diskZoneID, diskSize, diskIops)
	res, err := cdsDisk.CreateEbs(&cdsDisk.CreateEbsReq{
		AvailableZoneCode: diskZoneID,
		DiskName:          diskName,
		DiskFeature:       diskType,
		Size:              diskSize,
		BillingMethod:     BillingMethodPostPaid,
	})

	if err != nil {
		log.Errorf("createDisk: cdsDisk.CreateDisk api error, err is: %s", err.Error())
		return nil, err
	}

	log.Infof("createDisk: successfully!")

	return res, nil
}

func expandEbsDisk(diskId string, diskSize int) (*cdsDisk.CreateEbsResp, error) {
	return new(cdsDisk.CreateEbsResp), nil
}

func describeTaskStatus(taskID string) error {
	log.Infof("describeTaskStatus: taskID is: %s", taskID)

	for i := 1; i < 120; i++ {
		res, err := cdsDisk.DescribeTaskStatus(taskID)
		if err != nil {
			log.Errorf("task api error, err is: %s", err)
			return fmt.Errorf("apiError")
		}
		switch res.Data.EventStatus {
		case "finish", "success":
			return nil
		case "error", "failed", "part_fail":
			return fmt.Errorf("taskError")
		case "doing", "init":
			log.Debugf("task:%s is running, sleep 10s", taskID)
			time.Sleep(10 * time.Second)
		default:
			return fmt.Errorf("unkonw task status:%s ,taskId: %s", res.Data.EventStatus, taskID)
		}
	}

	return fmt.Errorf("task time out, running more than 20 minutes")
}

func findDiskByVolumeID(volumeID string) (*cdsDisk.FindDiskByVolumeIDResponse, error) {

	log.Infof("findDiskByVolumeID: volumeID is: %s", volumeID)

	res, err := cdsDisk.FindDiskByVolumeID(&cdsDisk.FindDiskByVolumeIDArgs{
		DiskId: volumeID,
	})

	if err != nil {
		log.Errorf("findDiskByVolumeID: cdsDisk.FindDiskByVolumeID [api error], err is: %s", err)
		return nil, err
	}

	log.Infof("findDiskByVolumeID: Successfully!")

	return res, nil
}

func deleteDisk(diskID string) (*cdsDisk.DeleteDiskResponse, error) {
	log.Infof("deleteDisk: diskID is:%s", diskID)

	res, err := cdsDisk.DeleteDisk(&cdsDisk.DeleteDiskArgs{
		DiskIds: []string{diskID},
	})

	if err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk api error, err is: %s", err)
		return nil, err
	}

	log.Infof("deleteDisk: cdsDisk.DeleteDisk task creation succeed, taskID is: %s", res.Data.EventId)

	return res, nil
}

func attachDisk(diskID, nodeID string) (string, error) {
	// attach disk to node
	log.Infof("attachDisk: diskID: %s, nodeID: %s", diskID, nodeID)

	res, err := cdsDisk.AttachDisk(&cdsDisk.AttachDiskArgs{
		DiskIds:             []string{diskID},
		InstanceId:          nodeID,
		ReleaseWithInstance: 0, // 不随实例删除
	})

	if err != nil {
		log.Errorf("attachDisk: cdsDisk.attachDisk api error, err is: %s", err)
		return "", err
	}

	log.Infof("attachDisk: cdsDisk.attachDisk task creation succeed, taskID is: %s", res.Data.EventId)

	return res.Data.EventId, nil
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
		if node.Annotations == nil {
			continue
		}

		csiInfo, ok := node.Annotations["csi.volume.kubernetes.io/nodeid"]
		if !ok {
			continue
		}
		var csiMap = make(map[string]string)
		if err = json.Unmarshal([]byte(csiInfo), &csiMap); err != nil {
			continue
		}
		instanceId := csiMap[DriverEbsDiskTypeName]
		if instanceId != nodeId {
			continue
		}
		for _, value := range node.Status.Conditions {
			if value.Type == "Ready" {
				nodeStatus = value.Status
				break OuterLoop
			}
		}
	}

	log.Infof("describeNodeStatus: nodeStatus is: %s", nodeStatus)
	return nodeStatus, nil
}

func detachDisk(diskID string) (string, error) {
	// to detach disk from node
	log.Infof("detachDisk: diskID: %s", diskID)

	res, err := cdsDisk.DetachDisk(&cdsDisk.DetachDiskArgs{
		DiskIds: []string{diskID},
	})

	if err != nil {
		log.Errorf("detachDisk: cdsDisk.detachDisk api error, err is: %s", err)
		return "", err
	}

	log.Infof("detachDisk: cdsDisk.detachDisk task creation succeed, taskID is: %s", res.Data.EventId)

	return res.Data.EventId, nil
}
