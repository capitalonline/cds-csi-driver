package block

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"github.com/wxnacy/wgo/arrays"
	"strconv"
)

func parseBlockVolumeOptions(req *csi.CreateVolumeRequest) (*BlockVolumeArgs, error) {
	var ok bool
	blockVolArgs := &BlockVolumeArgs{}
	volOptions := req.GetParameters()

	// regionID
	blockVolArgs.SiteID, ok = volOptions["siteId"]
	if !ok {
		return nil, fmt.Errorf("regionId cannot be empty")
	}

	// zoneID
	blockVolArgs.ZoneID, ok = volOptions["zoneId"]
	if !ok {
		// topology aware feature to get zoneid
		blockVolArgs.ZoneID = pickZone(req.GetAccessibilityRequirements())
		if blockVolArgs.ZoneID == "" {
			return nil, fmt.Errorf("zoneId cannot be empty, please input [zoneId] in parameters or add [allowedTopologies.matchLabelExpressions.key and values] in SC")
		}
	}

	// fstype
	blockVolArgs.FsType, ok = volOptions["fsType"]
	if !ok {
		// set to default xfs
		blockVolArgs.FsType = DefaultFsTypeXfs
	}
	if blockVolArgs.FsType != "xfs" && blockVolArgs.FsType != "ext4" && blockVolArgs.FsType != "ext3" {
		return nil, fmt.Errorf("illegal required parameter fsType, only support [xfs|ext3|ext4], the input is: %s", blockVolArgs.FsType)
	}

	// disk Type
	blockVolArgs.StorageType, ok = volOptions["storageType"]
	if !ok {
		// no default
		return nil, fmt.Errorf("[storageType] cant be empty")
	}
	if blockVolArgs.StorageType != HighDisk && blockVolArgs.StorageType != SsdDisk {
		return nil, fmt.Errorf("Illegal required parameter type, only support [high_disk], [ssd_disk], input is: %s" + blockVolArgs.StorageType)
	}

	// disk iops
	blockVolArgs.Iops, ok = volOptions["iops"]
	if !ok {
		return nil, fmt.Errorf("[iops] cant be empty")
	}
	iopsInt64, _ := strconv.ParseInt(blockVolArgs.Iops, 10, 64)
	if arrays.ContainsInt(IopsArrayInt64, iopsInt64) == -1 {
		return nil, fmt.Errorf("[iops] should be in one of [3000|5000|7500|10000], but input is: %s", blockVolArgs.Iops)
	}

	return blockVolArgs, nil
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
		// logrus.Infof("pickZone: exists is: %t", exists)
		// logrus.Infof("pickZone: zone is: %+v", zone)
		if exists {
			return zone
		}
	}
	logrus.Infof("pickZone: requirement.GetRequisite() is: %+v", requirement.GetRequisite())
	for _, topology := range requirement.GetRequisite() {
		zone, exists := topology.GetSegments()[TopologyZoneKey]
		// logrus.Infof("pickZone: exists is: %t", exists)
		// logrus.Infof("pickZone: zone is: %+v", zone)
		if exists {
			return zone
		}
	}
	logrus.Infof("pickZone: return null")
	return ""
}

