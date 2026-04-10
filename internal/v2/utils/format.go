package utils

import (
	"encoding/json"
)

func ParseJson(v interface{}) string {
	r, _ := json.Marshal(v)
	return string(r)
}
