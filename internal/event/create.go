// Copyright 2023 bytetrade
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

func (c *Client) CreateEvent(eventType string, msg string, data interface{}) error {
	url := fmt.Sprintf("http://%s/system-server/v1alpha1/event/message-disptahcer.system-server/v1", eventServer)
	token, err := c.getAccessToken()
	if err != nil {
		return err
	}

	event := Event{
		Type:    eventType,
		Version: EventVersion,
		Data: Data{
			Message: msg,
			Payload: data,
		},
	}

	postData, err := json.Marshal(event)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.R().
		SetHeaders(map[string]string{
			restful.HEADER_ContentType: restful.MIME_JSON,
			AccessTokenHeader:          token,
		}).
		SetResult(&Response{}).
		SetBody(postData).
		Post(url)

	if err != nil {
		glog.Warningf("url:%s post %s err:%s", url, string(postData), err.Error())
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		glog.Warningf("url:%s post %s resp.StatusCode():%d not 200", url, string(postData), resp.StatusCode())
		return errors.New(string(resp.Body()))
	}

	responseData := resp.Result().(*Response)

	if responseData.Code != 0 {
		glog.Warningf("url:%s post %s responseData.Code:%d not 0", url, string(postData), responseData.Code)
		return errors.New(responseData.Message)
	}

	glog.Infof("url:%s post %s success responseData:%#v", url, string(postData), *responseData)

	return nil
}
