package eks_client

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/errors"

	cdshttp "github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/http"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils/eks_client/profile"
)

type Client struct {
	region          string
	httpClient      *http.Client
	httpProfile     *profile.HttpProfile
	profile         *profile.ClientProfile
	credential      *Credential
	signMethod      string
	unsignedPayload bool
	debug           bool
}

func (c *Client) Send(request cdshttp.Request, response cdshttp.Response) (err error) {
	if request.GetDomain() == "" {
		domain := c.httpProfile.Endpoint
		if domain == "" {
			domain = cdshttp.GetServiceDomain(request.GetService())
		}
		request.SetDomain(domain)
	}

	if request.GetHttpMethod() == "" {
		request.SetHttpMethod(c.httpProfile.ReqMethod)
	}
	if request.GetScheme() == "" {
		request.SetScheme(c.httpProfile.Scheme)
	}
	cdshttp.CompleteCdsParams(c.credential.SecretId, request)
	return c.sendWithSignatureCds(request, response)
}

func (c *Client) sendWithSignatureCds(request cdshttp.Request, response cdshttp.Response) (err error) {
	headers := map[string]string{
		"Host":            request.GetDomain(),
		"Action":          request.GetAction(),
		"Version":         request.GetVersion(),
		"X-Timestamp":     request.GetParams()["Timestamp"],
		"X-RequestClient": request.GetParams()["RequestClient"],
		"X-Language":      c.profile.Language,
	}
	if request.GetHttpMethod() == "GET" {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	} else {
		headers["Content-Type"] = "application/json"
	}
	httpRequestMethod := request.GetHttpMethod()

	var canonicalQueryString = ""
	if httpRequestMethod == "GET" {
		// 需要先把参数进行格式化，将参数也一起签名
		err = cdshttp.ConstructParams(request)
		if err != nil {
			return err
		}
		err = signRequest(request, c.credential, c.signMethod)
		if err != nil {
			return err
		}
		params := make(map[string]string)
		for key, value := range request.GetParams() {
			params[key] = value
		}
		//var p = make(map[string]interface{})
		//bytes, _ := json.Marshal(request)
		//_ = json.Unmarshal(bytes, &p)
		//for key, value := range p {
		//	params[key] = fmt.Sprintf("%v", value)
		//}
		canonicalQueryString = cdshttp.GetUrlQueriesEncoded(params)
	}
	requestPayload := ""
	if httpRequestMethod == "POST" {
		// 只对请求路径进行签名，Body 参与签名
		err = signRequest(request, c.credential, c.signMethod)
		canonicalQueryString = cdshttp.GetUrlQueriesEncoded(request.GetParams())
		if err != nil {
			return err
		}
		b, err := json.Marshal(request)
		if err != nil {
			return err
		}
		requestPayload = string(b)
	}

	url := request.GetScheme() + "://" + request.GetDomain() + request.GetPath() + request.GetService()
	if canonicalQueryString != "" {
		url = url + "?" + canonicalQueryString
	}
	log.Printf("url: %s, request body: %v \n", url, requestPayload)
	httpRequest, err := http.NewRequest(httpRequestMethod, url, strings.NewReader(requestPayload))
	if err != nil {
		return err
	}
	if c.debug {
		outbytes, err := httputil.DumpRequest(httpRequest, true)
		if err != nil {
			log.Printf("[ERROR] dump request failed because %s", err)
			return err
		}
		log.Printf("[DEBUG] http url = %s\n request = %s", url, outbytes)
	}
	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		msg := fmt.Sprintf("Fail to get response because %s", err)
		return errors.NewCdsSDKError("ClientError.NetworkError. Errors.Code:", msg, "")
	}
	err = cdshttp.ParseFromHttpResponse(httpResponse, response, request)
	log.Printf("response: %#v", response)
	return err
}

func (c *Client) GetRegion() string {
	return c.region
}

func (c *Client) Init(region string) *Client {
	c.httpClient = &http.Client{
		Timeout: 20 * time.Second,
	}
	c.region = region
	c.signMethod = "HmacSHA1"
	//TODO dev:true release:false
	c.debug = false
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return c
}

func (c *Client) SetDebug(flag bool) {
	c.debug = flag
}

func (c *Client) GetDebug() bool {
	return c.debug
}

func (c *Client) WithSecretId(secretId, secretKey string) *Client {
	c.credential = NewCredential()
	return c
}

func (c *Client) WithCredential(cred *Credential) *Client {
	c.credential = cred
	return c
}

func (c *Client) WithProfile(clientProfile *profile.ClientProfile) *Client {
	c.profile = clientProfile
	c.signMethod = clientProfile.SignMethod
	c.unsignedPayload = clientProfile.UnsignedPayload
	c.httpProfile = clientProfile.HttpProfile
	c.httpClient.Timeout = time.Duration(c.httpProfile.ReqTimeout) * time.Second
	return c
}

func (c *Client) WithSignatureMethod(method string) *Client {
	c.signMethod = method
	return c
}

func (c *Client) WithHttpTransport(transport http.RoundTripper) *Client {
	c.httpClient.Transport = transport
	return c
}
