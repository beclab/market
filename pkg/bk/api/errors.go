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

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"market/pkg/utils"
	"net/http"
	"runtime"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

type ErrorType = string

const (
	ErrorInternalServerError ErrorType = "internal_server_error"
	ErrorInvalidGrant        ErrorType = "invalid_grant"
	ErrorBadRequest          ErrorType = "bad_request"
	ErrorUnknown             ErrorType = "unknown_error"
	ErrorIamOperator         ErrorType = "iam_operator"
)

const (
	OK                  = 200
	InternalServerError = 500

	Success = "success"
)

// Avoid emitting errors that look like valid HTML. Quotes are okay.
// var sanitizer = strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;")

func HandleInternalError(response *restful.Response, err error) {
	Handle(http.StatusInternalServerError, response, err)
}

func HandleUnauthorized(response *restful.Response, err error) {
	Handle(http.StatusUnauthorized, response, err)
}

func HandleError(response *restful.Response, err error) {

	glog.Info("err:", err.Error())
	info := make(map[string]interface{})
	errJ := json.Unmarshal([]byte(err.Error()), &info)
	if errJ == nil {
		response.Header().Set(restful.HEADER_ContentType, restful.MIME_JSON)
		_, err = response.Write([]byte(err.Error()))
		if err != nil {
			glog.Warningf("err:%s", err)
		}
		return
	}

	var statusCode int
	switch t := err.(type) {
	case restful.ServiceError:
		statusCode = t.Code
	default:
		statusCode = http.StatusBadRequest
	}
	Handle(statusCode, response, err)
}

func Handle(statusCode int, resp *restful.Response, err error) {
	_, fn, line, _ := runtime.Caller(2)
	glog.Errorf("%s:%d %v", fn, line, err)

	var errType ErrorType
	var errDesc string

	var t Error
	if errors.As(err, &t) {
		_ = resp.WriteHeaderAndEntity(statusCode, t)
		return
	}

	switch statusCode {
	case http.StatusBadRequest:
		errType = ErrorBadRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		errType = ErrorInvalidGrant
	case http.StatusInternalServerError:
		errType = ErrorInternalServerError
	default:
		errType = ErrorUnknown
	}
	errDesc = err.Error()
	_ = resp.WriteHeaderAndEntity(http.StatusOK, Error{
		Code:             statusCode,
		Msg:              errDesc,
		ErrorType:        errType,
		ErrorDescription: errDesc,
	})
}

type Error struct {
	Code             int    `json:"code"`
	Msg              string `json:"message"`
	ErrorType        string `json:"error_type,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (e Error) Error() string {
	return utils.PrettyJSON(e)
}

func NewError(t string, errs ...string) Error {
	var desc string
	if len(errs) > 0 {
		desc = errs[0]
	}
	return Error{ErrorType: t, ErrorDescription: desc}
}

func ErrorWithMessage(err error, message string) error {
	return fmt.Errorf("%v: %v", message, err.Error())
}

type ErrorMessage struct {
	Message string `json:"message"`
}

func (e ErrorMessage) Error() string {
	return e.Message
}

var ErrorNone = ErrorMessage{Message: "success"}
