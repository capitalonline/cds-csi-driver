package eks_client

import "os"

type Credential struct {
	SecretId  string
	SecretKey string
	Token     string
}

func NewCredential() *Credential {
	return &Credential{
		SecretId:  os.Getenv("ACCESS_KEY_ID"),
		SecretKey: os.Getenv("ACCESS_KEY_SECRET"),
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
