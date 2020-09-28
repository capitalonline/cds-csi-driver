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
// storing the block in creating status
var blockProcessingMap = map[string]string{}

// storing deleting block
var blockDeletingMap = map[string]string{}

// storing attaching block
var blockAttachingMap = map[string]string{}

// storing detaching block
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

// to create block
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
	blockRequestGB := req.CapacityRange.RequiredBytes / (1024 * 1024 * 1024)

	log.Infof("CreateVolume: blockRequestGB is: %d", blockRequestGB)

	// Step 3: parse DiskVolume params
	blockVol, err := parseBlockVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: error parameters from input, err is: %s", err.Error())
	}

	// Step 4: create block
	// check if block is in creating
	if value, ok := blockProcessingMap[pvName]; ok {
		if value == "creating" {
			log.Warnf("CreateVolume: block Volume(%s)'s is in creating, please wait", pvName)

			if tmpVol, ok := pvcCreatedMap[pvName]; ok {
				log.Infof("CreateVolume: block Volume(%s)'s blockID: %s creating process finished, return context", pvName, value)
				return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
			}

			return nil, fmt.Errorf("CreateVolume: block Volume(%s) is in creating, please wait", pvName)
		}

		log.Errorf("CreateVolume: block Volume(%s)'s creating process error", pvName)

		return nil, fmt.Errorf("CreateVolume: block Volume(%s)'s creating process error", pvName)
	}

	// do creation
	// blockName, blockType, blockSiteID, blockZoneID string, blockSize, blockIops int
	iopsInt, _ := strconv.Atoi(blockVol.Iops)
	bandwidthInt, _ := strconv.Atoi(blockVol.Bandwidth)
	createRes, err := createBlock(pvName, blockVol.StorageType, blockVol.SiteID, int(blockRequestGB), iopsInt, bandwidthInt)
	if err != nil {
		log.Errorf("CreateVolume: createBlock error, err is: %s", err.Error())
		return nil, fmt.Errorf("CreateVolume: createBlock error, err is: %s", err.Error())
	}

	blockID := createRes.Data.VolumeID
	taskID := createRes.TaskID

	// store creating block
	blockProcessingMap[pvName] = "creating"

	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createBlock: describeTaskStatus task result failed, err is: %s", err.Error())
		blockProcessingMap[pvName] = "error"
		return nil, err
	}

	// clean creating block
	delete(blockProcessingMap, pvName)

	// Step 5: generate return volume context
	volumeContext := map[string]string{}

	volumeContext["fsType"] = blockVol.FsType
	volumeContext["storageType"] = blockVol.StorageType
	volumeContext["siteID"] = blockVol.SiteID
	volumeContext["iops"] = blockVol.Iops

	tmpVol := &csi.Volume{
		VolumeId:      blockID,
		CapacityBytes: int64(volSizeBytes),
		VolumeContext: volumeContext,
		AccessibleTopology: []*csi.Topology{
			{
				Segments: map[string]string{
					TopologySiteKey: blockVol.SiteID,
				},
			},
		},
	}

	// Step 6: store
	// store blockID and pvName(pvName is equal to pvcName)
	blockIdPvMap[blockID] = pvName
	// store req.Name and csi.Volume
	pvcCreatedMap[pvName] = tmpVol

	log.Infof("CreateVolume: store [blockIdPvMap] and [pvcMap] succeed")
	log.Infof("CreateVolume: successfully create block, pvName is: %s, blockID is: %s", pvName, blockID)

	return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
}

// to delete block
func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume: Starting deleting volume, req is: %+v", req)

	// Step 1: check params
	blockID := req.VolumeId
	if blockID == "" {
		log.Error("DeleteVolume: req.VolumeID cannot be empty")
		return nil, fmt.Errorf("DeleteVolume: req.VolumeID cannot be empty")
	}

	// check if deleting or not
	if value, ok := blockDeletingMap[blockID]; ok {
		if value == "deleting" || value == "detaching" {
			log.Warnf("DeleteVolume: blockID: %s is in [deleting|detaching] status, please wait", blockID)
			return nil, fmt.Errorf("DeleteVolume: blockID: %s is in [deleting|detaching] status, please wait", blockID)
		}

		log.Errorf("DeleteVolume: blockID: %s has been deleted but failed, please manual deal with it", blockID)
		return nil, fmt.Errorf("DeleteVolume: blockID: %s has been deleted but failed, please manual deal with it", blockID)
	}

	// Step 2: find block by volumeID
	block, err := findBlockByVolumeID(req.VolumeId)
	if err != nil {
		log.Errorf("DeleteVolume: findBlockByVolumeID error, err is: %s", err.Error())
		return nil, err
	}
	if block.Data.BlockSlice == nil {
		log.Error("DeleteVolume: findBlockByVolumeID(cdsBlock.findBlockByVolumeID) response is nil")
		return nil, fmt.Errorf("DeleteVolume: findBlockByVolumeID(cdsBlock.findBlockByVolumeID) response is nil")
	}

	// Step 3: check block detached from node or not, detach it firstly if not
	diskStatus := block.Data.BlockSlice[0].Status
	// log.Infof("DeleteVolume: findBlockByVolumeID succeed, blockID is: %s, diskStatus is: %s", blockID, diskStatus)
	if diskStatus == StatusInMounted {
		log.Errorf("DeleteVolume: block [mounted], cant delete volumeID: %s ", blockID)
		return nil, fmt.Errorf("DeleteVolume: block [mounted], cant delete volumeID: %s", blockID)
	} else if diskStatus == StatusInOK {
		log.Infof("DeleteVolume: block is in [idle], then to delete directly!")
	} else if diskStatus == StatusInDeleted {
		log.Warnf("DeleteVolume: block had been deleted")
		return &csi.DeleteVolumeResponse{}, nil
	}

	// Step 4: delete block
	deleteRes, err := deleteBlock(blockID)
	if err != nil {
		log.Errorf("DeleteVolume: delete block error, err is: %s", err)
		return nil, fmt.Errorf("DeleteVolume: delete block error, err is: %s", err)
	}

	// store deleting block status
	blockDeletingMap[blockID] = "deleting"

	taskID := deleteRes.TaskID
	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("deleteBlock: cdsBlock.deleteBlock task result failed, err is: %s", err)
		blockDeletingMap[blockID] = "error"
		return nil, fmt.Errorf("deleteBlock: cdsBlock.deleteBlock task result failed, err is: %s", err)
	}

	// Step 5: delete pvcCreatedMap
	if pvName, ok := blockIdPvMap[blockID]; ok {
		delete(pvcCreatedMap, pvName)
	}

	// Step 6: clear blockDeletingMap and blockIdPvMap
	delete(blockIdPvMap, blockID)
	delete(blockDeletingMap, blockID)

	log.Infof("DeleteVolume: clean [blockIdPvMap] and [pvcMap] and [blockDeletingMap] succeed!")
	log.Infof("DeleteVolume: Successfully delete blockID: %s !", blockID)

	return &csi.DeleteVolumeResponse{}, nil
}

// to attach block to node
func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// Step 1: get necessary params
	blockID := req.VolumeId
	nodeID := req.NodeId

	log.Infof("ControllerPublishVolume: pvName: %s, starting attach blockID: %s to node: %s", req.VolumeId, blockID, nodeID)

	// Step 2: check necessary params
	if blockID == "" || nodeID == "" {
		log.Errorf("ControllerPublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume missing [VolumeId/NodeId] in request")
	}

	// check attaching status
	if value, ok := blockAttachingMap[blockID]; ok {
		if value == "attaching" {
			log.Warnf("ControllerPublishVolume: blockID: %s is in attaching, please wait", blockID)
			return nil, fmt.Errorf("ControllerPublishVolume: blockID: %s is in attaching, please wait", blockID)
		}

		log.Warnf("ControllerPublishVolume: blockID: %s attaching process finished, return context", blockID)
		return &csi.ControllerPublishVolumeResponse{}, nil
	}

	// Step 3: check block status
	res, err := findBlockByVolumeID(blockID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: findBlockByVolumeID api error, err is: %s", err)
		return nil, fmt.Errorf("ControllerPublishVolume: findBlockByVolumeID api error, err is: %s", err)
	}

	if res.Data.BlockSlice == nil {
		log.Errorf("ControllerPublishVolume: findBlockByVolumeID res is nil")
		return nil, fmt.Errorf("ControllerPublishVolume: findBlockByVolumeID res is nil")
	}

	diskStatus := res.Data.BlockSlice[0].Status
	diskMountedNodeID := res.Data.BlockSlice[0].NodeID

	blockAttachingMap[blockID] = "attaching"
	defer delete(blockAttachingMap, blockID)

	if diskStatus == StatusInMounted {
		if diskMountedNodeID == nodeID {
			log.Warnf("ControllerPublishVolume: blockID: %s had been attached to nodeID: %s", blockID, nodeID)
			return &csi.ControllerPublishVolumeResponse{}, nil
		}

		log.Warnf("ControllerPublishVolume: blockID: %s had been attached to nodeID: %s is different from current nodeID: %s, check node status", blockID, diskMountedNodeID, nodeID)

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
			//err = surplusDetachDisk(blockID)
			//if err != nil {
			//	log.Errorf("ControllerPublishVolume: surplusDetachDisk error, err is: %s", err)
			//	return nil, fmt.Errorf("ControllerPublishVolume: surplusDetachDisk error, err is: %s", err)
			//}

			taskID, err := detachBlock(blockID)
			if err != nil {
				log.Errorf("ControllerPublishVolume: detach blockID: %s from nodeID: %s error,  err is: %s", blockID, diskMountedNodeID, err.Error())
				return nil, fmt.Errorf("ControllerPublishVolume: detach blockID: %s from nodeID: %s error,  err is: %s", blockID, diskMountedNodeID, err.Error())
			}

			if err := describeTaskStatus(taskID); err != nil {
				log.Errorf("ControllerPublishVolume: cdsBlock.detachBlock task result failed, err is: %s", err)
				return nil, err
			}

			log.Warnf("ControllerPublishVolume: detach blockID: %s from nodeID: %s successfully", blockID, diskMountedNodeID)
		} else {
			log.Errorf("ControllerPublishVolume: blockID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", blockID, diskMountedNodeID, nodeID)
			return nil, fmt.Errorf("ControllerPublishVolume: blockID: %s had been attached to [Ready] nodeID: %s, cant attach to different nodeID: %s", blockID, diskMountedNodeID, nodeID)
		}
	} else if diskStatus == StatusInDeleted || diskStatus == StatusInError {
		log.Errorf("ControllerPublishVolume: blockID: %s was in [deleted|error], cant attach to nodeID", blockID)
		return nil, fmt.Errorf("ControllerPublishVolume: blockID: %s was in [deleted|error], cant attach to nodeID", blockID)
	}

	// Step 4: attach block to node
	//// use surplusAttachDisk temp
	//err = surplusAttachDisk(blockID, nodeID)
	//if err != nil {
	//	log.Errorf("ControllerPublishVolume: attach block:%s processing to node: %s with error, err is: %s", blockID, nodeID, err.Error())
	//	return nil, fmt.Errorf("ControllerPublishVolume: attach block:%s processing to node: %s with error, err is: %s", blockID, nodeID, err.Error())
	//}

	resAttach, err := attachBlock(blockID, nodeID)
	if err != nil {
		log.Errorf("ControllerPublishVolume: create attach task failed, err is:%s", err.Error())
		return nil, err
	}

	if err = describeTaskStatus(resAttach.TaskID); err != nil {
		blockAttachingMap[blockID] = "error"
		log.Errorf("ControllerPublishVolume: attach block:%s processing to node: %s with error, err is: %s", blockID, nodeID, err.Error())
		return nil, err
	}

	log.Infof("ControllerPublishVolume: Successfully attach block: %s to node: %s", blockID, nodeID)

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// to detach block from node
func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	// Step 1: get necessary params
	blockID := req.VolumeId
	nodeID := req.NodeId
	log.Infof("ControllerUnpublishVolume: starting detach block: %s from node: %s", blockID, nodeID)

	// Step 2: check necessary params
	if blockID == "" || nodeID == "" {
		log.Errorf("ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume: missing [VolumeId/NodeId] in request")
	}

	// Step 3: check block status
	if value, ok := blockDetachingMap[blockID]; ok && value == "detaching" {
		log.Warnf("ControllerUnpublishVolume: blockID: %s is in detaching, please wait", blockID)
		return nil, fmt.Errorf("ControllerUnpublishVolume: blockID: %s is in detaching, please wait", blockID)
	}

	blockDetachingMap[blockID] = "detaching"
	defer delete(blockDetachingMap, blockID)

	res, err := findBlockByVolumeID(blockID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findBlockByVolumeID error, err is: %s", err)
		return nil, fmt.Errorf("ControllerUnpublishVolume: findBlockByVolumeID error, err is: %s", err)
	}

	if res == nil {
		log.Errorf("ControllerUnpublishVolume: findBlockByVolumeID res is nil")
		return nil, fmt.Errorf("ControllerUnpublishVolume: findBlockByVolumeID res is nil")
	}

	if res.Data.BlockSlice[0].NodeID != nodeID {
		log.Warnf("ControllerUnpublishVolume: blockID: %s had been detached from nodeID: %s", blockID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Step 4: detach block
	// use surplusDetachDisk temp
	//err = surplusDetachDisk(blockID)
	//if err != nil {
	//	log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
	//	return nil, err
	//}

	taskID, err := detachBlock(blockID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	if err := describeTaskStatus(taskID); err != nil {
		log.Errorf("ControllerUnpublishVolume: cdsBlock.detachBlock task result failed, err is: %s", err)
		return nil, err
	}


	log.Infof("ControllerUnpublishVolume: Successfully detach block: %s from node: %s", blockID, nodeID)

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

func createBlock(blockName, blockType, blockSiteID string, blockSize, blockIops, blockBandwidth int) (*cdsBlock.CreateBlockResponse, error) {

	log.Infof("createBlock: blockName: %s, blockType: %s, blockSiteID: %s, blockSize: %d, blockIops: %d, blockBandwidth: %d", blockName, blockType, blockSiteID, blockSize, blockIops, blockBandwidth)

	// create block
	res, err := cdsBlock.CreateBlock(&cdsBlock.CreateBlockArgs{
		Name:      blockName,
		RegionID:  blockSiteID,
		DiskType:  blockType,
		Size:      blockSize,
		Iops:      blockIops,
		Bandwidth: blockBandwidth,
	})

	if err != nil {
		log.Errorf("createBlock: cdsBlock.createBlock api error, err is: %s", err.Error())
		return nil, err
	}

	log.Infof("createBlock: successfully!")

	return res, nil
}

func deleteBlock(blockID string) (*cdsBlock.DeleteBlockResponse, error) {
	log.Infof("deleteBlock: blockID is:%s", blockID)

	res, err := cdsBlock.DeleteBlock(&cdsBlock.DeleteBlockArgs{
		VolumeID: blockID,
	})

	if err != nil {
		log.Errorf("deleteBlock: cdsBlock.deleteBlock api error, err is: %s", err)
		return nil, err
	}

	log.Infof("deleteBlock: cdsBlock.deleteBlock task creation succeed, taskID is: %s", res.TaskID)

	return res, nil
}

func attachBlock(blockID, nodeID string) (*cdsBlock.AttachBlockResponse, error) {
	// attach block to node
	log.Infof("attachBlock: blockID: %s, nodeID: %s", blockID, nodeID)

	res, err := cdsBlock.AttachBlock(&cdsBlock.AttachBlockArgs{
		VolumeID: blockID,
		NodeID:   nodeID,
		BmInstanceId: nodeID,
	})

	if err != nil {
		log.Errorf("attachBlock: cdsBlock.attachBlock api error, err is: %s", err)
		return nil, err
	}

	log.Infof("attachBlock: cdsBlock.attachBlock task creation succeed, taskID is: %s", res.TaskID)

	return res, nil
}

func detachBlock(blockID string) (string, error) {
	// to detach block from node
	log.Infof("detachBlock: blockID: %s", blockID)

	res, err := cdsBlock.DetachBlock(&cdsBlock.DetachBlockArgs{
		VolumeID: blockID,
	})

	if err != nil {
		log.Errorf("detachBlock: cdsBlock.detachBlock api error, err is: %s", err)
		return "", err
	}

	log.Infof("detachBlock: cdsBlock.detachBlock task creation succeed, taskID is: %s", res.TaskID)

	return res.TaskID, nil
}

func findBlockByVolumeID(volumeID string) (*cdsBlock.FindBlockByVolumeIDResponse, error) {

	log.Infof("findBlockByVolumeID: volumeID is: %s", volumeID)

	res, err := cdsBlock.FindBlockByVolumeID(&cdsBlock.FindBlockByVolumeIDArgs{
		VolumeID: volumeID,
	})

	if err != nil {
		log.Errorf("findBlockByVolumeID: cdsBlock.findBlockByVolumeID [api error], err is: %s", err)
		return nil, err
	}

	log.Infof("findBlockByVolumeID: Successfully!")

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

//func surplusAttachDisk(blockID, nodeID string) error {
//	log.Infof("surplusAttachDisk: blockID is: %s, nodeID is: %s", blockID, nodeID)
//
//	// Step 1: attach
//	res, err := attachBlock(blockID, nodeID)
//	if err != nil {
//		log.Errorf("surplusAttachDisk: create attach task failed, err is:%s", err.Error())
//		return err
//	}
//
//	npn := res.Data.Npn
//	vip := res.Data.Vip
//	port := res.Data.Port
//	log.Infof("surplusAttachDisk: npn: %s, vip: %s, port: %s", npn, vip, port)
//
//	if err = describeTaskStatus(res.TaskID); err != nil {
//		log.Errorf("surplusAttachDisk: attach block:%s processing to node: %s with error, err is: %s", blockID, nodeID, err.Error())
//		return err
//	}
//
//	// Step 2: connect
//	// temp
//	discoveryCmd := fmt.Sprintf("nvme  discover -t rdma -a 10.177.178.201 -s 10060")
//	if _, err := blockUtils.RunCommand(discoveryCmd); err != nil {
//		log.Errorf("surplusAttachDisk: blockUtils.RunCommand error, err is: %s", err.Error())
//		return err
//	}
//
//	// temp
//	res1, err1 := findBlockByVolumeID(blockID)
//	if err1 != nil {
//		log.Errorf("surplusAttachDisk: findBlockByVolumeID api error, err is: %s", err1)
//		return fmt.Errorf("surplusAttachDisk: findBlockByVolumeID api error, err is: %s", err1)
//	}
//	diskUUid := res1.Data.BlockSlice[0].Uuid
//	log.Infof("surplusAttachDisk: diskUUid: %s", diskUUid)
//
//	connectCmd := fmt.Sprintf("nvme connect -n nqn.2019-06.suzaku.org:ssd_pool.g%s -t rdma -a 10.177.178.201 -s 10062", diskUUid)
//	if _, err := blockUtils.RunCommand(connectCmd); err != nil {
//		log.Errorf("surplusAttachDisk: blockUtils.RunCommand error, err is: %s", err.Error())
//		return err
//	}
//
//	log.Infof("surplusAttachDisk: successfully!")
//	return nil
//}
//
//func surplusDetachDisk(blockID string) error {
//	log.Infof("surplusDetachDisk: blockID is: %s", blockID)
//
//	// Step 1: disconnect
//	// temp
//	res1, err1 := findBlockByVolumeID(blockID)
//	if err1 != nil {
//		log.Errorf("surplusAttachDisk: findBlockByVolumeID api error, err is: %s", err1)
//		return fmt.Errorf("surplusAttachDisk: findBlockByVolumeID api error, err is: %s", err1)
//	}
//	diskUUid := res1.Data.BlockSlice[0].Uuid
//	log.Infof("surplusAttachDisk: diskUUid: %s", diskUUid)
//
//	disconnectCmd := fmt.Sprintf("nvme disconnect -n nqn.2019-06.suzaku.org:ssd_pool.g%s", diskUUid)
//	if _, err := blockUtils.RunCommand(disconnectCmd); err != nil {
//		log.Errorf("surplusDetachDisk: blockUtils.RunCommand error, err is: %s", err.Error())
//		return err
//	}
//
//	// Step 2: detach
//	taskID, err := detachBlock(blockID)
//	if err != nil {
//		log.Errorf("surplusDetachDisk: detach blockID: %s,  err is: %s", blockID, err.Error())
//		return err
//	}
//
//	if err := describeTaskStatus(taskID); err != nil {
//		log.Errorf("surplusDetachDisk: cdsBlock.detachBlock task result failed, err is: %s", err)
//		return err
//	}
//
//	log.Infof("surplusDetachDisk: successfully!")
//	return nil
//}
