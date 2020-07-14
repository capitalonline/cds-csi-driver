package disk

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"strings"
)

func parseDiskVolumeOptions (req *csi.CreateVolumeRequest) (*DiskVolumeArgs, error) {
	var ok bool
	diskVolArgs := &DiskVolumeArgs{}
	volOptions := req.GetParameters()

	// clusterID
	diskVolArgs.ClusterID, ok = volOptions["clusterID"]
	if !ok {
		return nil, fmt.Errorf("clusterID cannot be empty")
	}

	// regionID
	diskVolArgs.RegionID, ok = volOptions["regionID"]
	if !ok {
		return nil, fmt.Errorf("regionID cannot be empty")
	}

	// fstype
	diskVolArgs.FsType, ok = volOptions["fstype"]
	if !ok {
		// set to default ext4
		diskVolArgs.FsType = DefaultFsType
	}
	if diskVolArgs.FsType != "ext4" && diskVolArgs.FsType != "ext3" {
		return nil, fmt.Errorf("illegal required parameter fsType, only support [ext3], [ext4], the input is: %s", diskVolArgs.FsType)
	}

	// disk Type
	diskVolArgs.Type, ok = volOptions["type"]
	if !ok {
		// set to default disk_common
		diskVolArgs.Type = DiskCommon
	}
	if diskVolArgs.Type != DiskHigh && diskVolArgs.Type != DiskCommon {
		return nil, fmt.Errorf("Illegal required parameter type, only support [disk_high], [disk_common], input is: %s" + diskVolArgs.Type)
	}

	// readonly, default false
	value, ok := volOptions["readOnly"]
	if !ok {
		diskVolArgs.ReadOnly = false
	} else {
		value = strings.ToLower(value)
		if value == "yes" || value == "true" || value == "1" {
			diskVolArgs.ReadOnly = true
		} else {
			diskVolArgs.ReadOnly = false
		}
	}

	// encrypted or not
	value, ok = volOptions["encrypted"]
	if !ok {
		diskVolArgs.Encrypted = false
	} else {
		value = strings.ToLower(value)
		if value == "yes" || value == "true" || value == "1" {
			diskVolArgs.Encrypted = true
		} else {
			diskVolArgs.Encrypted = false
		}
	}

	return diskVolArgs, nil
}