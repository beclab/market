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

// PivotI18n transposes a chart-repo i18n bundle from locale-major
// ({locale: {field: value}}) to field-major ({field: {locale: value}})
// for the requested field names. Empty values are dropped so the
// JSON output never carries {locale: ""} entries; fields that end
// up with no locales are omitted from the result so a caller's
// `len(out[field]) > 0` check works as a "have any localised data
// for field?" probe.
//
// Typical caller:
//
//	pivoted := helper.PivotI18n(row.I18n.Data, "title", "description")
//	info.AppTitle       = pivoted["title"]
//	info.AppDescription = pivoted["description"]
func PivotI18n(i18n map[string]map[string]string, fields ...string) map[string]map[string]string {
	if len(i18n) == 0 || len(fields) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(fields))
	for locale, kv := range i18n {
		for _, field := range fields {
			v := kv[field]
			if v == "" {
				continue
			}
			inner, ok := out[field]
			if !ok {
				inner = make(map[string]string)
				out[field] = inner
			}
			inner[locale] = v
		}
	}
	return out
}
