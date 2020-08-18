package disk

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/wxnacy/wgo/arrays"
	"strconv"
)

func parseDiskVolumeOptions(req *csi.CreateVolumeRequest) (*DiskVolumeArgs, error) {
	var ok bool
	diskVolArgs := &DiskVolumeArgs{}
	volOptions := req.GetParameters()

	// clusterID
	diskVolArgs.ClusterID, ok = volOptions["clusterId"]
	if !ok {
		return nil, fmt.Errorf("clusterId cannot be empty")
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
		// set to default ext4
		diskVolArgs.FsType = DefaultFsTypeExt4
	}
	if diskVolArgs.FsType != "ext4" && diskVolArgs.FsType != "ext3" {
		return nil, fmt.Errorf("illegal required parameter fsType, only support [ext3], [ext4], the input is: %s", diskVolArgs.FsType)
	}

	// disk Type
	diskVolArgs.StorageType, ok = volOptions["storageType"]
	if !ok {
		// no default
		return nil, fmt.Errorf("[storageType] cant be empty")
	}
	if diskVolArgs.StorageType != HighDisk && diskVolArgs.StorageType != SsdDisk {
		return nil, fmt.Errorf("Illegal required parameter type, only support [high_disk], [ssd_disk], input is: %s" + diskVolArgs.StorageType)
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
	if requirement == nil {
		return ""
	}
	for _, topology := range requirement.GetPreferred() {
		zone, exists := topology.GetSegments()[TopologyZoneKey]
		if exists {
			return zone
		}
	}
	for _, topology := range requirement.GetRequisite() {
		zone, exists := topology.GetSegments()[TopologyZoneKey]
		if exists {
			return zone
		}
	}
	return ""
}
