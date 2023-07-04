package consts

import (
	"k8s.io/klog"
	"os"
)

var (
	AccessKeyID     string
	AccessKeySecret string
	Region          string
	Az              string
	ClusterId       string
	ApiHost         string
)

func init() {
	ak := os.Getenv(EnvAccessKeyID)
	if ak == "" {
		klog.Infoln("未获取到ak")
		//panic("env CDS_ACCESS_KEY_ID must be set")
	}
	AccessKeyID = ak
	sk := os.Getenv(EnvAccessKeySecret)
	if sk == "" {
		klog.Infoln("未获取sk")
		//panic("env CDS_ACCESS_KEY_SECRET must be set")
	}
	AccessKeySecret = sk

	clusterId := os.Getenv(EnvClusterId)
	if clusterId == "" {
		klog.Infoln("未获取到集群id")
		//panic("env CDS_CLUSTER_ID must be set")
	}
	region := os.Getenv(EnvRegion)
	if region != "" {
		Region = region
	}
	az := os.Getenv(EnvAz)
	if az != "" {
		Az = az
	}
	apiHost := os.Getenv(EnvAPIHost)
	if apiHost != "" {
		ApiHost = apiHost
	} else {
		ApiHost = ApiHostAddress
	}
}
