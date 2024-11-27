package v1

import (
	"errors"
	"market/internal/models"
	"market/internal/redisdb"
	"market/pkg/api"
	"net/http"

	"github.com/emicklei/go-restful/v3"
)

func (h *Handler) setNsfw(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	nsfw := &models.Nsfw{}
	err := req.ReadEntity(nsfw)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	err = redisdb.SetNsfwState(nsfw.Nsfw)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	_ = resp.WriteHeaderAndEntity(http.StatusOK, models.ResponseBase{
		Code: api.OK,
		Msg:  api.Success,
	})
}

func (h *Handler) getNsfw(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}
	nsfw := redisdb.GetNsfwState()
	_ = resp.WriteEntity(
		models.NsfwResp{
			Response: models.Response{
				Code: api.OK,
				Msg:  api.Success,
			},
			Data: models.Nsfw{
				Nsfw: nsfw,
			},
		},
	)
}
