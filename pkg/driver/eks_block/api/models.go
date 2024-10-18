package eks

import (
	"encoding/json"
	cdshttp "github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/http"
)

// CreateBlockRequest 创建块存储
type CreateBlockRequest struct {
	*cdshttp.BaseRequest
	//DiskInfo CreateBlockRequestDiskInfo `json:"DiskInfo"`
	DiskFeature       string `json:"DiskFeature"`
	DiskSize          int    `json:"DiskSize"`
	DiskName          string `json:"DiskName"`
	FsType            string `json:"FsType"`
	CreateSource      string `json:"CreateSource"`
	SubjectId         string `json:"SubjectId"`
	AvailableZoneCode string `json:"AvailableZoneCode"`
}

// CreateBlockRequestDiskInfo 块存储磁盘信息
type CreateBlockRequestDiskInfo struct {
	DiskFeature string `json:"DiskFeature"`
	DiskSize    int    `json:"DiskSize"`
}

func (req *CreateBlockRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *CreateBlockRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type CreateBlockResponse struct {
	*cdshttp.BaseResponse
	Code      string                   `json:"Code"`
	Msg       string                   `json:"Msg"`
	Data      *CreateBlockResponseData `json:"Data"`
	RequestId string                   `json:"RequestId"`
}

func (resp *CreateBlockResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *CreateBlockResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type CreateBlockResponseData struct {
	BlockId string `json:"BlockId"`
	TaskId  string `json:"TaskId"`
}

type DeleteBlockRequest struct {
	*cdshttp.BaseRequest
	BlockId string `json:"BlockId"`
}

func (req *DeleteBlockRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *DeleteBlockRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type DeleteBlockResponse struct {
	*cdshttp.BaseResponse
	Code      string                   `json:"Code"`
	Msg       string                   `json:"Msg"`
	Data      *DeleteBlockResponseData `json:"Data"`
	RequestId string                   `json:"RequestId"`
}

type DeleteBlockResponseData struct {
	TaskId  string `json:"TaskId"`
	BlockId string `json:"BlockId"`
}

func (resp *DeleteBlockResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *DeleteBlockResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type AttachBlockRequest struct {
	*cdshttp.BaseRequest
	BlockId string `json:"BlockId"`
	NodeId  string `json:"NodeId"`
}

func (req *AttachBlockRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *AttachBlockRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type AttachBlockResponse struct {
	*cdshttp.BaseResponse
	Code      string                   `json:"Code"`
	Msg       string                   `json:"Msg"`
	Data      *AttachBlockResponseData `json:"Data"`
	RequestId string                   `json:"RequestId"`
}

type AttachBlockResponseData struct {
	TaskId  string `json:"TaskId"`
	BlockId string `json:"BlockId"`
}

func (resp *AttachBlockResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *AttachBlockResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type DetachBlockRequest struct {
	*cdshttp.BaseRequest
	BlockId string
}

func (req *DetachBlockRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *DetachBlockRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type DetachBlockResponse struct {
	*cdshttp.BaseResponse
	Code      string                   `json:"Code"`
	Msg       string                   `json:"Msg"`
	Data      *DetachBlockResponseData `json:"Data"`
	RequestId string                   `json:"RequestId"`
}

type DetachBlockResponseData struct {
	TaskId  string `json:"TaskId"`
	BlockId string `json:"BlockId"`
}

func (resp *DetachBlockResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *DetachBlockResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type TaskStatusRequest struct {
	*cdshttp.BaseRequest
	TaskId string `json:"TaskId"`
}

func (req *TaskStatusRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *TaskStatusRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type TaskStatusResponse struct {
	*cdshttp.BaseResponse
	Code      string                 `json:"Code"`
	Msg       string                 `json:"Msg"`
	Data      TaskStatusResponseData `json:"Data"`
	RequestId string                 `json:"RequestId"`
}

type TaskStatusResponseData struct {
	TaskId     string `json:"TaskId"`
	TaskMsg    string `json:"TaskMsg"`
	TaskStatus string `json:"TaskStatus"`
}

func (resp *TaskStatusResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *TaskStatusResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type DescribeBlockLimitRequest struct {
	*cdshttp.BaseRequest
	AvailableZoneCodes []string `json:"AvailableZoneCodes"`
}

func (req *DescribeBlockLimitRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *DescribeBlockLimitRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type DescribeBlockLimitResponse struct {
	*cdshttp.BaseResponse
	Code      string                         `json:"Code"`
	Msg       string                         `json:"Msg"`
	Data      DescribeBlockLimitResponseData `json:"Data"`
	RequestId string                         `json:"RequestId"`
}

type DescribeBlockLimitResponseData struct {
	MaxRestVolume   int                                            `json:"MaxRestVolume"`
	TotalRestVolume int                                            `json:"TotalRestVolume"`
	RestVolumeList  []DescribeBlockLimitResponseDataRestVolumeList `json:"RestVolumeList"`
}

type DescribeBlockLimitResponseDataRestVolumeList struct {
	AvailableZoneCode string `json:"AvailableZoneCode"`
	AvailableZoneId   string `json:"AvailableZoneId"`
	RestVolume        int    `json:"RestVolume"`
}

func (resp *DescribeBlockLimitResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *DescribeBlockLimitResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type DescribeBlockInfoRequest struct {
	*cdshttp.BaseRequest
	BlockId   string `json:"BlockId,omitempty"`
	BlockName string `json:"BlockName,omitempty"`
}

func (req *DescribeBlockInfoRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *DescribeBlockInfoRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type DescribeBlockInfoResponse struct {
	*cdshttp.BaseResponse
	Code string                        `json:"Code"`
	Msg  string                        `json:"Msg"`
	Data DescribeBlockInfoResponseData `json:"Data"`
}

type DescribeBlockInfoResponseData struct {
	BlockId           string `json:"BlockId"`
	EbsId             string `json:"EbsId"`
	BlockName         string `json:"BlockName"`
	DiskSize          int    `json:"DiskSize"`
	NodeId            string `json:"NodeId"`
	NodeName          string `json:"NodeName"`
	Status            string `json:"Status"`
	StatusStr         string `json:"StatusStr"`
	Order             int    `json:"Order"`
	IsFormat          int    `json:"IsFormat"`
	FsType            string `json:"FsType"`
	DiskFeature       string `json:"DiskFeature"`
	AvailableZoneCode string `json:"AvailableZoneCode"`
}

func (resp *DescribeBlockInfoResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *DescribeBlockInfoResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type UpdateBlockFormatRequest struct {
	*cdshttp.BaseRequest
	BlockId  string `json:"BlockId"`
	IsFormat int    `json:"IsFormat"`
}

func (req *UpdateBlockFormatRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *UpdateBlockFormatRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type UpdateBlockFormatResponse struct {
	*cdshttp.BaseResponse
	Code      string      `json:"Code"`
	Msg       string      `json:"Msg"`
	Data      interface{} `json:"Data"`
	RequestId string      `json:"RequestId"`
}

func (resp *UpdateBlockFormatResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *UpdateBlockFormatResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type DescribeNodeMountNumRequest struct {
	*cdshttp.BaseRequest
	NodeId string `json:"NodeId"`
}

func (req *DescribeNodeMountNumRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *DescribeNodeMountNumRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type DescribeNodeMountNumResponse struct {
	*cdshttp.BaseResponse
	Code      string                           `json:"Code"`
	Msg       string                           `json:"Msg"`
	Data      DescribeNodeMountNumResponseData `json:"Data"`
	RequestId string                           `json:"RequestId"`
}

type DescribeNodeMountNumResponseData struct {
	BlockNum   int    `json:"BlockNum"`
	NodeStatus string `json:"NodeStatus"`
	StatusStr  string `json:"StatusStr"`
}

func (resp *DescribeNodeMountNumResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *DescribeNodeMountNumResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}

type ExpandBlockSizeRequest struct {
	*cdshttp.BaseRequest
	BlockId     string `json:"BlockId"`
	ExpandSize  int64  `json:"ExpandSize"`
	MountNodeId string `json:"MountNodeId"`
}

func (req *ExpandBlockSizeRequest) ToJsonString() string {
	b, _ := json.Marshal(req)
	return string(b)
}

func (req *ExpandBlockSizeRequest) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &req)
}

type ExpandBlockSizeResponse struct {
	*cdshttp.BaseResponse
	Code      string                      `json:"Code"`
	Msg       string                      `json:"Msg"`
	Data      ExpandBlockSizeResponseData `json:"Data"`
	RequestId string                      `json:"RequestId"`
}

type ExpandBlockSizeResponseData struct {
	TaskId string `json:"TaskId"`
}

func (resp *ExpandBlockSizeResponse) ToJsonString() string {
	b, _ := json.Marshal(resp)
	return string(b)
}

func (resp *ExpandBlockSizeResponse) FromJsonString(s string) error {
	return json.Unmarshal([]byte(s), &resp)
}
