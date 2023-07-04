package eks

import (
	"encoding/json"
	cdshttp "github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/http"
)

// CreateBlockRequest 创建块存储
type CreateBlockRequest struct {
	*cdshttp.BaseRequest
	DiskInfo CreateBlockRequestDiskInfo `json:"DiskInfo"`
	DiskName string                     `json:"DiskName"`
	AzId     string                     `json:"AzId"`
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
	Code string                   `json:"Code"`
	Msg  string                   `json:"Msg"`
	Data *CreateBlockResponseData `json:"Data"`
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
	Code string                   `json:"Code"`
	Msg  string                   `json:"Msg"`
	Data *DeleteBlockResponseData `json:"Data"`
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
	BlockId         string `json:"BlockId"`
	EbsId           string `json:"EbsId"`
	NodeId          string `json:"NodeId"`
	NodeName        string `json:"NodeName"`
	AvailableZoneId string `json:"AvailableZoneId"`
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
	Code string                   `json:"Code"`
	Msg  string                   `json:"Msg"`
	Data *AttachBlockResponseData `json:"Data"`
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
	BlockId         string
	EbsId           string
	NodeId          string
	AvailableZoneId string
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
	Code string                   `json:"Code"`
	Msg  string                   `json:"Msg"`
	Data *DetachBlockResponseData `json:"Data"`
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
