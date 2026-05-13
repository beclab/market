package uploadevent

import (
	"errors"
	"testing"
)

// TestDecodeUploadPayload_HappyPath round-trips a representative
// chart-repo /apps response into the typed shape and asserts the
// fields downstream writers depend on (raw_data_ex / metadata.appid /
// raw_package / rendered_package / version) all survive intact. Uses
// a JSON-keyed nested map so we exercise the json.Marshal+Unmarshal
// path the production caller hits.
func TestDecodeUploadPayload_HappyPath(t *testing.T) {
	in := map[string]interface{}{
		"type":             "app-info-latest",
		"version":          "1.1.8",
		"raw_package":      "file:///opt/app/data/v2/upload/wordpresspure-1.1.8.tgz",
		"rendered_package": "/opt/app/data/v2/zhaoyu002/upload/wordpresspure-1.1.8",
		"raw_data_ex": map[string]interface{}{
			"olaresManifest.version": "0.13.0",
			"olaresManifest.type":    "app",
			"apiVersion":             "v1",
			"metadata": map[string]interface{}{
				"name":    "wordpresspure",
				"appid":   "wordpresspure.bytetrade.io",
				"version": "1.1.8",
				"type":    "app",
			},
		},
		"i18n":      map[string]interface{}{"en-US": map[string]interface{}{"title": "WordPress Pure"}},
		"app_info":  map[string]interface{}{"app_entry": map[string]interface{}{"name": "wordpresspure"}},
	}

	payload, err := DecodeUploadPayload(in)
	if err != nil {
		t.Fatalf("DecodeUploadPayload returned error: %v", err)
	}
	if payload == nil || payload.RawDataEx == nil {
		t.Fatal("payload or raw_data_ex is nil")
	}
	if got, want := payload.RawDataEx.Metadata.AppID, "wordpresspure.bytetrade.io"; got != want {
		t.Errorf("metadata.appid = %q, want %q", got, want)
	}
	if got, want := payload.RawDataEx.Metadata.Name, "wordpresspure"; got != want {
		t.Errorf("metadata.name = %q, want %q", got, want)
	}
	if got, want := payload.RawDataEx.Metadata.Version, "1.1.8"; got != want {
		t.Errorf("metadata.version = %q, want %q", got, want)
	}
	if got, want := payload.RawDataEx.ConfigVersion, "0.13.0"; got != want {
		t.Errorf("ConfigVersion = %q, want %q", got, want)
	}
	if got, want := payload.RawDataEx.ConfigType, "app"; got != want {
		t.Errorf("ConfigType = %q, want %q", got, want)
	}
	if got, want := payload.RawPackage, "file:///opt/app/data/v2/upload/wordpresspure-1.1.8.tgz"; got != want {
		t.Errorf("RawPackage = %q, want %q", got, want)
	}
	if got, want := payload.RenderedPackage, "/opt/app/data/v2/zhaoyu002/upload/wordpresspure-1.1.8"; got != want {
		t.Errorf("RenderedPackage = %q, want %q", got, want)
	}
}

// TestDecodeUploadPayload_Unusable nails the documented sentinel
// returns for every "structurally valid but missing the bits we need"
// case. Each must produce ErrPayloadUnusable exactly so the handler
// can distinguish them from real decode failures.
func TestDecodeUploadPayload_Unusable(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
	}{
		{
			name: "nil map",
			in:   nil,
		},
		{
			name: "missing raw_data_ex",
			in: map[string]interface{}{
				"version": "1.0.0",
			},
		},
		{
			name: "raw_data_ex without metadata.appid",
			in: map[string]interface{}{
				"raw_data_ex": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":    "x",
						"version": "1.0",
					},
				},
			},
		},
		{
			name: "metadata.appid is whitespace",
			in: map[string]interface{}{
				"raw_data_ex": map[string]interface{}{
					"metadata": map[string]interface{}{
						"appid": "   ",
					},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeUploadPayload(tc.in)
			if !errors.Is(err, ErrPayloadUnusable) {
				t.Fatalf("got err=%v, want ErrPayloadUnusable", err)
			}
		})
	}
}
