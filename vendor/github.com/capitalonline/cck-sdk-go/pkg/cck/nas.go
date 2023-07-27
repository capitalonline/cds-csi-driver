package cck

import (
	"encoding/json"
	"fmt"
	"github.com/capitalonline/cck-sdk-go/pkg/common"
	"io/ioutil"
	"net/http"
)

func DescribeNasInstances(nasID, nasName, nasSiteID, clusterID string, usageFlag, pageNumber, pageSize int) (*DescribeNasInstancesResponse, error) {
	payload := struct {
		NasID      string `json:"NasId,omitempty"`
		NasName    string `json:"NasName,omitempty"`
		SiteID     string `json:"SiteId,omitempty"`
		ClusterID  string `json:"ClusterId,omitempty"`
		UsageFlag  int    `json:"UsageFlag,omitempty"`
		PageNumber int    `json:"PageNumber,omitempty"`
		PageSize   int    `json:"PageSize,omitempty"`
	}{
		nasID,
		nasName,
		nasSiteID,
		clusterID,
		usageFlag,
		pageNumber,
		pageSize,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionDescribeNasInstances, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DescribeNasInstancesResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func CreateNas(nasSiteID, nasName, diskType string, diskSize, isNotShared int) (*CreateNasResponse, error) {
	payload := struct {
		NasSiteID    string `json:"SiteId"`
		NasName      string `json:"NasName"`
		DiskType     string `json:"DiskType"`
		DiskSize     int    `json:"DiskSize"`
		UnsharedFlag int    `json:"UnsharedFlag"`
	}{
		nasSiteID,
		nasName,
		diskType,
		diskSize,
		isNotShared,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionCreateNas, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &CreateNasResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func ResizeNas(nasSiteID, nasID string, diskSize int) (*ResizeNasResponse, error) {
	payload := struct {
		NasSiteID string `json:"SiteId"`
		NasID     string `json:"NasId"`
		DiskSize  int    `json:"DiskSize"`
	}{
		nasSiteID,
		nasID,
		diskSize,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionResizeNas, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &ResizeNasResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func DeleteNas(nasID string) (*DeleteNasResponse, error) {
	payload := struct {
		NasID string `json:"NasId"`
	}{
		nasID,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionDeleteNas, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DeleteNasResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func MountNas(nasID, clusterID string) (*MountNasResponse, error) {
	payload := struct {
		NasId     string `json:"NasId"`
		ClusterId string `json:"ClusterId"`
	}{
		nasID,
		clusterID,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionMountNas, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &MountNasResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func UnMountNas(nasID string) (*UnMountNasResponse, error) {
	payload := struct {
		NasID string `json:"NasId"`
	}{
		nasID,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionUmountNas, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &UnMountNasResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func DescribeTaskStatus(TaskID string) (*DescribeTaskStatusResponse, error) {
	payload := struct {
		TaskID string `json:"task_id"`
	}{
		TaskID,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionTaskStatus, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DescribeTaskStatusResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}

func DescribeNasUsage(ClusterId, NasIP string) (*DescribeNasInstancesResponse, error) {
	payload := struct {
		MountPoint string`json:"MountPoint"`
		ClusterID  string`json:"ClusterId"`
	}{
		NasIP,
		ClusterId,
	}
	body, err := common.MarshalJsonToIOReader(payload)
	if err != nil {
		return nil, err
	}
	req, err := common.NewCCKRequest(common.ActionDescribeNasInstances, http.MethodPost, nil, body)
	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DescribeNasInstancesResponse{}
	err = json.Unmarshal(content, res)
	return res, err
}
