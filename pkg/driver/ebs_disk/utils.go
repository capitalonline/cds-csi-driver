package ebs_disk

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
)

func parseDiskVolumeOptions(req *csi.CreateVolumeRequest) (*EbsVolumeArgs, error) {
	var (
		ok          bool
		diskVolArgs = new(EbsVolumeArgs)
	)

	volOptions := req.GetParameters()

	// set region and zone
	diskVolArgs.Region, ok = volOptions["region"]
	if !ok {
		return nil, fmt.Errorf("region cannot be empty")
	}
	diskVolArgs.Zone, ok = volOptions["zone"]
	if !ok {
		return nil, fmt.Errorf("zone cannot be empty")
	}

	// fsType
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
	if diskVolArgs.StorageType != ebsSsdDisk {
		return nil, fmt.Errorf("illegal required parameter type, only support [ssd], input is: %s", diskVolArgs.StorageType)
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
