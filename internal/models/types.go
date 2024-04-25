// Copyright 2022 bytetrade
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

package models

type ListResult struct {
	Items      any   `json:"items"`
	TotalItems int   `json:"totalItems"`
	TotalCount int64 `json:"totalCount,omitempty"`
}

//	func NewListResult[T any](items []T) *ListResult {
//		return &ListResult{Items: items, TotalItems: len(items)}
//	}
func NewListResultWithCount[T any](items []T, count int64) *ListResult {
	return &ListResult{
		Items:      items,
		TotalItems: len(items),
		TotalCount: count,
	}
}

type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
	Data any    `json:"data,omitempty"`
}

func NewResponse(code int, msg string, data any) *Response {
	return &Response{
		Code: code,
		Msg:  msg,
		Data: data,
	}
}

type ResponseBase struct {
	Code int    `json:"code"`
	Msg  string `json:"message,omitempty"`
}

type InstallErrResponse struct {
	Code     int    `json:"code"`
	Resource string `json:"resource"`
	Msg      string `json:"message,omitempty"`
}

type ListResponseData struct {
	Items      []ApplicationInfo `json:"items"`
	TotalItems int               `json:"totalItems"`
	TotalCount int64             `json:"totalCount,omitempty"`
}

type ListResponse struct {
	ResponseBase
	Data ListResponseData `json:"data"`
}

type InfoResponse struct {
	ResponseBase
	Data ApplicationInfo `json:"data"`
}
