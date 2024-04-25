package appmgr

import (
	"encoding/json"
	"market/internal/models"
)

type Topic struct {
	Name         string                   `json:"name"`
	Introduction string                   `json:"introduction,omitempty"`
	Des          string                   `json:"des,omitempty"`
	IconImg      string                   `json:"iconimg,omitempty"`
	DetailImg    string                   `json:"detailimg,omitempty"`
	RichText     string                   `json:"richtext,omitempty"`
	Apps         []models.ApplicationInfo `json:"apps"`
}

type CategoryData struct {
	Type        string            `json:"type"`
	Name        string            `json:"name,omitempty"`
	Id          string            `json:"id,omitempty"`
	Description string            `json:"description,omitempty"`
	TopicType   string            `json:"topicType,omitempty"`
	Content     []json.RawMessage `json:"content,omitempty"`
}

type ResultData struct {
	Category string         `json:"category"`
	Data     []CategoryData `json:"data"`
}

type Result struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    []ResultData `json:"data"`
}
