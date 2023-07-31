package ccsdisk

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"strings"
	"sync"

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
	diskAttachingState  = "attaching"
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
		DiskCountLock:           &sync.Mutex{},
	}
}

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Infof("CreateVolume: Starting CreateVolume, req is:%+v", req)

	diskVolume, err := parseDiskVolumeOptions(req)
	if err != nil {
		log.Errorf("CreateVolume: error parameters from input, err is: %s", err.Error())
		return nil, err
	}

	// check pvName
	if volumeInfo, isExist, err := c.checkVolumeInfo(req.GetName()); err != nil {
		log.Errorf("CreateVolume: failed to fetch %s from config map: %+v", req.GetName(), err)
		return nil, err
	} else if volumeInfo != nil && isExist {
		log.Infof("CreateVolume: %s has been created, skip this", req.GetName())
		return &csi.CreateVolumeResponse{Volume: volumeInfo}, nil
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
	volumeInfo := buildCreateVolumeResponse(req, diskVolume)

	// record volume info
	if err := c.saveVolumeInfo(req.GetName(), volumeInfo); err != nil {
		log.Errorf("failed to record %s to %s: %+v", req.GetName(), defaultVolumeRecordConfigMap, err)
		return nil, err
	}

	if err = checkCreateDiskState(diskID); err != nil {
		log.Errorf("createDisk: getDiskInfo result failed, err is: %s", err.Error())
		return nil, err
	}

	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskInfo: %+v", req.GetName(), diskVolume)

	return &csi.CreateVolumeResponse{Volume: volumeInfo}, nil
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

	if disk.Data.IsValid == 1 && disk.Data.Mounted == 1 {
		log.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s ", diskID)
		return nil, fmt.Errorf("DeleteVolume: disk [mounted], cant delete volumeID: %s", diskID)
	} else if disk.Data.IsValid == 0 {
		log.Infof("DeleteVolume[%s]: disk had been deleted, skip this", diskID)
		return &csi.DeleteVolumeResponse{}, nil
	}

	// delete volume record info
	if err := c.deleteVolumeInfo(diskID); err != nil {
		log.Errorf("failed to delete record %s to %s: %+v", diskID, defaultVolumeRecordConfigMap, err)
		return nil, err
	}

	if _, err := deleteDisk(diskID); err != nil {
		log.Errorf("DeleteVolume: delete disk error, err is: %s", err)
		return nil, err
	}

	if err := checkDeleteDiskState(diskID); err != nil {
		log.Errorf("deleteDisk: cdsDisk.DeleteDisk task result failed, err is: %s", err)
		return nil, err
	}

	log.Infof("DeleteVolume: Successfully delete diskID: %s", diskID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	diskID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	nodeID = strings.Replace(nodeID, "\n", "", -1)

	log.Infof("ControllerPublishVolume: volumeId: %s, starting attach diskID: %s to node: %s", req.VolumeId, diskID, nodeID)

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

	if diskInfo.Data.Status == diskAttachingState || diskInfo.Data.Status == diskProcessingState {
		log.Errorf("ControllerPublishVolume: diskID %s is attaching, skip this", diskID)
		return nil, fmt.Errorf("ControllerPublishVolume: diskID %s is attaching, skip this", diskID)
	}

	// check disk count
	if err := c.checkDiskCount(req); err != nil {
		log.Errorf("ControllerPublishVolume: %+v", err)
		return nil, err
	}

	if diskInfo.Data.IsValid == 1 && diskInfo.Data.Mounted == 1 {
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
		if _, err := detachDisk(diskID, nodeID); err != nil {
			log.Errorf("ControllerPublishVolume: detach diskID: %s from nodeID: %s error,  err is: %s", diskID, diskMountedNodeID, err.Error())
			return nil, err
		}

		if err := checkDetachDiskState(diskID); err != nil {
			log.Errorf("ControllerPublishVolume: failed to detach disk %s, err is: %s", diskID, err)
			return nil, err
		}

		log.Warnf("ControllerPublishVolume: detach diskID: %s from nodeID: %s successfully", diskID, diskMountedNodeID)
	} else if diskInfo.Data.IsValid == 0 {
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

	nodeID = strings.Replace(nodeID, "\n", "", -1)

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

	diskInfo, err := getDiskInfo(diskID)
	if err != nil {
		log.Errorf("ControllerUnpublishVolume: findDiskByVolumeID error, err is: %s", err)
		return nil, err
	}

	if diskInfo.Data.Mounted == 0 {
		log.Infof("ControllerUnpublishVolume[%s]: disk has been unmounted by %s, skip this", diskID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if diskInfo.Data.NodeID != nodeID {
		log.Warnf("ControllerUnpublishVolume: diskID: %s had been detached from nodeID: %s, current node: %s", diskID, nodeID, diskInfo.Data.NodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if _, err = detachDisk(diskID, nodeID); err != nil {
		log.Errorf("ControllerUnpublishVolume: create detach task failed, err is: %s", err.Error())
		return nil, err
	}

	if err = checkDetachDiskState(diskID); err != nil {
		log.Errorf("ControllerUnpublishVolume: failed to detach %s, err is: %s", diskID, err)
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

func describeNodeStatus(ctx context.Context, c *ControllerServer, nodeId string) (corev1.ConditionStatus, error) {
	var nodeStatus corev1.ConditionStatus = ""

	csiNodeList, err := c.KubeClient.StorageV1().CSINodes().List(metav1.ListOptions{})
	if err != nil {
		log.Errorf("describeNodeStatus: get csi node list failed, err is: %s", err)
		return "", err
	}

	for i := range csiNodeList.Items {
		csiNode := csiNodeList.Items[i]

		if len(csiNode.Spec.Drivers) == 0 {
			log.Warnf("describeNodeStatus: not found csi driver by %s", nodeId)
			continue
		}

		if csiNode.Spec.Drivers[0].NodeID != nodeId {
			continue
		}

		ownerReferenceList := csiNode.GetOwnerReferences()
		if len(ownerReferenceList) == 0 {
			return "", fmt.Errorf("describeNodeStatus: not found %s ownerReference", nodeId)
		}

		node, err := c.KubeClient.CoreV1().Nodes().Get(ownerReferenceList[0].Name, metav1.GetOptions{})
		if err != nil {
			log.Errorf("describeNodeStatus: get node list failed, err is: %s", err)
			return "", err
		}

		for _, value := range node.Status.Conditions {
			if value.Type == "Ready" {
				log.Infof("describeNodeStatus: nodeStatus is: %s", nodeStatus)

				return value.Status, nil
			}
		}
	}

	return "", nil
}

func buildCreateVolumeResponse(req *csi.CreateVolumeRequest, diskVolume *DiskVolumeArgs) *csi.Volume {
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

	return tmpVol

}

func (c *ControllerServer) saveVolumeInfo(pvName string, volumeInfo *csi.Volume) error {
	updateFunc := func() error {
		cm, err := c.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(defaultVolumeRecordConfigMap, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to fetch %s config map: %+v", defaultVolumeRecordConfigMap, err)
		}

		if cm.Annotations == nil {
			cm.Annotations = make(map[string]string)
		}

		if _, ok := cm.Annotations[pvName]; ok {
			return nil
		}

		volumeInfoStr, err := json.Marshal(volumeInfo)
		if err != nil {
			return fmt.Errorf("failed to marshal %+v: %+v", volumeInfo, err)
		}

		cm.Annotations[pvName] = string(volumeInfoStr)

		if _, err = c.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Update(cm); err != nil {
			return fmt.Errorf("failed tp update %s config map by %s : %+v", defaultVolumeRecordConfigMap, pvName, err)
		}

		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, updateFunc); err != nil {
		return fmt.Errorf("failed tp update %s config map by %s : %+v", defaultVolumeRecordConfigMap, pvName, err)
	}

	return nil
}

func (c *ControllerServer) deleteVolumeInfo(diskId string) error {
	deleteFunc := func() error {
		pvList, err := c.KubeClient.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list pv: %+v", err)
		}

		currentPV := make(map[string]bool)
		for i := range pvList.Items {
			pv := pvList.Items[i]
			currentPV[pv.Name] = true
		}

		cm, err := c.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(defaultVolumeRecordConfigMap, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to fetch %s config map: %+v", defaultVolumeRecordConfigMap, err)
		}

		if cm.Annotations == nil {
			cm.Annotations = make(map[string]string)

			return nil
		}

		for pvName, volumeInfoStr := range cm.Annotations {
			foundPV := false

			if volumeInfoStr == "" {
				continue
			}

			volumeInfo := &csi.Volume{}
			if err = json.Unmarshal([]byte(volumeInfoStr), volumeInfo); err != nil {
				return fmt.Errorf("failed to unmarshal by %s: %+v", volumeInfoStr, err)
			}

			if volumeInfo.VolumeId == diskId {
				foundPV = true
			}

			// maintain residual configuration
			if _, ok := currentPV[pvName]; !ok {
				foundPV = true
			}

			if !foundPV {
				continue
			}

			if _, ok := cm.Annotations[volumeInfo.VolumeId]; ok {
				delete(cm.Annotations, volumeInfo.VolumeId)
			}
			delete(cm.Annotations, pvName)

			if _, err = c.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Update(cm); err != nil {
				return fmt.Errorf("failed to update %s config map by %s : %+v", defaultVolumeRecordConfigMap, diskId, err)
			}
		}

		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, deleteFunc); err != nil {
		return fmt.Errorf("failed tp delete %s config map by %s : %+v", defaultVolumeRecordConfigMap, diskId, err)
	}

	return nil
}

func (c *ControllerServer) checkVolumeInfo(pvName string) (*csi.Volume, bool, error) {
	cm, err := c.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(defaultVolumeRecordConfigMap, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			cmData := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:        defaultVolumeRecordConfigMap,
					Annotations: make(map[string]string),
				},
			}

			createFunc := func() error {
				_, err := c.KubeClient.CoreV1().ConfigMaps(metav1.NamespaceSystem).Create(cmData)
				return err
			}

			if err := retry.RetryOnConflict(retry.DefaultRetry, createFunc); err != nil {
				return nil, false, fmt.Errorf("failed to create config map %s: %+v", defaultVolumeRecordConfigMap, err)
			}

			// not found, new create config map
			return nil, false, nil
		} else {
			return nil, false, fmt.Errorf("failed to found %s config map: %+v", defaultVolumeRecordConfigMap, err)
		}
	}

	if cm.Annotations == nil {
		return nil, false, nil
	}

	if volumeInfoStr, ok := cm.Annotations[pvName]; ok {
		volumeInfo := &csi.Volume{}
		if err = json.Unmarshal([]byte(volumeInfoStr), volumeInfo); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal by %s: %+v", volumeInfoStr, err)
		}

		return volumeInfo, true, nil
	}

	return nil, false, nil
}

// check disk count on node
func (c *ControllerServer) checkDiskCount(req *csi.ControllerPublishVolumeRequest) error {
	c.DiskCountLock.Lock()
	defer c.DiskCountLock.Unlock()

	nodeId := strings.Replace(req.NodeId, "\n", "", -1)

	diskCount, err := getDiskCountByNodeId(req.NodeId)
	if err != nil {
		return fmt.Errorf("volumeId=%s, nodeId=%s, failed to get disk count: %+v", req.VolumeId, nodeId, err)
	}

	if diskCount.Data.DiskCount >= MaxDiskCount {
		msg := fmt.Sprintf("volumeId=%s, nodeId=%s, the maximum number of disks supported by node is %d, The current disk capacity is %d",
			req.VolumeId, nodeId, MaxDiskCount, diskCount.Data.DiskCount)
		log.Errorf(msg)
		return fmt.Errorf(msg)
	}

	return nil
}
