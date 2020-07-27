package disk

import (
	"encoding/json"
	"fmt"

	"github.com/capitalonline/cck-sdk-go/pkg/common"
	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"
)

func CreateDisk(args *CreateDiskArgs) (*CreateDiskResponse, error) {
	log.Infof("api:: CreateDisk")

	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCKRequest(common.ActionCreateDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	log.Infof("api:: content is: %s", content)

	res := &CreateDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func AttachDisk(args *AttachDiskArgs) (*AttachDiskResponse, error) {
	log.Infof("api:: AttachDisk")

	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCKRequest(common.ActionAttachDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	log.Infof("api:: content is: %s", content)

	res := &AttachDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func DetachDisk(args *DetachDiskArgs) (*DetachDiskResponse, error) {
	log.Infof("api:: AttachDisk")

	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCKRequest(common.ActionDetachDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	log.Infof("api:: content is: %s", content)

	res := &DetachDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func DeleteDisk(args *DeleteDiskArgs) (*DeleteDiskResponse, error) {
	log.Infof("api:: AttachDisk")

	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCKRequest(common.ActionDeleteDisk, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	log.Infof("api:: content is: %s", content)

	res := &DeleteDiskResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func FindDiskByVolumeID(args *FindDiskByVolumeIDArgs) (*FindDiskByVolumeIDResponse, error) {
	log.Infof("api:: AttachDisk")

	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCKRequest(common.ActionFindDiskByVolumeID, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	log.Infof("api:: content is: %s", content)

	res := &FindDiskByVolumeIDResponse{}
	err = json.Unmarshal(content, res)

	return res, err
}

func FindDeviceNameByVolumeID(args *FindDeviceNameByVolumeIDArgs) (*FindDeviceNameByVolumeIDResponse, error) {
	log.Infof("api:: FindDeviceNameByVolumeID")

	body, err := common.MarshalJsonToIOReader(args)
	if err != nil {
		return nil, err
	}

	req, err := common.NewCCKRequest(common.ActionDeviceNameByVolumeID, http.MethodPost, nil, body)

	response, err := common.DoRequest(req)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(response.Body)
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("http error:%s, %s", response.Status, string(content))
	}

	log.Infof("api:: content is: %s", content)

	res := &FindDeviceNameByVolumeIDResponse{}
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
	req, err := common.NewCCKRequest(common.ActionDiskTaskStatus, http.MethodPost, nil, body)
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
