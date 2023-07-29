package vmwaredisk

type Response struct {
	Code     string `json:"Code"`
	Message  string `json:"Message"`
	CodeDesc string `json:"codeDesc,omitempty"`
}

type AttachDiskArgs struct {
	VolumeID string `json:"UUID"`
	NodeID   string `json:"VM_ID"`
}
type AttachDiskResponse struct {
	Response
	TaskID string `json:"TaskId"`
}

type DetachDiskArgs struct {
	VolumeID string `json:"UUID"`
	NodeID   string `json:"VM_ID"`
}
type DetachDiskResponse struct {
	Response
	TaskID string `json:"TaskId"`
}

type DeleteDiskArgs struct {
	VolumeID string `json:"UUID"`
}
type DeleteDiskResponse struct {
	Response
	VolumeID string `json:"disk_id"`
}

type DiskInfoArgs struct {
	VolumeID string `json:"UUID"`
}

type DiskInfoResponse struct {
	Response
	Data struct {
		NodeID   string `json:"vm_id"`
		Status   string `json:"status"`
		VolumeId string `json:"disk_id"`
		Mounted  int    `json:"is_load"`
		IsValid  int    `json:"is_valid"`
	} `json:"data"`
}

type CreateDiskArgs struct {
	RegionID    string `json:"SiteId"`
	DiskType    string `json:"DiskType"`
	Size        int    `json:"Size"`
	Iops        int    `json:"IOPS"`
	ClusterName string `json:"ClusterName"`
}
type CreateDiskResponse struct {
	Response
	Data struct {
		VolumeID string `json:"uuid"`
	} `json:"Data"`
	TaskID string `json:"TaskId"`
}

type DescribeTaskStatusResponse struct {
	Response
	Data struct {
		Status string `json:"status"`
	} `json:"Data"`
}
