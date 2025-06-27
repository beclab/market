package v1

import (
	"bytes"
	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
	"io"
	"market/pkg/api"
)

type ReadMessage struct {
	Data     interface{} `json:"data"`
	UserName string      `json:"user_name"`
	ConnId   string      `json:"conn_id"`
}

type WriteMessage struct {
	MessageType int
	Message     interface{} `json:"message"`
	ConnId      string      `json:"conn_id"`
	Users       []string    `json:"users"`
}

func (h *Handler) receiveWebsocketMessage(req *restful.Request, resp *restful.Response) {
	for key, values := range req.Request.Header {
		for _, value := range values {
			glog.Infof("%s: %s\n", key, value)
		}
	}

	body, err := io.ReadAll(req.Request.Body)
	if err != nil {
		glog.Infof("read body err:%s", err.Error())
		api.HandleError(resp, err)
		return
	}

	glog.Infof("body: %s", string(body))
	req.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	_, _ = resp.Write([]byte("Hello, World!"))
}
