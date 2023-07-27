package disk

type Response struct {
	Code     string `json:"Code"`
	Message  string `json:"Message"`
	CodeDesc string `json:"codeDesc,omitempty"`
}

type AttachDiskArgs struct {
	VolumeID string `json:"block_id"`
	NodeID   string `json:"node_id"`
}
type AttachDiskResponse struct {
	Response
	TaskID string `json:"TaskId"`
}

type DetachDiskArgs struct {
	VolumeID string `json:"block_id"`
}
type DetachDiskResponse struct {
	Response
	TaskID string `json:"TaskId"`
}

type DeleteDiskArgs struct {
	VolumeID string `json:"block_id"`
}
type DeleteDiskResponse struct {
	Response
	TaskID string `json:"TaskId"`
}

type FindDiskByVolumeIDArgs struct {
	VolumeID string `json:"block_id"`
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
		DiskSlice []DiskInfo `json:"block_info"`
	} `json:"data"`
}

type FindDeviceNameByVolumeIDArgs struct {
	VolumeID string `json:"disk_id"`
}
type FindDeviceNameByVolumeIDResponse struct {
	Response
	Data struct {
		DeviceName string `json:"device_name"`
	} `json:"Data"`
}

type CreateDiskArgs struct {
	Name     string `json:"name"`
	RegionID string `json:"site_id"`
	DiskType string `json:"disk_type"`
	Size     int    `json:"size"`
	Iops     int    `json:"iops"`
	ZoneID   string `json:"vc_cluster_name"`
}
type CreateDiskResponse struct {
	Response
	Data struct {
		VolumeID string `json:"block_id"`
	} `json:"Data"`
	TaskID string `json:"TaskId"`
}

type DescribeTaskStatusResponse struct {
	Response
	Data struct {
		Status string `json:"status"`
	} `json:"Data"`
}

type UpdateBlockFormatFlagArgs struct {
	BlockID  string `json:"block_id"`
	IsFormat int    `json:"is_format"`
}

type UpdateBlockFormatFlagResponse struct {
	Response
}
