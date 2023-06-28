package ebs_disk

import (
	"context"
	"fmt"
	cdsDisk "github.com/capitalonline/cck-sdk-go/pkg/cck/disk"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"time"

	log "github.com/sirupsen/logrus"
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
	if value, ok := pvcCreatedMap[pvName]; ok {
		log.Warnf("CreateVolume: volume has been created, pvName: %s, volumeContext: %v, return directly", pvName, value.VolumeContext)
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
	if value, ok := diskProcessingMap[pvName]; ok {
		if value == "creating" {
			log.Warnf("CreateVolume: Disk Volume(%s)'s is in creating, please wait", pvName)

			if tmpVol, ok := pvcCreatedMap[pvName]; ok {
				log.Warnf("CreateVolume: Disk Volume(%s)'s diskID: %s creating process finished, return context", pvName, value)
				return &csi.CreateVolumeResponse{Volume: tmpVol}, nil
			}

			return nil, fmt.Errorf("CreateVolume: Disk Volume(%s) is in creating, please wait", pvName)
		}

		log.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)

		return nil, fmt.Errorf("CreateVolume: Disk Volume(%s)'s creating process error", pvName)
	}

	// todo go sdk need edit
	// do request to create ebs disk
	// diskName, diskType, diskSiteID, diskZoneID string, diskSize, diskIops int
	createRes, err := createEbsDisk(pvName, diskVol.StorageType, "", "", int(diskRequestGB), 0)
	if err != nil {
		log.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
		return nil, fmt.Errorf("CreateVolume: createDisk error, err is: %s", err.Error())
	}

	diskID := createRes.Data.VolumeID
	taskID := createRes.TaskID

	// store creating disk
	diskProcessingMap[pvName] = "creating"

	// check create ebs disk event
	// todo go sdk need edit
	err = describeTaskStatus(taskID)
	if err != nil {
		log.Errorf("createDisk: describeTaskStatus task result failed, err is: %s", err.Error())
		diskProcessingMap[pvName] = "error"
		return nil, err
	}

	// clean creating disk
	delete(diskProcessingMap, pvName)

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

	// Step 6: store
	// store diskId and pvName(pvName is equal to pvcName)
	diskIdPvMap[diskID] = pvName

	// store req.Name and csi.Volume
	pvcCreatedMap[pvName] = tmpVol

	log.Infof("CreateVolume: successfully create disk, pvName is: %s, diskID is: %s", pvName, diskID)

	return &csi.CreateVolumeResponse{}, nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	return &csi.DeleteVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (c *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (c *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.VolumeCapabilities,
		},
	}, nil
}

func (c *ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func createEbsDisk(diskName, diskType, diskSiteID, diskZoneID string, diskSize, diskIops int) (*cdsDisk.CreateDiskResponse, error) {

	log.Infof("createDisk: diskName: %s, diskType: %s, diskSiteID: %s, diskZoneID: %s, diskSize: %d, diskIops: %d", diskName, diskType, diskSiteID, diskZoneID, diskSize, diskIops)

	// create disk
	res, err := cdsDisk.CreateDisk(&cdsDisk.CreateDiskArgs{
		Name:     diskName,
		RegionID: diskSiteID,
		DiskType: diskType,
		Size:     diskSize,
		Iops:     diskIops,
		ZoneID:   diskZoneID,
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

		if res.Data.Status == "finish" {
			log.Debugf("task succeed")
			return nil
		} else if res.Data.Status == "doing" {
			log.Debugf("task:%s is running, sleep 10s", taskID)
			time.Sleep(10 * time.Second)
		} else if res.Data.Status == "error" {
			log.Debugf("task error")
			return fmt.Errorf("taskError")
		}
	}

	return fmt.Errorf("task time out, running more than 20 minutes")
}
