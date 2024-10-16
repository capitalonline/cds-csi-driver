package consts

const ApiVersion = "2019-08-08"

const (
	ServiceEKS = "eks/v1"
)
const ProviderName = "cdscloud"

const (
	ApiHostAddress = "api.capitalonline.net"
)

const (
	ActionCreateBlock          = "CreateBlock"
	ActionDeleteBlock          = "DeleteBlock"
	ActionAttachBlock          = "AttachBlock"
	ActionDetachBlock          = "DetachBlock"
	ActionTaskStatus           = "TaskStatus"
	ActionDescribeBlockLimit   = "DescribeBlockLimit"
	ActionUpdateBlockFormat    = "UpdateBlockFormat"
	ActionDescribeBlockInfo    = "DescribeBlockInfo"
	ActionDescribeNodeMountNum = "DescribeNodeMountNum"
	ActionExpandBlockSize      = "ExpandBlockSize"
)

const (
	EnvAccessKeyID     = "CDS_ACCESS_KEY_ID"
	EnvAccessKeySecret = "CDS_ACCESS_KEY_SECRET"
	EnvRegion          = "CDS_REGION"
	EnvAPIHost         = "CDS_API_HOST"
	EnvClusterId       = "CDS_CLUSTER_ID"
	EnvAz              = "CDS_CLUSTER_AZ"
)
