package helper

// JSON / JSONB extraction utilities.
//
// These helpers operate on values produced by encoding/json and
// database/sql JSONB scans (i.e. interface{} / map[string]any
// trees) and surface them in the typed shapes the API contract
// expects. They are deliberately stdlib-only so the package stays
// at the bottom of the dependency graph (see env.go's package
// comment for the no-internal-imports invariant).

// StringMapFromAny converts a json-decoded `interface{}` (which
// arrives as map[string]interface{}) into the map[string]string
// shape consumers like types.AppSimpleInfo's description / title
// fields expect. Returns nil for empty / missing input so the
// JSON-marshalled response keeps clean shape.
func StringMapFromAny(v any) map[string]string {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// StringSliceFromAny is the slice equivalent of StringMapFromAny.
func StringSliceFromAny(v any) []string {
	s, ok := v.([]interface{})
	if !ok || len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, val := range s {
		if str, ok := val.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// PickStringFromAny coerces a json-decoded value into a single
// string, handling both bare-string and {locale: string}
// multi-language map shapes. Used where the wire contract surfaces
// a scalar even though the storage shape may be multi-language;
// the locale picker prefers en-US, then any other locale present.
func PickStringFromAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]interface{}:
		if s, ok := x["en-US"].(string); ok {
			return s
		}
		for _, vv := range x {
			if s, ok := vv.(string); ok {
				return s
			}
		}
	}
	return ""
}

// LocaleMapFromAny normalises a chart-repo metadata field that may
// arrive as either a {locale: string} map or a bare single-language
// string into the {locale: string} shape the wire contract carries.
// Bare strings are wrapped under "en-US" so legacy single-language
// manifests still surface a non-empty multi-language map. Returns
// nil for empty / nil input so the JSON output stays clean.
func LocaleMapFromAny(v any) map[string]string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		return map[string]string{"en-US": x}
	case map[string]interface{}:
		return StringMapFromAny(x)
	}
	return nil
}

// LocalisedString reads a single localised field from chart-repo's
// i18n bundle (field-major shape: {field: {locale: value}}) and
// returns the inner {locale: value} map with empty-string locales
// dropped, so the JSON output never carries entries like
// {"en-US": ""}. Returns nil if the field is absent or every locale
// is empty.
//
// Typical caller:
//
//	info.AppTitle       = helper.LocalisedString(row.I18n.Data, "title")
//	info.AppDescription = helper.LocalisedString(row.I18n.Data, "description")
func LocalisedString(i18n map[string]map[string]string, field string) map[string]string {
	inner, ok := i18n[field]
	if !ok || len(inner) == 0 {
		return nil
	}
	out := make(map[string]string, len(inner))
	for locale, v := range inner {
		if v == "" {
			continue
		}
		out[locale] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
