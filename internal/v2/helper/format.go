package helper

import "encoding/json"

// ParseJson marshals v to a JSON string for logging / diagnostic purposes,
// swallowing any encoding error and returning an empty string in that case.
// It is intentionally lossy: callers should not feed its output back into a
// JSON decoder.
func ParseJson(v interface{}) string {
	r, _ := json.Marshal(v)
	return string(r)
}
