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

// system server api types

type PermissionRequire struct {
	Group    string   `json:"group"`
	DataType string   `json:"dataType"`
	Version  string   `json:"version"`
	Ops      []string `json:"ops"`
}

type AccessTokenRequest struct {
	AppKey    string            `json:"app_key"`
	Timestamp int64             `json:"timestamp"`
	Token     string            `json:"token"`
	Perm      PermissionRequire `json:"perm"`
}

type Header struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Header

	Data any `json:"data,omitempty"` // data field, optional, object or list
}

type AccessToken struct {
	AccessToken string `json:"access_token"`
}

type AccessTokenResp struct {
	Header

	Data AccessToken `json:"data,omitempty"`
}

// event types

type Event struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Data    Data   `json:"data"`
}

type Data struct {
	Message string      `json:"msg"`
	Payload interface{} `json:"payload"`
}
