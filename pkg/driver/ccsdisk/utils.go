package ccsdisk

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"github.com/wxnacy/wgo/arrays"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"os"
	"strconv"
)

const (
	scsiHostPath = "/sys/class/scsi_host/"
)

func parseDiskVolumeOptions(req *csi.CreateVolumeRequest) (*DiskVolumeArgs, error) {
	var ok bool
	diskVolArgs := &DiskVolumeArgs{}
	volOptions := req.GetParameters()

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "pv Name (req.Name) cannot be empty")
	}
	if req.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "req.VolumeCapabilities cannot be empty")
	}
	if req.GetCapacityRange() == nil {
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: Capacity cannot be empty", req.Name)
	}

	if IsBlockSingleWriter(req.VolumeCapabilities) {
		return nil, status.Error(codes.InvalidArgument, "single node access modes are only supported on block")
	}

	diskRequestGB := req.GetCapacityRange().GetRequiredBytes() / (1024 * 1024 * 1024)
	if diskRequestGB < MinDiskSize || diskRequestGB > MaxDiskSize {
		msg := fmt.Sprintf("reqName: %s, disk capacity must be between %dG and %dG, request size: %d", req.GetName(), MinDiskSize, MaxDiskSize, diskRequestGB)
		return nil, status.Errorf(codes.InvalidArgument, msg)
	}

	// regionID
	diskVolArgs.SiteID, ok = volOptions["siteId"]
	if !ok {
		return nil, fmt.Errorf("regionId cannot be empty")
	}

	// zoneID
	diskVolArgs.ZoneID, ok = volOptions["zoneId"]
	if !ok {
		// topology aware feature to get zoneid
		diskVolArgs.ZoneID = pickZone(req.GetAccessibilityRequirements())
		if diskVolArgs.ZoneID == "" {
			return nil, fmt.Errorf("zoneId cannot be empty, please input [zoneId] in parameters or add [allowedTopologies.matchLabelExpressions.key and values] in SC")
		}
	}

	// fstype
	diskVolArgs.FsType, ok = volOptions["fsType"]
	if !ok {
		// set to default xfs
		diskVolArgs.FsType = DefaultFsTypeXfs
	}
	if diskVolArgs.FsType != "xfs" && diskVolArgs.FsType != "ext4" && diskVolArgs.FsType != "ext3" {
		return nil, fmt.Errorf("illegal required parameter fsType, only support [xfs|ext3|ext4], the input is: %s", diskVolArgs.FsType)
	}

	// disk Type
	diskVolArgs.StorageType, ok = volOptions["storageType"]
	if !ok {
		// no default
		return nil, fmt.Errorf("[storageType] cant be empty")
	}
	if diskVolArgs.StorageType != HighDisk && diskVolArgs.StorageType != SsdDisk {
		return nil, fmt.Errorf("required parameter type, only support [high_disk|ssd_disk], input is: %s", diskVolArgs.StorageType)
	}

	// disk iops
	diskVolArgs.Iops, ok = volOptions["iops"]
	if !ok {
		return nil, fmt.Errorf("[iops] cant be empty")
	}
	iopsInt64, _ := strconv.ParseInt(diskVolArgs.Iops, 10, 64)
	if arrays.ContainsInt(IopsArrayInt64, iopsInt64) == -1 {
		return nil, fmt.Errorf("[iops] should be in one of [3000|5000|7500|10000], but input is: %s", diskVolArgs.Iops)
	}

	return diskVolArgs, nil
}

// pickZone selects 1 zone given topology requirement.
// if not found, empty string is returned.
func pickZone(requirement *csi.TopologyRequirement) string {
	logrus.Infof("pickZone: requirement is: %+v", requirement)
	if requirement == nil {
		return ""
	}
	logrus.Infof("pickZone: requirement.GetPreferred() is: %+v", requirement.GetPreferred())

	for _, topology := range requirement.GetPreferred() {
		zone, exists := topology.GetSegments()[TopologyZoneKey]
		if exists {
			return zone
		}
	}
	logrus.Infof("pickZone: requirement.GetRequisite() is: %+v", requirement.GetRequisite())

	for _, topology := range requirement.GetRequisite() {
		zone, exists := topology.GetSegments()[TopologyZoneKey]
		if exists {
			return zone
		}
	}
	logrus.Infof("pickZone: return null")

	return ""
}

func scanNodeDiskList() error {
	flushFileFunc := func(filePath string) error {
		f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			return fmt.Errorf("failed to open %s: %+v", filePath, err)
		}
		defer f.Close()

		if _, err = f.WriteString("- - -"); err != nil {
			return fmt.Errorf("failed to write %s file: %+v", filePath, err)
		}

		return nil
	}

	scsiHostList, _ := ioutil.ReadDir(scsiHostPath)
	for _, f := range scsiHostList {
		if err := flushFileFunc(fmt.Sprintf("%s%s/scan", scsiHostPath, f.Name())); err != nil {
			return err
		}
	}

	return nil
}

// IsBlockSingleWriter validates the volume capability slice against the access modes and access type.
// if the capability is of multi write the first return value will be set to true and if the request
// is of type block, the second return value will be set to true.
func IsBlockSingleWriter(caps []*csi.VolumeCapability) bool {
	// multiWriter has been set and returned after validating multi writer caps regardless of
	// single or multi node access mode. The caps check is agnostic to whether it is a filesystem or block
	// mode volume.
	singleWriter := false

	for _, cap := range caps {
		if cap.AccessMode != nil {
			switch cap.AccessMode.Mode { //nolint:exhaustive // only check what we want
			case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
				singleWriter = true
			}
		}
	}

	return singleWriter
}
