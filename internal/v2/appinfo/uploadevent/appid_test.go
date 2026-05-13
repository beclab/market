package uploadevent

import (
	"crypto/md5"
	"encoding/hex"
	"testing"

	"github.com/beclab/Olares/framework/oac"
	apimanifest "github.com/beclab/api/manifest"
)

// TestDeriveAppID_Stable pins the md5(metadata.appid)[:8] derivation
// against a hand-computed value so an accidental change to the
// algorithm (length, charset, hash) breaks loudly. The expected value
// is recomputed inside the test so it stays self-checking; an
// independent verification is in the comment.
func TestDeriveAppID_Stable(t *testing.T) {
	cfg := &oac.AppConfiguration{
		Metadata: apimanifest.AppMetaData{AppID: "wordpress.bytetrade.io"},
	}

	got := DeriveAppID(cfg)

	// Independent recompute: md5("wordpress.bytetrade.io")[:8].
	sum := md5.Sum([]byte("wordpress.bytetrade.io"))
	want := hex.EncodeToString(sum[:])[:8]

	if got != want {
		t.Fatalf("DeriveAppID = %q, want %q", got, want)
	}
	if len(got) != 8 {
		t.Fatalf("DeriveAppID length = %d, want 8", len(got))
	}
}

// TestDeriveAppID_TrimsWhitespace asserts that surrounding whitespace
// in metadata.appid does NOT change the derivation — chart-repo
// occasionally emits trailing newlines in YAML-derived strings and we
// must produce the same id either way (otherwise rendered vs uploaded
// rows for the same app would land on different keys).
func TestDeriveAppID_TrimsWhitespace(t *testing.T) {
	clean := DeriveAppID(&oac.AppConfiguration{
		Metadata: apimanifest.AppMetaData{AppID: "alpha"},
	})
	padded := DeriveAppID(&oac.AppConfiguration{
		Metadata: apimanifest.AppMetaData{AppID: "  alpha\n"},
	})

	if clean == "" {
		t.Fatal("DeriveAppID returned empty for clean input")
	}
	if clean != padded {
		t.Fatalf("DeriveAppID padded = %q, want %q (whitespace must not change derivation)", padded, clean)
	}
}

// TestDeriveAppID_EmptyInputs asserts the documented "" return contract
// for every nil / empty input branch. The handler treats "" as a
// hard skip, so flipping any of these to a non-empty fallback would
// silently corrupt the (source_id, app_id) keying.
func TestDeriveAppID_EmptyInputs(t *testing.T) {
	cases := []struct {
		name string
		cfg  *oac.AppConfiguration
	}{
		{"nil cfg", nil},
		{"empty appid", &oac.AppConfiguration{Metadata: apimanifest.AppMetaData{AppID: ""}}},
		{"whitespace only", &oac.AppConfiguration{Metadata: apimanifest.AppMetaData{AppID: "   "}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveAppID(tc.cfg); got != "" {
				t.Fatalf("DeriveAppID = %q, want empty string", got)
			}
		})
	}
}
