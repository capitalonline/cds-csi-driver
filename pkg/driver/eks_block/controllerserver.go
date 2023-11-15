package eks_block

import (
	"context"
	"encoding/json"
	"fmt"
	cckAlarm "github.com/capitalonline/cck-sdk-go/pkg/cck/alarm"
	"net/http"
	"time"

	api "github.com/capitalonline/cds-csi-driver/pkg/driver/eks_block/api"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client"
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
)

const (
	StatusEbsMounted      = "running"
	StatusEbsError        = "error"
	StatusWaitEbs         = "waiting"
	MinDiskSize           = 80
	EcsMountLimit         = 16
	StatusEcsRunning      = "running"
	ebsSsdDisk            = "SSD"
	DriverEbsDiskTypeName = "eks-disk.csi.cds.net"
	BillingMethodPostPaid = "0"
)

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

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (resp *csi.CreateVolumeResponse, err error) {
	log.Infof("CreateVolume: Starting CreateVolume, req is:%#v", req)
	pvName := req.Name
	// check critical params
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

	res, err := describeBlockLimit(diskVol.Zone)
	if err != nil || res == nil || res.Data.MaxRestVolume == 0 {
		log.Errorf("CreateVolume: error when describeDiskQuota,err: %v , res:%v", err, res)
		return nil, err
	}
	if diskRequestGB > int64(res.Data.MaxRestVolume) {
		quota := res.Data.MaxRestVolume
		msg := fmt.Sprintf("az %s free quota is: %d,less than requested %d", diskVol.Zone, quota, diskRequestGB)
		log.Error(msg)
		return nil, status.Error(codes.InvalidArgument, msg)
	}

	if _, ok := CacheLockMap.LoadOrStore(pvName, struct{}{}); ok {
		return nil, fmt.Errorf("当前pv有其它操作正在进行，请稍后再试")
	}
	defer CacheLockMap.Delete(pvName)
	// 查询pv(块)是否创建
	describeRes, err := describeBlockInfo("", pvName)
	if err != nil {
		log.Errorf("查询块存储详情失败，请稍后再试，err:%v", err)
		return nil, err
	}

	switch describeRes.Data.Status {
	case DiskStatusBuilding:
		return nil, fmt.Errorf("块存储%s正在创建中！", pvName)
	case DiskStatusError:
		// 中断
		return nil, fmt.Errorf("块存储%s创建失败！", pvName)
	case "":
		// 不存在，忽略
	default:
		return &csi.CreateVolumeResponse{Volume: &csi.Volume{
			VolumeId:      describeRes.Data.BlockId,
			CapacityBytes: int64(volSizeBytes),
			VolumeContext: map[string]string{
				"fsType":      diskVol.FsType,
				"storageType": diskVol.StorageType,
				"region":      diskVol.Region,
				"zone":        diskVol.Zone,
			},
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{
						TopologyZoneKey: "",
					},
				},
			},
		}}, nil
	}

	log.Infof("CreateVolume: diskRequestGB is: %d", diskRequestGB)

	createRes, err := createBlock(pvName, int(diskRequestGB), diskVol.Zone, diskVol.FsType)
	if err != nil || createRes.Data == nil {
		log.Errorf("CreateVolume: createDisk error, err is: %v", err)
		return nil, fmt.Errorf("CreateVolume: createDisk error, err is: %v", err)
	}

	blockId := createRes.Data.BlockId
	if blockId == "" {
		return nil, fmt.Errorf("创建块存储失败,返回值：%#v", createRes)
	}

	err = waitTaskFinish(createRes.Data.TaskId)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		return nil, err
	}

	// generate return volume context for /csi.v1.Controller/ControllerPublishVolume GRPC
	volumeContext := map[string]string{
		"fsType":      diskVol.FsType,
		"storageType": diskVol.StorageType,
		"region":      diskVol.Region,
		"zone":        diskVol.Zone,
	}

	tmpVol := &csi.Volume{
		VolumeId:      blockId,
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
	disk, err := describeBlockInfo(req.VolumeId, "")
	if err != nil {
		log.Errorf("DeleteVolume: findDiskByVolumeID error, err is: %s", err.Error())
		return nil, err
	}
	// 已经删除
	if disk.Data.BlockId == "" {
		return &csi.DeleteVolumeResponse{}, nil
	}
	// 非终态，不允许删除
	if disk.Data.Status != DiskStatusError && disk.Data.Status != DiskStatusWaiting && disk.Data.Status != DiskStatusUnmountFailed {
		msg := fmt.Sprintf("block status is %s ,not allowed to delete", disk.Data.Status)
		return nil, fmt.Errorf(msg)
	}

	// Step 4: delete disk
	_, err = deleteDisk(diskID)
	if err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, fmt.Errorf("DeleteVolume: delete disk error, err is: %s", err)
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

	res, err := describeBlockInfo(diskID, "")
	if err != nil {
		log.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
		return nil, fmt.Errorf("ControllerPublishVolume: findDiskByVolumeID api error, err is: %s", err)
	}
	// 块存储不存在
	if res.Data.BlockId == "" {
		msg := fmt.Sprintf("block %s not found", diskID)
		return nil, status.Errorf(codes.NotFound, msg)
	}
	log.Infof("Attach Disk Info %#v", res)

	diskStatus := res.Data.Status
	diskMountedNodeID := res.Data.NodeId
	// 挂载操作
	switch diskStatus {
	case DiskStatusWaiting:
		// 正常状态：待挂载
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

				_, err := detachDisk(diskID, diskMountedNodeID)
				if err != nil {
					log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
					return nil, fmt.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
				}
				log.Warnf("ControllerPublishVolume: detach diskID: %s from nodeID: %s successfully", diskID, diskMountedNodeID)
			} else {
				// 当前pv 已挂载一个正常的节点是，应该不需要进行挂载迁移的操作 中断
				log.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
				return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", diskID, diskMountedNodeID, nodeID)
			}
		}
	default:
		// 拦截
		log.Errorf("ControllerPublishVolume: diskID %s status is %s, cant attach to nodeID %s", diskID, diskStatus, nodeID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID %s status is %s, cant attach to nodeID %s", diskID, diskStatus, nodeID)
	}
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

	if describeRes.Data.BlockNum+1 > DiskMountLimit {
		msg := fmt.Sprintf("ControllerPublishVolume: node %s's data disk number is %d,supported max number is %d", nodeID, describeRes.Data.BlockNum, DiskMountLimit)
		log.Errorf(msg)
		return nil, fmt.Errorf(msg)
	}

	_, err = attachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
		return nil, err
	}
	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)

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

	res, err := describeBlockInfo(diskID, "")
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
	}
	if res == nil || res.Data.BlockId == "" {
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if res.Data.Status == DiskStatusWaiting {
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
	if res.Data.Status != DiskStatusRunning && res.Data.Status != DiskStatusUnmountFailed {
		return nil, fmt.Errorf("unmount failed, disk status %v", res.Data.Status)
	}

	if res.Data.NodeId == "" || res.Data.NodeId != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	_, err = detachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}
	log.Infof("ControllerUnpublishVolume: Successfully detach disk: %s from node: %s", diskID, nodeID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

// ControllerExpandVolume 扩容，这期不做
func (c *ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func createBlock(diskName string, diskSize int, zone, fs string) (*api.CreateBlockResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewCreateBlockRequest()
	request.DiskFeature = DiskFeatureSSD
	request.DiskSize = diskSize
	request.DiskName = diskName
	request.AvailableZoneCode = zone
	request.CreateSource = "eks-csi"
	request.FsType = fs

	return client.CreateBlock(request)
}

func waitTaskFinish(taskID string) error {
	count := 0
	for {
		time.Sleep(5 * time.Second)
		if count > 150 {
			return fmt.Errorf("query task faild more than %d", count)
		}
		resp, err := describeTaskStatus(taskID)
		if err != nil {
			count++
			if _, ok := alarmMap.Load(taskID); !ok && count > 3 {
				alarmRes, err := sendAlarm(&cckAlarm.SendAlarmArgs{
					Msg:        fmt.Sprintf("eks-csi 查询任务连续失败超过3次, 任务id:%s,请求错误码：%s, 错误信息：%s", taskID, resp.Code, resp.Msg),
					Hostname:   "容器重要告警",
					AlarmType:  "eks",
					AlarmGroup: "容器",
				})
				if err != nil || (alarmRes != nil && alarmRes.Code != "Success") {
					log.Errorf("send alarm ,err: %s, res:%#v", err.Error(), alarmRes)
				}
				alarmMap.Store(taskID, nil)
			}
			continue
		}
		taskStatus := resp.Data.TaskStatus
		if taskStatus == TaskStatusFinish {
			return nil
		}
		if taskStatus == TaskStatusError {
			return fmt.Errorf("task %s err,resp: %+v", taskID, resp)
		}
		count++
	}
}

func describeTaskStatus(taskId string) (*api.TaskStatusResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewTaskStatusRequest()
	request.TaskId = taskId
	return client.TaskStatus(request)
}

func describeBlockInfo(diskID string, blockName string) (*api.DescribeBlockInfoResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewDescribeBlockInfoRequest()
	request.BlockId = diskID
	request.BlockName = blockName
	return client.DescribeBlockInfo(request)
}

func deleteDisk(diskID string) (*api.DeleteBlockResponse, error) {
	if _, ok := CacheLockMap.LoadOrStore(diskID, true); ok {
		return nil, fmt.Errorf("当前pv有正在进行的操作，请稍后再试")
	}
	defer CacheLockMap.Delete(diskID)
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewDeleteBlockRequest()
	request.BlockId = diskID
	deleteRes, err := client.DeleteBlock(request)
	if err != nil || deleteRes.Code != "Success" {
		log.Errorf("ControllerPublishVolume: attach disk:%s  err is: %v", diskID, err)
		return nil, err
	}
	taskID := deleteRes.Data.TaskId
	if err = waitTaskFinish(taskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		return nil, fmt.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
	}
	return deleteRes, nil
}

func attachDisk(diskID, nodeID string) (*api.AttachBlockResponse, error) {
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
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewAttachBlockRequest()
	request.BlockId = diskID
	request.NodeId = nodeID
	resp, err := client.AttachBlock(request)
	if err != nil || resp.Code != "Success" {
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %v", diskID, nodeID, err)
		return nil, err
	}
	if err = waitTaskFinish(resp.Data.TaskId); err != nil {
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %v", diskID, nodeID, err)
		return nil, err
	}
	return resp, err
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

func detachDisk(diskID string, nodeId string) (*api.DetachBlockResponse, error) {
	// 锁节点
	if _, ok := CacheLockMap.LoadOrStore(nodeId, true); ok {
		log.Errorf("The Node %s Has Another Event, Please wait", nodeId)
		return nil, status.Errorf(codes.InvalidArgument, "The Node %s Has Another Event, Please wait", nodeId)
	}
	defer CacheLockMap.Delete(nodeId)

	// 锁disk
	if _, ok := CacheLockMap.LoadOrStore(diskID, true); ok {
		log.Errorf("The disk %s Has Another Event, Please wait", diskID)
		return nil, status.Errorf(codes.InvalidArgument, "The disk %s Has Another Event, Please wait", diskID)
	}
	defer CacheLockMap.Delete(diskID)
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewDetachBlockRequest()
	request.BlockId = diskID
	resp, err := client.DetachBlock(request)
	if err != nil || resp.Code != "Success" {
		log.Errorf("ControllerPublishVolume: attach disk:%s, err is: %v", diskID, err)
		return nil, err
	}
	if err = waitTaskFinish(resp.Data.TaskId); err != nil {
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %v", diskID, nodeId, err)
		return nil, err
	}
	return resp, err
}

func describeBlockLimit(zone string) (*api.DescribeBlockLimitResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodPost)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewDescribeBlockLimitRequest()
	request.AvailableZoneCodes = []string{zone}
	return client.DescribeBlockLimit(request)
}

func describeNodeMountNum(instancesId string) (*api.DescribeNodeMountNumResponse, error) {
	cpf := profile.NewClientProfileWithMethod(http.MethodGet)
	client, _ := api.NewClient(eks_client.NewCredential(), "", cpf)
	request := api.NewDescribeNodeMountNumRequest()
	request.NodeId = instancesId
	return client.DescribeNodeMountNum(request)
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

func sendAlarm(args *cckAlarm.SendAlarmArgs) (*cckAlarm.SendAlarmResponse, error) {
	return cckAlarm.SendAlarm(args)
}
