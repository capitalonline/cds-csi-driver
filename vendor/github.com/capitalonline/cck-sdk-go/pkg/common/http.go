package common

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type CloudRequest struct {
	method      string
	params      map[string]string
	action      string
	productType string
	body        io.Reader
}

func NewCCKRequest(action, method string, params map[string]string, body io.Reader) (*CloudRequest, error) {
	return NewRequest(action, method, params, cckProductType, body)
}

func NewCCSRequest(action, method string, params map[string]string, body io.Reader) (*CloudRequest, error) {
	return NewRequest(action, method, params, ccsProductType, body)
}

func NewEbsRequest(action, method string, params map[string]string, body io.Reader) (*CloudRequest, error) {
	return NewRequest(action, method, params, ebsProductType, body)
}

func NewEcsRequest(action, method string, params map[string]string, body io.Reader) (*CloudRequest, error) {
	return NewRequest(action, method, params, ecsProductType, body)
}

func NewRequest(action, method string, params map[string]string, productType string, body io.Reader) (*CloudRequest, error) {
	method = strings.ToUpper(method)
	req := &CloudRequest{
		method:      method,
		params:      params,
		action:      action,
		productType: productType,
		body:        body,
	}
	return req, nil
}

func DoRequest(req *CloudRequest) (resp *http.Response, err error) {
	if !IsAccessKeySet() {
		return nil, fmt.Errorf("AccessKeyID or accessKeySecret is empty")
	}

	reqUrl := getUrl(req)
	sendRequest, err := http.NewRequest(req.method, reqUrl, req.body)
	if err != nil {
		return
	}
	sendRequest.Header.Set("Content-Type", "application/json")
	log.Infof("send request url: %s", reqUrl)
	resp, err = http.DefaultClient.Do(sendRequest)
	return
}

func getUrl(req *CloudRequest) string {
	urlParams := map[string]string{
		"Action":           req.action,
		"AccessKeyId":      AccessKeyID,
		"SignatureMethod":  signatureMethod,
		"SignatureNonce":   uuid.New().String(),
		"SignatureVersion": signatureVersion,
		"Timestamp":        time.Now().UTC().Format(timeStampFormat),
		"Version":          version,
	}
	if req.params != nil {
		for k, v := range req.params {
			urlParams[k] = v
		}
	}
	var paramSortKeys sort.StringSlice
	for k, _ := range urlParams {
		paramSortKeys = append(paramSortKeys, k)
	}
	sort.Sort(paramSortKeys)
	var urlStr string
	for _, k := range paramSortKeys {
		urlStr += "&" + percentEncode(k) + "=" + percentEncode(urlParams[k])
	}
	urlStr = req.method + "&%2F&" + percentEncode(urlStr[1:])

	h := hmac.New(sha1.New, []byte(AccessKeySecret))
	h.Write([]byte(urlStr))
	signStr := base64.StdEncoding.EncodeToString(h.Sum(nil))

	urlParams["Signature"] = signStr

	urlVal := url.Values{}
	for k, v := range urlParams {
		urlVal.Add(k, v)
	}
	urlValStr := urlVal.Encode()
	reqUrl := fmt.Sprintf("%s/%s?%s", APIHost, req.productType, urlValStr)
	return reqUrl
}

func percentEncode(str string) string {
	str = url.QueryEscape(str)
	strings.Replace(str, "+", "%20", -1)
	strings.Replace(str, "*", "%2A", -1)
	strings.Replace(str, "%7E", "~", -1)
	return str
}
