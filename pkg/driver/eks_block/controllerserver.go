package eks_block

import (
	"context"
	"encoding/json"
	"fmt"
	api "github.com/capitalonline/cds-csi-driver/pkg/driver/eks_block/api"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client"
	common "github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/http"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/profile"
	"github.com/container-storage-interface/spec/lib/go/csi"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v12 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
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

//var AttachDetachMap = new(sync.Map)
//
//var DiskMultiTaskMap = new(sync.Map)

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

	res, err := describeBlockLimit(diskVol.AzId)
	if err != nil || res == nil || res.Data.MaxRestVolume == 0 {
		log.Errorf("CreateVolume: error when describeDiskQuota,err: %v , res:%v", err, res)
		return nil, err
	}
	if diskRequestGB > int64(res.Data.MaxRestVolume) {
		quota := res.Data.MaxRestVolume
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

	if _, ok := CacheLockMap.LoadOrStore(pvName, struct{}{}); ok {
		time.Sleep(time.Second * 10)
		return nil, fmt.Errorf("当前pv有其它操作正在进行，请稍后再试")
	}
	defer CacheLockMap.Delete(pvName)
	createRes, err := createBlock(pvName, int(diskRequestGB), diskVol.AzId)
	if err != nil {
		log.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
		return nil, fmt.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
	}

	blockId := createRes.Data.BlockId
	if blockId == "" {
		return nil, fmt.Errorf("创建块存储失败,返回值：%+v", createRes)
	}

	err = waitTaskFinsh(createRes.Data.TaskId)
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
		VolumeId:      blockId,
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
	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskID is: %s", pvName, blockId)

	return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (resp *csi.DeleteVolumeResponse, err error) {
	log.Infof("DeleteVolume: Starting deleting volume, req is: %+v", req)

	diskID := req.VolumeId
	if diskID == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}
	// 直接为openapi查询
	disk, err := describeBlockInfo(req.VolumeId)
	if err != nil {
		log.Errorf("DeleteVolume: findDiskByVolumeID error, err is: %s", err.Error())
		return nil, err
	}
	// 非终态，不允许删除
	if disk.Data.Status != DiskStatusError && disk.Data.Status != DiskStatusWaiting {
		time.Sleep(time.Second * 10)
		msg := fmt.Sprintf("block status is %s ,not allowed to delete", disk.Data.Status)
		return nil, fmt.Errorf(msg)
	}
	// 已经删除
	if disk.Data.BlockId == "" {
		return &csi.DeleteVolumeResponse{}, nil
	}

	if _, ok := CacheLockMap.LoadOrStore(diskID, struct{}{}); ok {
		time.Sleep(time.Second * 10)
		return nil, fmt.Errorf("当前pv有其它操作正在进行，请稍后再试")
	}
	defer CacheLockMap.Delete(diskID)
	// Step 4: delete disk
	deleteRes, err := deleteDisk(diskID)
	if err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, fmt.Errorf("DeleteVolume: delete disk error, err is: %s", err)
	}

	taskID := deleteRes.Data.TaskId
	if err := waitTaskFinsh(taskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		return nil, fmt.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
	}
	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)
	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume 挂载
func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (resp *csi.ControllerPublishVolumeResponse, err error) {
	diskID := req.VolumeId
	nodeID := req.NodeId

	log.Infof("ControllerPublishVolume: pvName: %s, starting attach diskID: %s to node: %s", req.VolumeId, diskID, nodeID)

	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
	}

	res, err := describeBlockInfo(diskID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
		return nil, fmt.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
	}
	// 块存储不存在
	if res.Data.BlockId == "" {
		msg := fmt.Sprintf("block %s not found", diskID)
		return nil, status.Errorf(codes.NotFound, msg)
	}
	log.Infof("Attach Disk Info %+v", res)

	diskStatus := res.Data.Status
	diskMountedNodeID := res.Data.NodeId
	// 挂载操作
	switch diskStatus {

	case DiskStatusWaiting: // 待挂载
		describeRes, err := describeNodeMountNum(nodeID)
		if err != nil {
			log.Errorf("ControllerPublishVolume: describeInstances %s failed: %v, it will try again later", nodeID, err)
			return nil, err
		}

		if describeRes.Data.NodeStatus != StatusEcsRunning {
			msg := fmt.Sprintf("ControllerPublishVolume: node %s is not running, attach will try again later", nodeID)
			log.Errorf(msg)
			return nil, fmt.Errorf(msg)
		}

		// TODO 挂盘数量限制
		//var number = len(describeRes.Data.Disk.DataDiskConf)
		//if number+1 > 16 {
		//	msg := fmt.Sprintf("ControllerPublishVolume: node %s's data disk number is %d,supported max number is 16", nodeID, number)
		//	log.Errorf(msg)
		//	return nil, fmt.Errorf(msg)
		//}

		// 对节点和盘都上锁
		if _, ok := CacheLockMap.LoadOrStore(nodeID, "doing"); ok {
			log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
			return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
		}
		defer CacheLockMap.Delete(nodeID)
		if value, ok := CacheLockMap.LoadOrStore(diskID, "attaching"); ok {
			msg := fmt.Sprintf("disk %s is ,please wait", value)
			log.Errorf(msg)
			return nil, fmt.Errorf(msg)
		}
		defer CacheLockMap.Delete(diskID)

		attachRes, err := attachDisk(diskID, nodeID)
		if err != nil {
			log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
			return nil, err
		}
		if err = waitTaskFinsh(attachRes.Data.TaskId); err != nil {
			log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
			return nil, err
		}
		log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)
	case DiskStatusRunning: // 已挂载
		// 下面的逻辑是为了解决挂载漂移的（卸载错误的节点，挂载正确的节点）
		if diskMountedNodeID != "" {
			// 已经挂载在目标机器，直接退出
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

				// 锁节点
				if _, ok := CacheLockMap.LoadOrStore(nodeID, true); ok {
					log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
					return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
				}
				defer CacheLockMap.Delete(nodeID)

				// 锁disk
				if _, ok := CacheLockMap.LoadOrStore(diskID, true); ok {
					log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
					return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
				}
				defer CacheLockMap.Delete(diskID)

				detachRes, err := detachDisk(diskID)
				if err != nil {
					log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
					return nil, fmt.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
				}

				if err := waitTaskFinsh(detachRes.Data.TaskId); err != nil {
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
	diskID := req.VolumeId
	nodeID := req.NodeId
	log.Infof("ControllerUnpublishVolume: starting detach disk: %s from node: %s", diskID, nodeID)

	if diskID == "" || nodeID == "" {
		log.Errorf("ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
	}

	res, err := describeBlockInfo(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
	}

	if res == nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID res is nil")
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID res is nil")
	}
	// todo 如果是卸载中，返回异常
	log.Infof("Detach Disk Info %+v", res)

	if res.Data.NodeId == "" || res.Data.NodeId != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
	// 锁节点
	if _, ok := CacheLockMap.LoadOrStore(nodeID, nil); ok {
		log.Errorf("The Node %s Has Another Event, Please wait", nodeID)
		return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeID)
	}
	defer CacheLockMap.Delete(nodeID)

	// 锁disk
	if _, ok := CacheLockMap.LoadOrStore(diskID, nil); ok {
		log.Errorf("The disk %s Has Another Event, Please wait", diskID)
		return nil, status.Errorf(codes.InvalidArgument, "The disk %s Has Another Event, Please wait", diskID)

	}
	defer CacheLockMap.Delete(diskID)

	detachRes, err := detachDisk(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	if err := waitTaskFinsh(detachRes.Data.TaskId); err != nil {
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

// ControllerExpandVolume 这期不做
func (c *ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func createBlock(diskName string, diskSize int, azId string) (*api.CreateBlockResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.CreateBlockRequest{
		BaseRequest: &common.BaseRequest{},
		DiskFeature: DiskFeatureSSD,
		DiskSize:    diskSize,
		DiskName:    diskName,
		AzId:        azId,
	}
	return client.CreateBlock(request)
}

func waitTaskFinsh(taskID string) error {
	timer := time.NewTimer(time.Second * 2)
	ctx, _ := context.WithTimeout(context.Background(), time.Minute*40)
	var count int64
	for {
		select {
		case <-timer.C:
			resp, err := describeTaskStatus(taskID)
			if err != nil {
				count++
				continue
			}
			taskStatus := resp.Data.TaskStatus
			if taskStatus == TaskStatusFinish {
				return nil
			}
			if taskStatus == TaskStatusError {
				return fmt.Errorf("task %s err,resp: %+v", taskID, resp)
			}
		case <-ctx.Done():
			return fmt.Errorf("describe task %s time out", taskID)
		}
		if count > 10 {
			return fmt.Errorf("query task faild more than %d", count)
		}
	}
}

func describeTaskStatus(taskId string) (*api.TaskStatusResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.TaskStatusRequest{
		BaseRequest: &common.BaseRequest{},
		TaskId:      taskId,
	}
	return client.TaskStatus(request)
}

func describeBlockInfo(diskID string) (*api.DescribeBlockInfoResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.DescribeBlockInfoRequest{
		BaseRequest: &common.BaseRequest{},
		BlockId:     diskID,
	}
	return client.DescribeBlockInfo(request)
}

func deleteDisk(diskID string) (*api.DeleteBlockResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.DeleteBlockRequest{
		BaseRequest: &common.BaseRequest{},
		BlockId:     diskID,
	}
	return client.DeleteBlock(request)
}

// todo 建议：内存锁， 查询事件的逻辑写道这里来（完成流程，同时做好内存锁的锁定和释放），当检查到有锁的时候抛异常
func attachDisk(diskID, nodeID string) (*api.AttachBlockResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.AttachBlockRequest{
		BaseRequest: &common.BaseRequest{},
		BlockId:     diskID,
		NodeId:      nodeID,
	}
	return client.AttachBlock(request)
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
func detachDisk(diskID string) (*api.DetachBlockResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.DetachBlockRequest{
		BaseRequest: &common.BaseRequest{},
		BlockId:     diskID,
	}
	return client.DetachBlock(request)
}

func describeBlockLimit(azId string) (*api.DescribeBlockLimitResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.DescribeBlockLimitRequest{
		BaseRequest:        &common.BaseRequest{},
		AvailableZoneCodes: []string{azId},
	}
	return client.DescribeBlockLimit(request)
}

func describeNodeMountNum(instancesId string) (*api.DescribeNodeMountNumResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := &api.DescribeNodeMountNumRequest{
		BaseRequest: &common.BaseRequest{},
		NodeId:      instancesId,
	}
	return client.DescribeNodeMountNum(request)
}
