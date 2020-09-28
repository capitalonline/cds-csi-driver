package block

import (
	"context"
	"fmt"
	// "github.com/capitalonline/cluster-autoscaler/clusterstate/utils"
	v12 "k8s.io/api/core/v1"
	"strconv"
	"time"

	cdsBlock "github.com/capitalonline/cck-sdk-go/pkg/cck/block"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	blockUtils "github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// the map of req.Name and csi.Volume.
var pvcCreatedMap = map[string]*csi.Volume{}

// the map of diskId and pvName
// diskId and pvName is not same under csi plugin
var blockIdPvMap = map[string]string{}

// the map of diskID and pvName
// storing the disk in creating status
var blockProcessingMap = map[string]string{}

// storing deleting disk
var blockDeletingMap = map[string]string{}

// storing attaching disk
var blockAttachingMap = map[string]string{}

// storing detaching disk
var blockDetachingMap = map[string]string{}

func NewControllerServer(d *BlockDriver) *ControllerServer {
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
	log.Infof("CreateVolume: Starting CreateVolume, req is:%+v", req)

	pvName := req.Name
	// Step 1: check the pvc(req.Name) is created or not. return pv directly if created, else do creation
	if value, ok := pvcCreatedMap[pvName]; ok {
		log.Warnf("CreateVolume: volume has been created, pvName: %s, volumeContext: %v, return directly", pvName, value.VolumeContext)
		return &csi.CreateVolumeResponse{Volume: value}, nil
	}

	// Step 2: check critical params
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

	// Step 3: parse DiskVolume params
	diskVol, err := parseBlockVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: error parameters from input, err is: %s", err.Error())
	}

	// Step 4: create disk
	// check if disk is in creating
	if value, ok := blockProcessingMap[pvName]; ok {
		if value == "creating" {
			log.Warnf("CreateVolume: Disk Volume(%s)'s is in creating, please wait", pvName)

			if tmpVol, ok := pvcCreatedMap[pvName]; ok {
				log.Infof("CreateVolume: Disk Volume(%s)'s diskID: %s creating process finished, return context", pvName, value)
				return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
			}

			return nil, fmt.Errorf("CreateVolume: Disk Volume(%s) is in creating, please wait", pvName)
		}

		log.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)

		return nil, fmt.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
	}

	// do creation
	// diskName, diskType, diskSiteID, diskZoneID string, diskSize, diskIops int
	iopsInt, _ := strconv.Atoi(diskVol.Iops)
	bandwidthInt, _ := strconv.Atoi(diskVol.Bandwidth)
	createRes, err := createDisk(pvName, diskVol.StorageType, diskVol.SiteID, int(diskRequestGB), iopsInt, bandwidthInt)
	if err != nil {
		log.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
		return nil, fmt.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
	}

	diskID := createRes.Data.VolumeID
	taskID := createRes.TaskID

	// store creating disk
	blockProcessingMap[pvName] = "creating"

	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		blockProcessingMap[pvName] = "error"
		return nil, err
	}

	// clean creating disk
	delete(blockProcessingMap, pvName)

	// Step 5: generate return volume context
	volumeContext := map[string]string{}

	volumeContext["fsType"] = diskVol.FsType
	volumeContext["storageType"] = diskVol.StorageType
	volumeContext["siteID"] = diskVol.SiteID
	volumeContext["iops"] = diskVol.Iops

	tmpVol := &csi.Volume{
		VolumeId:      diskID,
		CapacityBytes: int64(volSizeBytes),
		VolumeContext: volumeContext,
		AccessibleTopology: []*csi.Topology{
			{
				Segments: map[string]string{
					TopologySiteKey: diskVol.SiteID,
				},
			},
		},
	}

	// Step 6: store
	// store diskId and pvName(pvName is equal to pvcName)
	blockIdPvMap[diskID] = pvName
	// store req.Name and csi.Volume
	pvcCreatedMap[pvName] = tmpVol

	log.Infof("CreateVolume: store [blockIdPvMap] and [pvcMap] succeed")
	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskID is: %s", pvName, diskID)

	return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
}

// to delete disk
func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume: Starting deleting volume, req is: %+v", req)

	// Step 1: check params
	diskID := req.VolumeId
	if diskID == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}

	// check if deleting or not
	if value, ok := blockDeletingMap[diskID]; ok {
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
	if disk.Data.BlockSlice == nil {
		log.Error("DeleteVolume: findDiskByVolumeID(cdsBlock.FindDiskByVolumeID) response is nil")
		return nil, fmt.Errorf("DeleteVolume: findDiskByVolumeID(cdsBlock.FindDiskByVolumeID) response is nil")
	}

	// Step 3: check disk detached from node or not, detach it firstly if not
	diskStatus := disk.Data.BlockSlice[0].Status
	// log.Infof("DeleteVolume: findDiskByVolumeID succeed, diskID is: %s, diskStatus is: %s", diskID, diskStatus)
	if diskStatus == StatusInMounted {
		log.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s ", diskID)
		return nil, fmt.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s", diskID)
	} else if diskStatus == StatusInOK {
		log.Infof("DeleteVolume: disk is in [idle], then to delete directly!")
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
	blockDeletingMap[diskID] = "deleting"

	taskID := deleteRes.TaskID
	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("deleteDisk: cdsBlock.DeleteDisk task result failed, err is: %s", err)
		blockDeletingMap[diskID] = "error"
		return nil, fmt.Errorf("deleteDisk: cdsBlock.DeleteDisk task result failed, err is: %s", err)
	}

	// Step 5: delete pvcCreatedMap
	if pvName, ok := blockIdPvMap[diskID]; ok {
		delete(pvcCreatedMap, pvName)
	}

	// Step 6: clear blockDeletingMap and blockIdPvMap
	delete(blockIdPvMap, diskID)
	delete(blockDeletingMap, diskID)

	log.Infof("DeleteVolume: clean [blockIdPvMap] and [pvcMap] and [blockDeletingMap] succeed!")
	log.Infof("DeleteVolume: Successfully delete diskID: %s !", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

// to attach disk to node
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
	if value, ok := blockAttachingMap[diskID]; ok {
		if value == "attaching" {
			log.Warnf("ControllerPublishVolume: diskID: %s is in attaching, please wait", diskID)
			return nil, fmt.Errorf("ControllerPublishVolume: diskID: %s is in attaching, please wait", diskID)
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

	if res.Data.BlockSlice == nil {
		log.Errorf("ControllerPublishVolume: findDiskByVolumeID res is nil")
		return nil, fmt.Errorf("ControllerPublishVolume: findDiskByVolumeID res is nil")
	}

	diskStatus := res.Data.BlockSlice[0].Status
	diskMountedNodeID := res.Data.BlockSlice[0].NodeID

	blockAttachingMap[diskID] = "attaching"
	defer delete(blockAttachingMap, diskID)

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
			// node is not exist or in NotReady status, detach force
			log.Warnf("ControllerPublishVolume: diskMountedNodeID: %s is in [NotRead|NotExist], detach forcedly", diskMountedNodeID)

			// use surplusDetachDisk temp
			err = surplusDetachDisk(diskID)
			if err != nil {
				log.Errorf("ControllerPublishVolume: surplusDetachDisk error, err is: %s", err)
				return nil, fmt.Errorf("ControllerPublishVolume: surplusDetachDisk error, err is: %s", err)
			}

			//taskID, err := detachDisk(diskID)
			//if err != nil {
			//	log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
			//	return nil, fmt.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
			//}
			//
			//if err := describeTaskStatus(taskID); err != nil {
			//	log.Errorf("ControllerPublishVolume: cdsBlock.detachDisk task result failed, err is: %s", err)
			//	return nil, err
			//}

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
	// use surplusAttachDisk temp
	err = surplusAttachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
		return nil, fmt.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
	}

	//taskID, err := attachDisk(diskID, nodeID)
	//if err != nil {
	//	log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
	//	return nil, err
	//}
	//
	//if err = describeTaskStatus(taskID); err != nil {
	//	blockAttachingMap[diskID] = "error"
	//	log.Errorf("ControllerPublishVolume: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
	//	return nil, err
	//}
	//
	log.Infof("ControllerPublishVolume: Successfully attach disk: %s to node: %s", diskID, nodeID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// to detach disk from node
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
	if value, ok := blockDetachingMap[diskID]; ok && value == "detaching" {
		log.Warnf("ControllerUnpublishVolume: diskID: %s is in detaching, please wait", diskID)
		return nil, fmt.Errorf("ControllerUnpublishVolume: diskID: %s is in detaching, please wait", diskID)
	}

	blockDetachingMap[diskID] = "detaching"
	defer delete(blockDetachingMap, diskID)

	res, err := findDiskByVolumeID(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
	}

	if res == nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID res is nil")
		return nil, fmt.Errorf("ControllerUnpublishVolume: findDiskByVolumeID res is nil")
	}

	if res.Data.BlockSlice[0].NodeID != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Step 4: detach disk
	// use surplusDetachDisk temp
	err = surplusDetachDisk(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	//taskID, err := detachDisk(diskID)
	//if err != nil {
	//	log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
	//	return nil, err
	//}
	//
	//if err := describeTaskStatus(taskID); err != nil {
	//	log.Errorf("ControllerUnpublishVolume: cdsBlock.detachDisk task result failed, err is: %s", err)
	//	return nil, err
	//}
	//

	log.Infof("ControllerUnpublishVolume: Successfully detach disk: %s from node: %s", diskID, nodeID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (c *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	log.Infof("ValidateVolumeCapabilities: req is: %v", req)

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

func createDisk(diskName, diskType, diskSiteID string, diskSize, diskIops, diskBandwidth int) (*cdsBlock.CreateBlockResponse, error) {

	log.Infof("createDisk: diskName: %s, diskType: %s, diskSiteID: %s, diskSize: %d, diskIops: %d, diskBandwidth: %d", diskName, diskType, diskSiteID, diskSize, diskIops, diskBandwidth)

	// create disk
	res, err := cdsBlock.CreateBlock(&cdsBlock.CreateBlockArgs{
		Name:      diskName,
		RegionID:  diskSiteID,
		DiskType:  diskType,
		Size:      diskSize,
		Iops:      diskIops,
		Bandwidth: diskBandwidth,
	})

	if err != nil {
		log.Errorf("createDisk: cdsBlock.CreateDisk api error, err is: %s", err.Error())
		return nil, err
	}

	log.Infof("createDisk: successfully!")

	return res, nil
}

func deleteDisk(diskID string) (*cdsBlock.DeleteBlockResponse, error) {
	log.Infof("deleteDisk: diskID is:%s", diskID)

	res, err := cdsBlock.DeleteBlock(&cdsBlock.DeleteBlockArgs{
		VolumeID: diskID,
	})

	if err != nil {
		log.Errorf("deleteDisk: cdsBlock.DeleteDisk api error, err is: %s", err)
		return nil, err
	}

	log.Infof("deleteDisk: cdsBlock.DeleteDisk task creation succeed, taskID is: %s", res.TaskID)

	return res, nil
}

func attachDisk(diskID, nodeID string) (*cdsBlock.AttachBlockResponse, error) {
	// attach disk to node
	log.Infof("attachDisk: diskID: %s, nodeID: %s", diskID, nodeID)

	res, err := cdsBlock.AttachBlock(&cdsBlock.AttachBlockArgs{
		VolumeID: diskID,
		NodeID:   nodeID,
		BmInstanceId: nodeID,
	})

	if err != nil {
		log.Errorf("attachDisk: cdsBlock.attachDisk api error, err is: %s", err)
		return nil, err
	}

	log.Infof("attachDisk: cdsBlock.attachDisk task creation succeed, taskID is: %s", res.TaskID)

	return res, nil
}

func detachDisk(diskID string) (string, error) {
	// to detach disk from node
	log.Infof("detachDisk: diskID: %s", diskID)

	res, err := cdsBlock.DetachBlock(&cdsBlock.DetachBlockArgs{
		VolumeID: diskID,
	})

	if err != nil {
		log.Errorf("detachDisk: cdsBlock.detachDisk api error, err is: %s", err)
		return "", err
	}

	log.Infof("detachDisk: cdsBlock.detachDisk task creation succeed, taskID is: %s", res.TaskID)

	return res.TaskID, nil
}

func findDiskByVolumeID(volumeID string) (*cdsBlock.FindBlockByVolumeIDResponse, error) {

	log.Infof("findDiskByVolumeID: volumeID is: %s", volumeID)

	res, err := cdsBlock.FindBlockByVolumeID(&cdsBlock.FindBlockByVolumeIDArgs{
		VolumeID: volumeID,
	})

	if err != nil {
		log.Errorf("findDiskByVolumeID: cdsBlock.FindDiskByVolumeID [api error], err is: %s", err)
		return nil, err
	}

	log.Infof("findDiskByVolumeID: Successfully!")

	return res, nil
}

func describeTaskStatus(taskID string) error {
	log.Infof("describeTaskStatus: taskID is: %s", taskID)

	for i := 1; i < 120; i++ {
		res, err := cdsBlock.DescribeTaskStatus(taskID)
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

func surplusAttachDisk(diskID, nodeID string) error {
	log.Infof("surplusAttachDisk: diskID is: %s, nodeID is: %s", diskID, nodeID)

	// Step 1: attach
	res, err := attachDisk(diskID, nodeID)
	if err != nil {
		log.Errorf("surplusAttachDisk: create attach task failed, err is:%s", err.Error())
		return err
	}

	npn := res.Data.Npn
	vip := res.Data.Vip
	port := res.Data.Port
	log.Infof("surplusAttachDisk: npn: %s, vip: %s, port: %s", npn, vip, port)

	if err = describeTaskStatus(res.TaskID); err != nil {
		log.Errorf("surplusAttachDisk: attach disk:%s processing to node: %s with error, err is: %s", diskID, nodeID, err.Error())
		return err
	}

	// Step 2: connect
	// temp
	discoveryCmd := fmt.Sprintf("nvme  discover -t rdma -a 10.177.178.201 -s 10060")
	if _, err := blockUtils.RunCommand(discoveryCmd); err != nil {
		log.Errorf("surplusAttachDisk: blockUtils.RunCommand error, err is: %s", err.Error())
		return err
	}

	// temp
	res1, err1 := findDiskByVolumeID(diskID)
	if err1 != nil {
		log.Errorf("surplusAttachDisk: findDiskByVolumeID api error, err is: %s", err1)
		return fmt.Errorf("surplusAttachDisk: findDiskByVolumeID api error, err is: %s", err1)
	}
	diskUUid := res1.Data.BlockSlice[0].Uuid
	log.Infof("surplusAttachDisk: diskUUid: %s", diskUUid)

	connectCmd := fmt.Sprintf("nvme connect -n nqn.2019-06.suzaku.org:ssd_pool.g%s -t rdma -a 10.177.178.201 -s 10062", diskUUid)
	if _, err := blockUtils.RunCommand(connectCmd); err != nil {
		log.Errorf("surplusAttachDisk: blockUtils.RunCommand error, err is: %s", err.Error())
		return err
	}

	log.Infof("surplusAttachDisk: successfully!")
	return nil
}

func surplusDetachDisk(diskID string) error {
	log.Infof("surplusDetachDisk: diskID is: %s", diskID)

	// Step 1: disconnect
	// temp
	res1, err1 := findDiskByVolumeID(diskID)
	if err1 != nil {
		log.Errorf("surplusAttachDisk: findDiskByVolumeID api error, err is: %s", err1)
		return fmt.Errorf("surplusAttachDisk: findDiskByVolumeID api error, err is: %s", err1)
	}
	diskUUid := res1.Data.BlockSlice[0].Uuid
	log.Infof("surplusAttachDisk: diskUUid: %s", diskUUid)

	disconnectCmd := fmt.Sprintf("nvme disconnect -n nqn.2019-06.suzaku.org:ssd_pool.g%s", diskUUid)
	if _, err := blockUtils.RunCommand(disconnectCmd); err != nil {
		log.Errorf("surplusDetachDisk: blockUtils.RunCommand error, err is: %s", err.Error())
		return err
	}

	// Step 2: detach
	taskID, err := detachDisk(diskID)
	if err != nil {
		log.Errorf("surplusDetachDisk: detach diskID: %s,  err is: %s", diskID, err.Error())
		return err
	}

	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("surplusDetachDisk: cdsBlock.detachDisk task result failed, err is: %s", err)
		return err
	}

	log.Infof("surplusDetachDisk: successfully!")
	return nil
}