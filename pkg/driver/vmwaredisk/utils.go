package vmwaredisk

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"github.com/wxnacy/wgo/arrays"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
	"strings"

	utilexec "k8s.io/utils/exec"
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

func getDiskFormat(exec utilexec.Interface, disk string) (string, error) {
	args := []string{"-p", "-s", "TYPE", "-s", "PTTYPE", "-o", "export", disk}
	logrus.Infof("Attempting to determine if disk %q is formatted using blkid with args: (%v)", disk, args)
	dataOut, err := exec.Command("blkid", args...).CombinedOutput()
	output := string(dataOut)
	logrus.Infof("Output: %q", output)

	if err != nil {
		if exit, ok := err.(utilexec.ExitError); ok {
			if exit.ExitStatus() == 2 {
				// Disk device is unformatted.
				// For `blkid`, if the specified token (TYPE/PTTYPE, etc) was
				// not found, or no (specified) devices could be identified, an
				// exit code of 2 is returned.
				return "", nil
			}
		}
		logrus.Errorf("Could not determine if disk %q is formatted (%v)", disk, err)
		return "", err
	}

	var fstype, pttype string

	lines := strings.Split(output, "\n")
	for _, l := range lines {
		if len(l) <= 0 {
			// Ignore empty line.
			continue
		}
		cs := strings.Split(l, "=")
		if len(cs) != 2 {
			return "", fmt.Errorf("blkid returns invalid output: %s", output)
		}
		// TYPE is filesystem type, and PTTYPE is partition table type, according
		// to https://www.kernel.org/pub/linux/utils/util-linux/v2.21/libblkid-docs/.
		if cs[0] == "TYPE" {
			fstype = cs[1]
		} else if cs[0] == "PTTYPE" {
			pttype = cs[1]
		}
	}

	if len(pttype) > 0 {
		logrus.Infof("Disk %s detected partition table type: %s", disk, pttype)
		// Returns a special non-empty string as filesystem type, then kubelet
		// will not format it.
		return "unknown data, probably partitions", nil
	}

	return fstype, nil
}
