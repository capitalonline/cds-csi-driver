package eks

import (
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/consts"
	cdshttp "github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/http"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/profile"
)

type Client struct {
	eks_client.Client
}

func NewClient(credential *eks_client.Credential, region string, clientProfile *profile.ClientProfile) (client *Client, err error) {
	client = &Client{}
	client.Init(region).
		WithCredential(credential).
		WithProfile(clientProfile)
	return
}

func NewCreateBlockRequest() (request *CreateBlockRequest) {
	request = &CreateBlockRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionCreateBlock)
	request.SetDomain(consts.ApiHost)
	return
}

func NewCreateBlockResponse() (response *CreateBlockResponse) {
	response = &CreateBlockResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) CreateBlock(request *CreateBlockRequest) (response *CreateBlockResponse, err error) {
	if request == nil {
		request = NewCreateBlockRequest()
	}
	response = NewCreateBlockResponse()
	err = c.Send(request, response)
	return
}

func NewDeleteBlockRequest() (request *DeleteBlockRequest) {
	request = &DeleteBlockRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionDeleteBlock)
	request.SetDomain(consts.ApiHost)
	return
}

func NewDeleteBlockResponse() (response *DeleteBlockResponse) {
	response = &DeleteBlockResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) DeleteBlock(request *DeleteBlockRequest) (response *DeleteBlockResponse, err error) {
	if request == nil {
		request = NewDeleteBlockRequest()
	}
	response = NewDeleteBlockResponse()
	err = c.Send(request, response)
	return
}

func NewAttachBlockRequest() (request *AttachBlockRequest) {
	request = &AttachBlockRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionAttachBlock)
	request.SetDomain(consts.ApiHost)
	return
}

func NewAttachBlockResponse() (response *AttachBlockResponse) {
	response = &AttachBlockResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) AttachBlock(request *AttachBlockRequest) (response *AttachBlockResponse, err error) {
	if request == nil {
		request = NewAttachBlockRequest()
	}
	response = NewAttachBlockResponse()
	err = c.Send(request, response)
	return
}

func NewDetachBlockRequest() (request *DetachBlockRequest) {
	request = &DetachBlockRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionDetachBlock)
	request.SetDomain(consts.ApiHost)
	return
}

func NewDetachBlockResponse() (response *DetachBlockResponse) {
	response = &DetachBlockResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) DetachBlock(request *DetachBlockRequest) (response *DetachBlockResponse, err error) {
	if request == nil {
		request = NewDetachBlockRequest()
	}
	response = NewDetachBlockResponse()
	err = c.Send(request, response)
	return
}

func NewTaskStatusRequest() (request *TaskStatusRequest) {
	request = &TaskStatusRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionTaskStatus)
	request.SetDomain(consts.ApiHost)
	return
}

func NewTaskStatusResponse() (response *TaskStatusResponse) {
	response = &TaskStatusResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) TaskStatus(request *TaskStatusRequest) (response *TaskStatusResponse, err error) {
	if request == nil {
		request = NewTaskStatusRequest()
	}
	response = NewTaskStatusResponse()
	err = c.Send(request, response)
	return
}

func NewDescribeBlockLimitRequest() (request *DescribeBlockLimitRequest) {
	request = &DescribeBlockLimitRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionDescribeBlockLimit)
	request.SetDomain(consts.ApiHost)
	return
}

func NewDescribeBlockLimitResponse() (response *DescribeBlockLimitResponse) {
	response = &DescribeBlockLimitResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) DescribeBlockLimit(request *DescribeBlockLimitRequest) (response *DescribeBlockLimitResponse, err error) {
	if request == nil {
		request = NewDescribeBlockLimitRequest()
	}
	response = NewDescribeBlockLimitResponse()
	err = c.Send(request, response)
	return
}

func NewDescribeBlockInfoRequest() (request *DescribeBlockInfoRequest) {
	request = &DescribeBlockInfoRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionDescribeBlockInfo)
	request.SetDomain(consts.ApiHost)
	return
}

func NewDescribeBlockInfoResponse() (response *DescribeBlockInfoResponse) {
	response = &DescribeBlockInfoResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) DescribeBlockInfo(request *DescribeBlockInfoRequest) (response *DescribeBlockInfoResponse, err error) {
	if request == nil {
		request = NewDescribeBlockInfoRequest()
	}
	response = NewDescribeBlockInfoResponse()
	err = c.Send(request, response)
	return
}

func NewUpdateBlockFormatRequest() (request *UpdateBlockFormatRequest) {
	request = &UpdateBlockFormatRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionUpdateBlockFormat)
	request.SetDomain(consts.ApiHost)
	return
}

func NewUpdateBlockFormatResponse() (response *UpdateBlockFormatResponse) {
	response = &UpdateBlockFormatResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) UpdateBlockFormat(request *UpdateBlockFormatRequest) (response *UpdateBlockFormatResponse, err error) {
	if request == nil {
		request = NewUpdateBlockFormatRequest()
	}
	response = NewUpdateBlockFormatResponse()
	err = c.Send(request, response)
	return
}

func NewDescribeNodeMountNumRequest() (request *DescribeNodeMountNumRequest) {
	request = &DescribeNodeMountNumRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionDescribeNodeMountNum)
	request.SetDomain(consts.ApiHost)
	return
}

func NewDescribeNodeMountNumResponse() (response *DescribeNodeMountNumResponse) {
	response = &DescribeNodeMountNumResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) DescribeNodeMountNum(request *DescribeNodeMountNumRequest) (response *DescribeNodeMountNumResponse, err error) {
	if request == nil {
		request = NewDescribeNodeMountNumRequest()
	}
	response = NewDescribeNodeMountNumResponse()
	err = c.Send(request, response)
	return
}

func NewExpandBlockSizeRequest() (request *ExpandBlockSizeRequest) {
	request = &ExpandBlockSizeRequest{
		BaseRequest: &cdshttp.BaseRequest{},
	}
	request.Init().WithApiInfo(consts.ServiceEKS, consts.ApiVersion, consts.ActionExpandBlockSize)
	request.SetDomain(consts.ApiHost)
	return
}

func NewExpandBlockSizeResponse() (response *ExpandBlockSizeResponse) {
	response = &ExpandBlockSizeResponse{BaseResponse: &cdshttp.BaseResponse{}}
	return
}

func (c *Client) ExpandBlockSize(request *ExpandBlockSizeRequest) (response *ExpandBlockSizeResponse, err error) {
	if request == nil {
		request = NewExpandBlockSizeRequest()
	}
	response = NewExpandBlockSizeResponse()
	err = c.Send(request, response)
	return
}
