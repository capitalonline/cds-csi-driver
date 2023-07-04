package eks_block

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

const (
	StatusEbsMounted      = "running"
	StatusEbsError        = "error"
	StatusWaitEbs         = "waiting"
	MinDiskSize           = 24
	EcsMountLimit         = 16
	StatusEcsRunning      = "running"
	ebsSsdDisk            = "SSD"
	DriverEbsDiskTypeName = "ebs-disk.csi.cds.net"
	BillingMethodPostPaid = "0"
)

//// the map of req.Name and csi.Volume.
//var pvcCreatedMap = new(sync.Map)
//
//// the map of diskId and pvName
//// diskId and pvName is not same under csi plugin
//// var diskIdPvMap = map[string]string{}
//var diskIdPvMap = new(sync.Map)
//
//// the map od diskID and pvName
//// storing the disk in creating status
//// var diskProcessingMap = map[string]string{}
//var diskProcessingMap = new(sync.Map)
//
//// storing deleting disk
//// var diskDeletingMap = map[string]string{}
//var diskDeletingMap = new(sync.Map)
//
//// storing attaching disk
//// var diskAttachingMap = map[string]string{}
//var diskAttachingMap = new(sync.Map)
//
//// storing detaching disk
//var diskDetachingMap = new(sync.Map)
//
//var diskEventIdMap = new(sync.Map)

var AttachDetachMap = new(sync.Map)

var DiskMultiTaskMap = new(sync.Map)

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

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (resp *csi.CreateVolumeResponse, err error) {
	log.Infof("CreateVolume: Starting CreateVolume, req is:%#v", req)
	pvName := req.Name
	// Step 1: check critical params
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

	volSizeBytes := req.GetCapacityRange().GetRequiredBytes()
	diskRequestGB := req.CapacityRange.RequiredBytes / (1024 * 1024 * 1024)

	diskVol, err := parseDiskVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: error parameters from input, err is: %s", err.Error())
	}

	if diskRequestGB < MinDiskSize {
		msg := fmt.Sprintf("CreateVolume: error parameters from input, disk size must greater than %dGB, request size: %d", MinDiskSize, diskRequestGB)
		log.Error(msg)
		return nil, status.Errorf(codes.InvalidArgument, msg)
	}

	// todo 查询余量
	res, err := describeDiskQuota(diskVol.AzId)
	if err != nil || res == nil || len(res.Data.QuotaList) == 0 {
		log.Errorf("CreateVolume: error when describeDiskQuota,err: %v , res:%v", err, res)
		return nil, err
	}
	// todo 查询余量是否满足条件 -> openapi
	if diskRequestGB > int64(res.Data.QuotaList[0].FreeQuota) {
		quota := res.Data.QuotaList[0].FreeQuota
		msg := fmt.Sprintf("az %s free quota is: %d,less than requested %d", diskVol.AzId, quota, diskRequestGB)
		log.Error(msg)
		return nil, status.Error(codes.InvalidArgument, msg)
	}

	// todo 查询pv(块)是否创建 eks openapi 接口查询 node -> pv_name  （creating  error waiting（详情））
	//if v, ok := diskProcessingMap.LoadOrStore(pvName, "creating"); ok {
	//	value, _ := v.(string)
	//	if value == "creating" {
	//		log.Warnf("CreateVolume: Disk Volume(%s)'s is in creating, please wait", pvName)
	//		return nil, fmt.Errorf("CreateVolume: Disk Volume(%s) is in creating, please wait", pvName)
	//	} else if value == "error" {
	//		return nil, status.Errorf(codes.Unknown, "CreateVolume: Disk Volume(%s) error", pvName)
	//	}
	//
	//	log.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
	//	return nil, fmt.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
	//}

	log.Infof("CreateVolume: diskRequestGB is: %d", diskRequestGB)

	// todo eks openapi 创建盘（pv）-> pv_name -> creating
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

	// todo eks openapi 任务查询
	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		return nil, err
	}

	// Step 5: generate return volume context for /csi.v1.Controller/ControllerPublishVolume GRPC
	// node 表： fsType storageType:SSD, {}
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
	// todo 不要查询内存
	//if v, ok := diskDeletingMap.Load(diskID); ok {
	//	value, _ := v.(string)
	//	if value == "deleting" || value == "detaching" {
	//		log.Warnf("DeleteVolume: diskID: %s is in [deleting|detaching] status, please wait", diskID)
	//		return nil, fmt.Errorf("DeleteVolume: diskID: %s is in [deleting|detaching] status, please wait", diskID)
	//	}
	//
	//	log.Errorf("DeleteVolume: diskID: %s has been deleted but failed, please manual deal with it", diskID)
	//	return nil, fmt.Errorf("DeleteVolume: diskID: %s has been deleted but failed, please manual deal with it", diskID)
	//}

	// 直接为openapi查询
	disk, err := findDiskByVolumeID(req.VolumeId)
	if err != nil {
		log.Errorf("DeleteVolume: findDiskByVolumeID error, err is: %s", err.Error())
		return nil, err
	}

	if disk != nil && disk.Code == "InvalidParameter" && strings.Contains(disk.Message, "云盘信息不存在") {
		log.Warnf("DeleteVolume: disk had been deleted by InvalidParameter")
		return &csi.DeleteVolumeResponse{}, nil
	}
	// todo 查询盘如果不处于 待挂载waiting 状态，异常错误的
	switch disk.Data.DiskInfo.Status {
	case "running":
		return nil, fmt.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s", diskID)
	case "mounting", "unmounting", "updating", "creating_snapshot", "recovering":
		return nil, fmt.Errorf("DeleteVolume: disk's status is %s ,can't delete volumeID: %s", disk.Data.DiskInfo.Status, diskID)
	}

	// todo 去除diskDeletingMap锁
	//if _, ok := diskDeletingMap.LoadOrStore(diskID, "deleting"); ok {
	//	return nil, fmt.Errorf("DeleteVolume: disk:(%s) had another process to delete", diskID)
	//}
	//
	//defer func() {
	//	diskDeletingMap.Delete(diskID)
	//}()

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

	// todo 去除
	//if pvName, ok := diskIdPvMap.Load(diskID); ok {
	//	name, _ := pvName.(string)
	//	pvcCreatedMap.Delete(name)
	//}
	//
	//diskIdPvMap.Delete(diskID)

	// log.Infof("DeleteVolume: clean [diskIdPvMap] and [pvcMap] and [diskDeletingMap] succeed!")
	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume 挂载
func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (resp *csi.ControllerPublishVolumeResponse, err error) {
	diskID := req.VolumeId
	nodeID := req.NodeId

	log.Infof("ControllerPublishVolume: pvName: %s, starting attach diskID: %s to node: %s", req.VolumeId, diskID, nodeID)

	// Step 2: check necessary params
	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
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
	// 挂载操作
	switch diskStatus {
	case StatusWaitEbs:
		// todo openapi eks查询节点（状态，挂盘数量）
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
		// todo 简化内存锁
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
		if err = describeTaskStatus(taskID); err != nil {
			log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
			return nil, err
		}
		log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)
	case StatusEbsMounted:
		// 下面的逻辑是为了解决挂载漂移的（卸载错误的节点，挂载正确的节点）
		if diskMountedNodeID != "" {
			if diskMountedNodeID == nodeID {
				log.Warnf("ControllerPublishVolume: diskID: %s had been attached to nodeID: %s", diskID, nodeID)
				return &csi.ControllerPublishVolumeResponse{}, nil
			}
			// id 两方不一样，如果下面节点异常，会进行先卸载，再挂载，
			log.Warnf("ControllerPublishVolume: diskID: %s had been attached to nodeID: %s is different from current nodeID: %s, check node status", diskID, diskMountedNodeID, nodeID)

			// check node status
			nodeStatus, err := describeNodeStatus(ctx, c, diskMountedNodeID)
			if err != nil {
				log.Warnf("ControllerPublishVolume: check nodeStatus error, err is: %s", err)
				return nil, err
			}
			// 检查当前pv已挂载的节点是否正常运行，如果未正常运行（nodeStatus != "True"），卸载pv和node
			if nodeStatus != "True" {
				// 错误节点漂移
				// node is not exist or NotReady status, detach force
				log.Warnf("ControllerPublishVolume: diskMountedNodeID: %s is in [NotRead|Not Exist], detach forcely", diskMountedNodeID)

				// todo 内存锁可以有， 简化一下， key：id， value：bool
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
				// todo nodeid running，diskid running， 可以卸载
				taskID, err := detachDisk(diskID)
				if err != nil {
					log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
					return nil, fmt.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
				}

				if err := describeTaskStatus(taskID); err != nil {
					log.Errorf("ControllerPublishVolume: cdsDisk.detachDisk task result failed, err is: %s", err)
					return nil, err
				}

				log.Warnf("ControllerPublishVolume: detach diskID: %s from nodeID: %s successfully", diskID, diskMountedNodeID)
			} else {
				// todo 并发挂盘，其中一个盘已经抢到节点，其他盘就不能在继续挂这节点了，应该做一个拦截操作
				log.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
				return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
			}
		}
	default:
		// todo 整理一下返回信息， 拦截
		log.Errorf("ControllerPublishVolume: diskID %s status is %s, cant attach to nodeID %s", diskID, diskStatus, nodeID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID %s status is %s, cant attach to nodeID %s", diskID, diskStatus, nodeID)

	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// ControllerUnpublishVolume 卸载
func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (resp *csi.ControllerUnpublishVolumeResponse, err error) {
	// Step 1: get necessary params
	diskID := req.VolumeId
	nodeID := req.NodeId
	log.Infof("ControllerUnpublishVolume: starting detach disk: %s from node: %s", diskID, nodeID)
	// todo 事件处理？无用
	//if _, ok := diskEventIdMap.Load(nodeID); ok {
	//	log.Errorf("ControllerUnpublishVolume: Disk has another Event, please wait")
	//	return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: Disk has another Event, please wait")
	//}

	// Step 2: check necessary params
	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
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
	// todo 如果是卸载中，返回异常
	log.Infof("Detach Disk Info %#v", res.Data.DiskInfo)

	if res.Data.DiskInfo.EcsId == "" || res.Data.DiskInfo.EcsId != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
	// todo 内存锁 简化
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

	// Step 4: detach disk todo 优化成 锁+操作+事件 封装一个函数
	taskID, err := detachDisk(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	//diskDetachingMap[diskID] = "detaching"

	if err := describeTaskStatus(taskID); err != nil {
		//diskDetachingMap[diskID] = "error"

		log.Errorf("ControllerUnpublishVolume: cdsDisk.detachDisk task result failed, err is: %s", err)
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

// todo+操作+事件 同步
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
	// todo eks openapi 查询任务
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

// todo+操作+事件 同步
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

// todo 建议：内存锁， 查询事件的逻辑写道这里来（完成流程，同时做好内存锁的锁定和释放），当检查到有锁的时候抛异常
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

// todo 建议：内存锁， 查询事件的逻辑写道这里来（完成流程，同时做好内存锁的锁定和释放），当检查到有锁的时候抛异常
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

func describeDiskQuota(azId string) (*cdsDisk.DescribeDiskQuotaResponse, error) {

	res, err := cdsDisk.DescribeDiskQuota(azId)

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

// ControllerExpandVolume 这期不做
func (c *ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
