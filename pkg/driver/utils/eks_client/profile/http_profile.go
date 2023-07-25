package profile

import (
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/consts"
	url2 "net/url"
	"os"
)

type HttpProfile struct {
	ReqMethod  string
	ReqTimeout int
	Endpoint   string
	Scheme     string
}

func NewHttpProfile() *HttpProfile {
	var apiHost = os.Getenv(consts.EnvAPIHost)
	endpoint, scheme := "", "https"
	if url, err := url2.Parse(apiHost); err != nil {
		endpoint = url.Host
		scheme = url.Scheme
	}
	return &HttpProfile{
		ReqMethod:  "POST",
		ReqTimeout: 60,
		Endpoint:   endpoint,
		Scheme:     scheme,
	}
}
