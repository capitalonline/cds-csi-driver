package ebs

type Response struct {
	Code      string      `json:"Code"`
	Message   string      `json:"Msg"`
	Data      interface{} `json:"Data,omitempty"`
	RequestId string      `json:"RequestId"`
	OpenapiPage
}

type OpenapiPage struct {
	TotalCount int `json:"TotalCount,omitempty"`
	PageIndex  int `json:"PageIndex,omitempty"`
	PageSize   int `json:"PageSize,omitempty"`
}

type AttachDiskArgs struct {
	DiskIds             []string `json:"DiskIds"`
	InstanceId          string   `json:"InstanceId"`
	ReleaseWithInstance int      `json:"ReleaseWithInstance"`
}
type AttachDiskResponse struct {
	Response
	Data struct {
		EventId string `json:"EventId"`
	} `json:"Data"`
}

type DetachDiskArgs struct {
	DiskIds []string `json:"DiskIds"`
}
type DetachDiskResponse struct {
	Response
	Data struct {
		EventId string `json:"EventId"`
	} `json:"Data"`
}

type DeleteDiskArgs struct {
	DiskIds []string `json:"DiskIds"`
}
type DeleteDiskResponse struct {
	Response
	Data struct {
		EventId string `json:"EventId"`
	} `json:"Data"`
}

type FindDiskByVolumeIDArgs struct {
	DiskId string `json:"DiskId"`
}

type DiskInfo struct {
	NodeID   string `json:"node_id"`
	Status   string `json:"status"`
	Uuid     string `json:"disk_uuid"`
	IsFormat int    `json:"is_format"`
}
type FindDiskByVolumeIDResponse struct {
	Response
	Data struct {
		DiskInfo FindDiskByVolumeIDDiskInfo `json:"DiskInfo"`
	} `json:"Data"`
}

type FindDiskByVolumeIDDiskInfo struct {
	DiskId              string `json:"DiskId"`
	DiskName            string `json:"DiskName"`
	Size                int    `json:"Size"`
	EcsId               string `json:"EcsId"`
	EcsName             string `json:"EcsName"`
	BillingMethod       string `json:"BillingMethod"`
	ReleaseWithInstance int    `json:"ReleaseWithInstance"`
	RegionCode          string `json:"RegionCode"`
	AvailableZoneCode   string `json:"AvailableZoneCode"`
	Status              string `json:"Status"`
	StatusDisplay       string `json:"StatusDisplay"`
	DiskFeature         string `json:"DiskFeature"`
	Property            string `json:"Property"`
	Order               int    `json:"Order"`
}

type FindDeviceNameByDiskIdArgs struct {
	DiskId string `json:"DiskId"`
}
type FindDeviceNameByDiskIdResponse struct {
	Response
	Data struct {
		DiskName string `json:"DiskName"`
	} `json:"Data"`
}

type CreateEbsReq struct {
	AvailableZoneCode string `json:"AvailableZoneCode"`
	DiskName          string `json:"DiskName"`
	DiskFeature       string `json:"DiskFeature"`
	Size              int    `json:"Size"`
	Number            *int   `json:"Number,omitempty"`
	BillingMethod     string `json:"BillingMethod,omitempty"`
}

type CreateEbsResp struct {
	Response
	Data CreateEbsRespData
}

type CreateEbsRespData struct {
	EventId   string   `json:"EventId"`
	DiskIdSet []string `json:"DiskIdSet"`
}

type DescribeTaskStatusResponse struct {
	Response
	Data struct {
		EventStatus string `json:"EventStatus"`
	} `json:"Data"`
}

type UpdateBlockFormatFlagArgs struct {
	BlockID  string `json:"block_id"`
	IsFormat int    `json:"is_format"`
}

type UpdateBlockFormatFlagResponse struct {
	Response
}

type ExtendDiskArgs struct {
	DiskId       string `json:"DiskId"`
	ExtendedSize int    `json:"ExtendedSize"`
}

type ExtendDiskResponse struct {
	Response
	Data ExtendDiskData `json:"Data"`
}

type ExtendDiskData struct {
	EventId string `json:"EventId"`
}

type DescribeDiskQuotaRequest struct {
	AvailableZoneCode string `json:"AvailableZoneCode"`
}

type DescribeDiskQuotaResponse struct {
	Response
	Data DescribeDiskQuotaResponseData `json:"Data"`
}

type DescribeDiskQuotaResponseData struct {
	QuotaList []Quota `json:"QuotaList"`
}

type Quota struct {
	TotalQuota  int    `json:"TotalQuota"`
	UsedQuota   int    `json:"UsedQuota"`
	FreeQuota   int    `json:"FreeQuota"`
	DiskFeature string `json:"DiskFeature"`
}

type DescribeInstanceRequest struct {
	EcsId string `json:"EcsId"`
}

type DescribeInstanceResponse struct {
	Response
	Data DescribeInstanceData `json:"Data"`
}

type DescribeInstanceData struct {
	EcsId   string                   `json:"EcsId"`
	EcsName string                   `json:"EcsName"`
	Status  string                   `json:"Status"`
	Disk    DescribeInstanceDataDisk `json:"Disk"`
}

type DescribeInstanceDataDisk struct {
	SystemDiskConf DescribeInstanceDataDiskInfo   `json:"SystemDiskConf"`
	DataDiskConf   []DescribeInstanceDataDiskInfo `json:"DataDiskConf"`
}

type DescribeInstanceDataDiskInfo struct {
	IsFollowDelete      int    `json:"IsFollowDelete"`
	DiskType            string `json:"DiskType"`
	Name                string `json:"Name"`
	Size                int    `json:"Size"`
	EcsGoodsId          string `json:"EcsGoodsId"`
	DiskIops            int    `json:"DiskIops"`
	BandMbps            int    `json:"BandMbps"`
	ReleaseWithInstance int    `json:"ReleaseWithInstance"`
	EbsGoodsId          string `json:"EbsGoodsId"`
	Unit                string `json:"Unit"`
	DiskFeature         string `json:"DiskFeature"`
	DiskName            string `json:"DiskName"`
	DiskId              string `json:"DiskId"`
}
