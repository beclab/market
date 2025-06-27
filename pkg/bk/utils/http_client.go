package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"market/internal/constants"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang/glog"
)

var (
	client *http.Client
	once   sync.Once
)

func init() {
	once.Do(func() {
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   time.Duration(3) * time.Second,
					KeepAlive: time.Duration(60) * time.Second,
				}).DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     time.Duration(60) * time.Second,
			},
			Timeout: time.Duration(60) * time.Second,
		}
	})
}

func GetHttpClient() *http.Client {
	return client
}

func Download(url, dstPath string) error {
	err := CheckParentDir(dstPath)
	if err != nil {
		glog.Warningf("CheckParentDir %s err:%s", dstPath, err.Error())
		return err
	}

	res, err := http.Get(url)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		glog.Warningf("http.Get url:%s err:%s", url, err.Error())
		return err
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("StatusCode %d not 200", res.StatusCode)
	}

	out, err := os.Create(dstPath)
	if err != nil {
		glog.Warningf("os.Create %s err:%s", dstPath, err)
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, res.Body)
	if err != nil {
		glog.Warningf("io.Copy %s err:%s", dstPath, err)
		return err
	}

	return nil
}

func SendHttpRequestWithMethod(method, url string, reader io.Reader) (string, error) {
	httpReq, err := http.NewRequest(method, url, reader)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return "", err
	}
	if reader != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	return SendHttpRequest(httpReq)
}

func SendHttpRequest(req *http.Request) (string, error) {
	resp, err := GetHttpClient().Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		glog.Warningf("url:%s, method:%s, err:%v", req.URL, req.Method, err)
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		glog.Warningf("url:%s, method:%s, err:%v", req.URL, req.Method, err)
		return "", err
	}
	//glog.Infof("%s, res:%s\n", req.URL, string(body))

	if resp.StatusCode != http.StatusOK {
		glog.Warningf("url:%s, res:%s, resp.StatusCode:%d", req.URL, string(body), resp.StatusCode)
		if len(body) != 0 {
			return string(body), errors.New(string(body))
		}
		return string(body), fmt.Errorf("http status not 200 %d msg:%s", resp.StatusCode, string(body))
	}

	debugBody := string(body)
	if len(debugBody) > 256 {
		debugBody = debugBody[:256]
	}
	// glog.Infof("url:%s, method:%s, resp.StatusCode:%d, body:%s", req.URL, req.Method, resp.StatusCode, debugBody)

	return string(body), nil
}

func SendHttpRequestByte(req *http.Request) ([]byte, error) {
	resp, err := GetHttpClient().Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		glog.Warningf("do request error: %v", err)
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	//glog.Infof("%s, res:%s\n", req.URL, string(body))

	if resp.StatusCode != http.StatusOK {
		glog.Warningf("url:%s, res:%s, resp.StatusCode:%d", req.URL, string(body), resp.StatusCode)
		if len(body) != 0 {
			return body, errors.New(string(body))
		}
		return body, fmt.Errorf("http status not 200 %d msg:%s", resp.StatusCode, string(body))
	}

	debugBody := string(body)
	if len(debugBody) > 256 {
		debugBody = debugBody[:256]
	}
	// glog.Infof("url:%s, method:%s, resp.StatusCode:%d, body:%s", req.URL, req.Method, resp.StatusCode, debugBody)

	return body, nil
}

func SendHttpRequestWithToken(method, url, token string, reader io.Reader) (string, error) {
	httpReq, err := http.NewRequest(method, url, reader)
	if err != nil {
		glog.Warningf("url:%s, err:%s", url, err.Error())
		return "", err
	}

	httpReq.Header.Set(constants.AuthorizationTokenKey, token)
	httpReq.Header.Set("Accept", "*/*")
	//if method == http.MethodPost {
	httpReq.Header.Set("Content-Type", "application/json")
	//}

	return SendHttpRequest(httpReq)
}

func DoHttpRequest(method, url, token string, bodyData interface{}) (string, map[string]interface{}, error) {
	var reader io.Reader
	if bodyData != nil {
		jsonData, err := json.Marshal(bodyData)
		if err != nil {
			return "", nil, errors.New("body data parse error")
		}

		reader = bytes.NewBuffer(jsonData)
	}
	httpReq, err := http.NewRequest(method, url, reader)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return "", nil, err
	}
	httpReq.Header.Set(constants.AuthorizationTokenKey, token)
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Content-Type", "application/json")

	body, err := SendHttpRequestByte(httpReq)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return string(body), nil, err
	}

	return ParseStrToMap(body)
}

func ParseStrToMap(data []byte) (string, map[string]interface{}, error) {
	app := make(map[string]interface{}) // simple get. TODO: application struct
	err := json.Unmarshal(data, &app)
	if err != nil {
		glog.Warning("parse response error: ", err, string(data))
		return string(data), nil, nil
	}

	return string(data), app, nil
}
