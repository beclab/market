package models

type Nsfw struct {
	Nsfw bool `json:"nsfw"`
}

type NsfwResp struct {
	Response
	Data Nsfw `json:"data"`
}
