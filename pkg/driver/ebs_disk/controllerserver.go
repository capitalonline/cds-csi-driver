package ebs_disk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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

var diskEventIdMap = new(sync.Map)

var AttachDetachMap = new(sync.Map)

var DiskMultiTaskMap = new(sync.Map)

type TaskRecord struct {
	ID        string
	StartTime time.Time
}

func NewControllerServer(d *DiskDriver) *ControllerServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes client: %v", err)
	}

	log.Infof("String thread to path pv topology every %s", PatchPVInterval)
	go patchTopologyOfPVsPeriodically(clientset)

	return &ControllerServer{
		Client:                  clientset,
		DefaultControllerServer: csicommon.NewDefaultControllerServer(d.csiDriver),
	}
}

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (resp *csi.CreateVolumeResponse, err error) {
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

	// check if disk is in creating first
	if v, ok := diskProcessingMap.LoadOrStore(pvName, "creating"); ok {
		value, _ := v.(string)
		if value == "creating" {
			log.Warnf("CreateVolume: Disk Volume(%s)'s is in creating, please wait", pvName)
			return nil, fmt.Errorf("CreateVolume: Disk Volume(%s) is in creating, please wait", pvName)
		} else if value == "error" {
			return nil, status.Errorf(codes.Unknown, "CreateVolume: Disk Volume(%s) error", pvName)
		}

		log.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
		return nil, fmt.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
	}

	defer func() {
		if resp == nil {
			diskProcessingMap.Store(pvName, "error")
		}
	}()

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

	if diskRequestGB < MinDiskSize {
		msg := fmt.Sprintf("CreateVolume: error parameters from input, disk size must greater than %dGB, request size: %d", MinDiskSize, diskRequestGB)
		log.Error(msg)
		return nil, status.Errorf(codes.InvalidArgument, msg)
	}

	log.Infof("CreateVolume: diskRequestGB is: %d", diskRequestGB)

	diskVol, err := parseDiskVolumeOptions(req)
	res, err := describeDiskQuota(diskVol.Zone)
	if err != nil || res == nil || len(res.Data.QuotaList) == 0 {
		log.Errorf("CreateVolume: error when describeDiskQuota,err: %v , res:%v", err, res)
		return nil, err
	}
	if diskRequestGB > int64(res.Data.QuotaList[0].FreeQuota) {
		quota := res.Data.QuotaList[0].FreeQuota
		msg := fmt.Sprintf("az %s free quota is: %d,less than requested %d", diskVol.Zone, quota, diskRequestGB)
		log.Error(msg)
		return nil, status.Error(codes.InvalidArgument, msg)
	}
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: error parameters from input, err is: %s", err.Error())
	}

	// do request to create ebs disk
	// diskName, diskType, diskSiteID, diskZoneID string, diskSize, diskIops int
	createRes, err := createEbsDisk(pvName, diskVol.StorageType, diskVol.Zone, int(diskRequestGB), 0)
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

	// check create ebs disk event
	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		return nil, err
	}

	// Step 5: generate return volume context for /csi.v1.Controller/ControllerPublishVolume GRPC
	volumeContext := map[string]string{
		"fsType":      diskVol.FsType,
		"storageType": diskVol.StorageType,
		"region":      diskVol.Region,
		"zone":        diskVol.Zone,
	}

	tmpVol := &csi.Volume{
		VolumeId:      diskID,
		CapacityBytes: int64(volSizeBytes),
		VolumeContext: volumeContext,
		AccessibleTopology: []*csi.Topology{
			{
				Segments: map[string]string{
					TopologyRegionKey: diskVol.Region,
					TopologyZoneKey:   diskVol.Zone,
				},
			},
		},
	}
	diskIdPvMap.Store(diskID, pvName)
	pvcCreatedMap.Store(pvName, tmpVol)
	diskProcessingMap.Delete(pvName)
	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskID is: %s", pvName, diskID)

	return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (resp *csi.DeleteVolumeResponse, err error) {
	log.Infof("DeleteVolume: Starting deleting volume, req is: %+v", req)

	diskID := req.VolumeId
	if diskID == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}

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

	if _, ok := diskDeletingMap.LoadOrStore(diskID, "deleting"); ok {
		return nil, fmt.Errorf("DeleteVolume: disk:(%s) had another process to delete", diskID)
	}

	defer func() {
		diskDeletingMap.Delete(diskID)
	}()

	// Step 4: delete disk
	deleteRes, err := deleteDisk(diskID)
	if err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, fmt.Errorf("DeleteVolume: delete disk error, err is: %s", err)
	}

	taskID := deleteRes.Data.EventId
	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		return nil, fmt.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
	}

	if pvName, ok := diskIdPvMap.Load(diskID); ok {
		name, _ := pvName.(string)
		pvcCreatedMap.Delete(name)
	}

	diskIdPvMap.Delete(diskID)

	// log.Infof("DeleteVolume: clean [diskIdPvMap] and [pvcMap] and [diskDeletingMap] succeed!")
	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (resp *csi.ControllerPublishVolumeResponse, err error) {
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
	log.Infof("Attach Disk Info %v", res.Data.DiskInfo)

	diskStatus := res.Data.DiskInfo.Status
	diskMountedNodeID := res.Data.DiskInfo.EcsId
	if diskStatus == StatusEbsMounted && diskMountedNodeID != "" {
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

			if _, ok := AttachDetachMap.LoadOrStore(nodeID, "doing"); ok {
				log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
				return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
			}
			defer func() {
				AttachDetachMap.Delete(nodeID)
			}()

			if _, ok := DiskMultiTaskMap.LoadOrStore(diskID, "doing"); ok {
				log.Errorf("The Disk %s Has Another Event, Please wait", diskID)
				return nil, status.Errorf(codes.InvalidArgument, "The Disk %s Has Another Event, Please wait", diskID)
			}

			defer func() {
				DiskMultiTaskMap.Delete(diskID)
			}()
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
	} else if diskStatus != StatusWaitEbs {
		log.Errorf("ControllerPublishVolume: diskID %s status is %s, cant attach to nodeID %s", diskID, diskStatus, nodeID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID %s status is %s, cant attach to nodeID %s", diskID, diskStatus, nodeID)
	}

	describeRes, err := describeInstances(nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: describeInstances %s failed: %v, it will try again later", nodeID, err)
		return nil, err
	}
	if describeRes.Data.Status != StatusEcsRunning {
		msg := fmt.Sprintf("ControllerPublishVolume: node %s is not running, attach will try again later", nodeID)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}
	var number = len(describeRes.Data.Disk.DataDiskConf)
	if number+1 > 16 {
		msg := fmt.Sprintf("ControllerPublishVolume: node %s's data disk number is %d,supported max number is 16", nodeID, number)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}

	if _, ok := AttachDetachMap.LoadOrStore(nodeID, "doing"); ok {
		log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
		return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
	}

	defer func() {
		AttachDetachMap.Delete(nodeID)
	}()

	if _, ok := DiskMultiTaskMap.LoadOrStore(diskID, "doing"); ok {
		log.Errorf("The Disk %s Has Another Event, Please wait", diskID)
		return nil, status.Errorf(codes.InvalidArgument, "The Disk %s Has Another Event, Please wait", diskID)
	}

	defer func() {
		DiskMultiTaskMap.Delete(diskID)
	}()

	// Step 4: attach disk to node
	taskID, err := attachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
		return nil, err
	}

	//diskAttachingMap[diskID] = "attaching"
	//defer deleteNodeId(nodeID, taskID)

	diskAttachingMap.Store(diskID, "attaching")
	if err = describeTaskStatus(taskID); err != nil {
		//diskAttachingMap[diskID] = "error"
		diskAttachingMap.Store(diskID, "error")
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
		return nil, err
	}

	diskAttachingMap.Delete(diskID)
	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// ControllerUnpublishVolume detach
func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (resp *csi.ControllerUnpublishVolumeResponse, err error) {

	// Step 1: get necessary params
	diskID := req.VolumeId
	nodeID := req.NodeId
	log.Infof("ControllerUnpublishVolume: starting detach disk: %s from node: %s", diskID, nodeID)

	if _, ok := diskEventIdMap.Load(nodeID); ok {
		log.Errorf("ControllerUnpublishVolume: Disk has another Event, please wait")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: Disk has another Event, please wait")
	}

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
	log.Infof("Detach Disk Info %#v", res.Data.DiskInfo)

	if res.Data.DiskInfo.EcsId == "" || res.Data.DiskInfo.EcsId != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if _, ok := AttachDetachMap.LoadOrStore(nodeID, "doing"); ok {
		log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
		return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
	}
	defer func() {
		AttachDetachMap.Delete(nodeID)
	}()

	if _, ok := DiskMultiTaskMap.LoadOrStore(diskID, "doing"); ok {
		log.Errorf("The Disk %s Has Another Event, Please wait", diskID)
		return nil, status.Errorf(codes.InvalidArgument, "The Disk %s Has Another Event, Please wait", diskID)
	}

	defer func() {
		DiskMultiTaskMap.Delete(diskID)
	}()

	// Step 4: detach disk
	taskID, err := detachDisk(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}
	diskEventIdMap.Store(nodeID, taskID)

	defer deleteNodeId(nodeID, taskID)

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

func (c *ControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	log.Infof("ControllerExpandVolume:: Starting expand disk with: %v", req)

	diskID := req.VolumeId

	res, err := findDiskByVolumeID(req.VolumeId)
	if err != nil {
		return nil, fmt.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
	}

	if res == nil {
		return nil, fmt.Errorf("expand disk find disk not exist: %s", diskID)
	}

	if res.Data.DiskInfo.Status != "running" && res.Data.DiskInfo.Status != "waiting" {
		return nil, fmt.Errorf("ControllerExpandVolume: disk's status is %s ,can't expend volumeID: %s", res.Data.DiskInfo.Status, diskID)
	}

	if res.Data.DiskInfo.EcsId != "" {
		ecs, err := describeInstances(res.Data.DiskInfo.EcsId)
		if err != nil {
			return nil, fmt.Errorf("failed to describe instances: %v", err)
		}
		if ecs == nil {
			return nil, fmt.Errorf("err query ecs %s", res.Data.DiskInfo.EcsId)
		}
		if ecs.Data.Status != "running" {
			return nil, fmt.Errorf("ecs %s status is not running", res.Data.DiskInfo.EcsId)
		}
	}
	volSizeBytes := req.GetCapacityRange().GetRequiredBytes()
	requestGB := int(volSizeBytes / (1024 * 1024 * 1024))

	diskSize := res.Data.DiskInfo.Size
	if requestGB == diskSize {
		log.Infof("ControllerExpandVolume:: expect size is same with current: %s, size: %dGi", req.VolumeId, requestGB)
		if res.Data.DiskInfo.Status == "waiting" {
			return &csi.ControllerExpandVolumeResponse{CapacityBytes: volSizeBytes, NodeExpansionRequired: false}, nil
		}
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: volSizeBytes, NodeExpansionRequired: true}, nil
	}

	if requestGB < diskSize {
		log.Infof("ControllerExpandVolume:: expect size is less than current: %d, expected: %d, disk: %s", diskSize, requestGB, req.VolumeId)
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: volSizeBytes, NodeExpansionRequired: false}, nil
	}

	if _, err = expandEbsDisk(diskID, requestGB); err != nil {
		return nil, fmt.Errorf("ControllerExpandVolume:: resize disk %s get error: %s", diskID, err.Error())
	}

	return nil, fmt.Errorf("ControllerExpandVolume: disk Volume(%s) is in expanding, please wait", diskID)
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

func describeDiskQuota(zone string) (*cdsDisk.DescribeDiskQuotaResponse, error) {

	res, err := cdsDisk.DescribeDiskQuota(zone)

	if err != nil {
		log.Errorf("deleteDisk: cdsDisk.describeDiskQuota api error, err is: %s", err)
		return nil, err
	}

	return res, nil
}

func describeInstances(instancesId string) (*cdsDisk.DescribeInstanceResponse, error) {
	res, err := cdsDisk.DescribeInstance(instancesId)

	if err != nil {
		log.Errorf("deleteDisk: cdsDisk.describeInstances api error, err is: %s", err)
		return nil, err
	}

	return res, nil
}

func deleteNodeId(nodeId, taskID string) {
	if v, ok := diskEventIdMap.Load(nodeId); ok {
		if v == taskID {
			diskEventIdMap.Delete(nodeId)
		}
	}
}

func patchTopologyOfPVsPeriodically(clientSet *kubernetes.Clientset) {
	for {
		patchTopologyOfPVs(clientSet)
		time.Sleep(PatchPVInterval)
	}
}

func patchTopologyOfPVs(clientSet *kubernetes.Clientset) {
	pvs, err := clientSet.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		log.Errorf("Failed to patch the topology of PVs: cannot list all pvs: %s", err)
	}

	for _, pv := range pvs.Items {
		region := pv.Spec.PersistentVolumeSource.CSI.VolumeAttributes["region"]
		zone := pv.Spec.PersistentVolumeSource.CSI.VolumeAttributes["zone"]

		//skip if the PV is not created by this driver
		if pv.Spec.PersistentVolumeSource.CSI.Driver != DriverEbsDiskTypeName {
			continue
		}

		//creat labels if the pv doesn't have any labels yet
		if pv.Labels == nil {
			pv.Labels = make(map[string]string)
		} else {
			// skip if the topology has been patched
			if pv.Labels[TopologyZoneKey] == zone && pv.Labels[TopologyRegionKey] == region {
				continue
			}
		}

		log.Infof("Patching the topology of PV %s with region=%s->%s, zone=%s->%s", pv.Name, pv.Labels[TopologyRegionKey], region, pv.Labels[TopologyZoneKey], zone)
		pv.Labels[TopologyRegionKey] = region
		pv.Labels[TopologyZoneKey] = zone
		_, err = clientSet.CoreV1().PersistentVolumes().Update(&pv)
		if err != nil {
			log.Errorf("Failed to patch the topology of PV %s: %s", pv.Name, err)
		}
		log.Infof("PV %s has been patched", pv.Name)
	}
}

func expandEbsDisk(diskID string, diskSize int) (*cdsDisk.ExtendDiskResponse, error) {
	res, err := cdsDisk.ExtendDisk(&cdsDisk.ExtendDiskArgs{
		DiskId:       diskID,
		ExtendedSize: diskSize,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to extend disk %s by OpenAPI %s", diskID, err.Error())
	}
	log.Infof("expend disk %s, eventId %s", diskID, res.Data.EventId)
	return res, nil
}
