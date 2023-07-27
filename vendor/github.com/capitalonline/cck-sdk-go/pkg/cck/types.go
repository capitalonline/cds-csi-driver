package cck

type BaseResponse struct {
	Code    string `json:"code"`
	Message string `json:"msg"`
}

type DescribeNasInstanceSlice struct {
	ID 			string`json:"id"`
	Name 		string`json:"name"`
	SiteID 		string`json:"site_id"`
	ClusterID 	string`json:"cluster_id"`
	DiskType 	string`json:"disk_type"`
	Iops 		int`json:"iops"`
	Size 		int`json:"size"`
	Status 		string`json:"status"`
	CreateTime	string`json:"create_time"`
	StorageVmId string`json:"storage_vm_id"`
	MountPoint  string`json:"Mount_point"`
	BackupDiskMountPath string`json:"backup_disk_mount_path"`
	StatusStr   string`json:"status_str"`
	Usage 		string`json:"usage"`
	UsageRate	string`json:"usage_rate"`
}

type DescribeNasInstancesResponse struct {
	BaseResponse
	Data struct {
		NasInfo []DescribeNasInstanceSlice `json:"nas_info"`
		Total int `json:"total"`
	} `json:"Data"`
}

type CreateNasResponse struct {
	BaseResponse
	TaskID string `json:"TaskId"`
	Data struct {
		NasID  string `json:"nas_id"`
	} `json:"Data"`
}

type ResizeNasResponse struct {
	BaseResponse
}

type DeleteNasResponse struct {
	BaseResponse
}

type MountNasResponse struct {
	BaseResponse
	TaskID string `json:"TaskId"`
	Data struct {
		NasIP  string `json:"nas_ip"`
	} `json:"Data"`

}

type UnMountNasResponse struct {
	BaseResponse
	TaskID string `json:"TaskId"`
}

type DescribeTaskStatusResponse struct {
	BaseResponse
	Data struct {
		Status string `json:"status"`
	} `json:"Data"`
}




