package vmwaredisk

import (
	"encoding/json"
	"fmt"
	"github.com/capitalonline/cck-sdk-go/pkg/common"
	"io/ioutil"
	"net/http"
)

const (
	ActionCreateDisk = "CsiCreateDisk"
	ActionDeleteDisk = "CsiDeleteDisk"
	ActionAttachDisk = "CsiAttachDisk"
	ActionDetachDisk = "CsiDetachDisk"
	ActionDiskStatus = "CsiDiskInfo"
)

func CreateDisk(args *CreateDiskArgs) (*CreateDiskResponse, error) {
	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCSRequest(ActionCreateDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &CreateDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func DeleteDisk(args *DeleteDiskArgs) (*DeleteDiskResponse, error) {
	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCSRequest(ActionDeleteDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DeleteDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func AttachDisk(args *AttachDiskArgs) (*AttachDiskResponse, error) {
	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCSRequest(ActionAttachDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &AttachDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func DetachDisk(args *DetachDiskArgs) (*DetachDiskResponse, error) {
	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCSRequest(ActionDetachDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DetachDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func GetDiskInfo(args *DiskInfoArgs) (*DiskInfoResponse, error) {
	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCSRequest(ActionDiskStatus, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	res := &DiskInfoResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}
