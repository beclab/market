package watchdog

import (
	"encoding/json"
	"market/pkg/utils"
	"net/http"
	"strings"

	"github.com/golang/glog"
)

const (
	broadcastUrl   = "http://localhost:40010/tapr/ws/conn/send"
	getConnListUrl = "http://localhost:40010/tapr/ws/conn/list"
)

type Connection struct {
	ID        string `json:"id"`
	UserAgent string `json:"userAgent"`
}

type UserConnection struct {
	Name  string       `json:"name"`
	Conns []Connection `json:"conns"`
}

type ConnResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []UserConnection `json:"data"`
}

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type BroadcastRequest struct {
	Payload interface{} `json:"payload"`
	ConnID  string      `json:"conn_id"`
	Users   []string    `json:"users"`
}

func getConnections() (*ConnResponse, error) {
	bodyStr, err := utils.SendHttpRequestWithMethod(http.MethodGet, getConnListUrl, nil)
	if err != nil {
		return nil, err
	}

	var response ConnResponse
	err = json.Unmarshal([]byte(bodyStr), &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func BroadcastMessage(payload interface{}) error {
	connRes, err := getConnections()
	if err != nil {
		return err
	}

	if len(connRes.Data) == 0 {
		glog.Info("empty conn return")
		return nil
	}

	users := make([]string, len(connRes.Data))
	for i := range connRes.Data {
		users[i] = connRes.Data[i].Name
	}

	glog.Infof("connRes:%v", connRes)
	requestBody := BroadcastRequest{
		Payload: payload,
		ConnID:  "",
		Users:   users,
	}

	requestBodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}

	bodyStr, err := utils.SendHttpRequestWithMethod(http.MethodPost, broadcastUrl, strings.NewReader(string(requestBodyJSON)))
	if err != nil {
		glog.Warningf("err:%v", err)
		return err
	}

	glog.Infof("post %s to %s success, res:%s", string(requestBodyJSON), broadcastUrl, bodyStr)

	var response Response
	err = json.Unmarshal([]byte(bodyStr), &response)
	if err != nil {
		glog.Warningf("err:%v", err)
		return err
	}

	return nil
}
