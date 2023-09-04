package eks_client

import (
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/consts"
)

type Credential struct {
	SecretId  string
	SecretKey string
	Token     string
}

func NewCredential() *Credential {
	return &Credential{
		SecretId:  consts.AccessKeyID,
		SecretKey: consts.AccessKeySecret,
	}
}

func NewTokenCredential(secretId, secretKey, token string) *Credential {
	return &Credential{
		SecretId:  secretId,
		SecretKey: secretKey,
		Token:     token,
	}
}

func (c *Credential) GetCredentialParams() map[string]string {
	p := map[string]string{
		"SecretId": c.SecretId,
	}
	if c.Token != "" {
		p["Token"] = c.Token
	}
	return p
}
