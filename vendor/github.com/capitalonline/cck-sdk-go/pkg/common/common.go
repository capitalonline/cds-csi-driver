package common

import (
	"os"
)

const (
	defaultApiHost         = "http://cdsapi.capitalonline.net"
	defaultApiHostOversea  = "http://cdsapi-us.capitalonline.net"
	apiHostLiteral         = "API_HOST"
	accessKeyIdLiteral     = "ACCESS_KEY_ID"
	accessKeySecretLiteral = "ACCESS_KEY_SECRET"
	overseaFlag            = "CDS_OVERSEA"
	cckProductType         = "cck"
	version                = "2019-08-08"
	signatureVersion       = "1.0"
	signatureMethod        = "HMAC-SHA1"
	timeStampFormat        = "2006-01-02T15:04:05Z"
)

const (
	// Nas
	ActionDescribeNasInstances = "DescribeNasInstances"
	ActionMountNas = "MountNas"
	ActionUmountNas = "UmountNas"
	ActionCreateNas = "CreateNas"
	ActionResizeNas = "ResizeNas"
	ActionDeleteNas = "DeleteNas"
	ActionTaskStatus = "CheckNasTaskStatus"

	// Disk
	ActionCreateDisk = "CreateDisk"
	ActionAttachDisk = "AttachDisk"
	ActionDetachDisk = "DetachDisk"
	ActionDeleteDisk = "DeleteDisk"
	ActionFindDiskByVolumeID = "FindDiskByVolumeID"
	ActionDeviceNameByVolumeID = "FindDeviceNameByVolumeID"
	ActionDiskTaskStatus = "CheckDiskTaskStatus"

)

var (
	APIHost         string
	AccessKeyID     string
	AccessKeySecret string
)

func init() {
	if APIHost == "" {
		APIHost = os.Getenv(apiHostLiteral)
	}
	if AccessKeyID == "" {
		AccessKeyID = os.Getenv(accessKeyIdLiteral)
	}
	if AccessKeySecret == "" {
		AccessKeySecret = os.Getenv(accessKeySecretLiteral)
	}

	// True is oversea cluster; False is domestic cluster
	if os.Getenv(overseaFlag) == "True" && APIHost == "" {
		APIHost = defaultApiHostOversea
	} else if os.Getenv(overseaFlag) == "False" && APIHost == "" {
		APIHost = defaultApiHost
	}
}

func IsAccessKeySet() bool {
	return AccessKeyID != "" && AccessKeySecret != ""
}
