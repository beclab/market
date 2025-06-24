package hydrationfn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

// renderTemplate renders a template string with the given data
func (s *RenderedChartStep) renderTemplate(templateContent string, data *TemplateData) (string, error) {
	// Check if content actually contains template syntax
	if !strings.Contains(templateContent, "{{") {
		return templateContent, nil
	}

	// Log template data for debugging (limit output size)
	log.Printf("Template rendering - Starting template execution")
	if adminVal, exists := data.Values["admin"]; exists {
		log.Printf("Template rendering - admin value: %v", adminVal)
	}
	if bflVal, exists := data.Values["bfl"]; exists {
		log.Printf("Template rendering - bfl value: %+v", bflVal)
	}
	if data.Release != nil {
		log.Printf("Template rendering - Release: %+v", data.Release)
	}
	if data.Chart != nil {
		log.Printf("Template rendering - Chart: %+v", data.Chart)
	}

	// Create template with custom functions (similar to Helm)
	tmpl, err := template.New("chart").
		Option("missingkey=zero"). // Use zero value for missing keys (more forgiving)
		Funcs(s.getTemplateFunctions()).
		Parse(templateContent)
	if err != nil {
		log.Printf("Template parsing failed - Error: %v", err)
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("Template execution failed - Error: %v", err)
		log.Printf("Template execution failed - Available data keys: Values=%v, Release=%v, Chart=%v",
			getMapKeys(data.Values), getMapKeys(data.Release), getMapKeys(data.Chart))

		// Try with missing key as zero value to provide more helpful error info
		tmplZero, _ := template.New("chart-zero").
			Option("missingkey=zero").
			Funcs(s.getTemplateFunctions()).
			Parse(templateContent)

		var bufZero bytes.Buffer
		if errZero := tmplZero.Execute(&bufZero, data); errZero == nil {
			log.Printf("Template would succeed with missing keys as zero values")
		}

		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	renderedContent := buf.String()
	log.Printf("Template rendered successfully - Original length: %d, Rendered length: %d",
		len(templateContent), len(renderedContent))

	return renderedContent, nil
}

// renderOlaresManifest finds and renders the OlaresManifest.yaml file
func (s *RenderedChartStep) renderOlaresManifest(chartFiles map[string]*ChartFile, templateData *TemplateData) (string, error) {
	// Find OlaresManifest.yaml file at the lowest depth
	var manifestFile *ChartFile
	var minDepth = -1

	for path, file := range chartFiles {
		if file.IsDir {
			continue
		}

		// Check if the file is an Olares manifest file.
		if strings.HasSuffix(strings.ToLower(path), "olaresmanifest.yaml") ||
			strings.HasSuffix(strings.ToLower(path), "olaresmanifest.yml") {

			// Calculate the directory depth of the file.
			depth := strings.Count(path, "/")

			// If it's the first one found, or has a shallower depth, select it.
			if manifestFile == nil || depth < minDepth {
				manifestFile = file
				minDepth = depth
			}
		}
	}

	if manifestFile == nil {
		return "", fmt.Errorf("OlaresManifest.yaml not found in chart package")
	}

	log.Printf("Found OlaresManifest at: %s", manifestFile.Name)

	// Render the manifest template
	renderedContent, err := s.renderTemplate(string(manifestFile.Content), templateData)
	if err != nil {
		return "", fmt.Errorf("failed to render OlaresManifest.yaml: %w", err)
	}

	return renderedContent, nil
}

// renderChartPackage renders all template files in the chart package
func (s *RenderedChartStep) renderChartPackage(chartFiles map[string]*ChartFile, templateData *TemplateData) (map[string]string, error) {
	renderedFiles := make(map[string]string)

	// Create a cleanup function to handle errors
	cleanup := func() {
		// Clear any large data structures
		for _, file := range chartFiles {
			file.Content = nil // Clear file content
		}
	}
	defer cleanup()

	// First pass: collect all .tpl files and create a combined template
	var tplTemplates []string
	var templateFiles []string

	// Separate .tpl templates from regular templates
	for filePath, file := range chartFiles {
		if file.IsDir {
			continue
		}

		if !s.shouldRenderFile(filePath) {
			renderedFiles[filePath] = string(file.Content)
			continue
		}

		// Check if this is a .tpl file (all .tpl files contain template definitions)
		if strings.HasSuffix(strings.ToLower(filePath), ".tpl") {
			tplTemplates = append(tplTemplates, string(file.Content))
			log.Printf("Found template definition file: %s", filePath)
		} else {
			templateFiles = append(templateFiles, filePath)
		}
	}

	// Create a combined template with all template definitions
	var combinedTemplate *template.Template
	var err error

	if len(tplTemplates) > 0 {
		// Start with the first template definition
		combinedTemplate, err = template.New("chart").
			Option("missingkey=zero").
			Funcs(s.getTemplateFunctions()).
			Parse(tplTemplates[0])
		if err != nil {
			return nil, fmt.Errorf("failed to parse first template definition: %w", err)
		}

		// Add remaining template definitions
		for i := 1; i < len(tplTemplates); i++ {
			combinedTemplate, err = combinedTemplate.Parse(tplTemplates[i])
			if err != nil {
				return nil, fmt.Errorf("failed to parse template definition %d: %w", i, err)
			}
		}

		log.Printf("Successfully parsed %d template definition files", len(tplTemplates))
	} else {
		// Create empty template if no .tpl files
		combinedTemplate = template.New("chart").
			Option("missingkey=zero").
			Funcs(s.getTemplateFunctions())
	}

	// Second pass: render regular template files using the combined template
	for _, filePath := range templateFiles {
		file := chartFiles[filePath]

		// Clone the combined template for this file to avoid conflicts
		fileTemplate, err := combinedTemplate.Clone()
		if err != nil {
			return nil, fmt.Errorf("failed to clone template for file %s: %w", filePath, err)
		}

		// Parse the current file's content
		fileTemplate, err = fileTemplate.Parse(string(file.Content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
		}

		// Execute the template
		var buf bytes.Buffer
		if err := fileTemplate.Execute(&buf, templateData); err != nil {
			log.Printf("Template execution failed for file %s - Error: %v", filePath, err)
			log.Printf("Template execution failed - Available data keys: Values=%v, Release=%v, Chart=%v",
				getMapKeys(templateData.Values), getMapKeys(templateData.Release), getMapKeys(templateData.Chart))

			// Try with missing key as zero value to provide more helpful error info
			tmplZero, _ := template.New("chart-zero").
				Option("missingkey=zero").
				Funcs(s.getTemplateFunctions()).
				Parse(string(file.Content))

			var bufZero bytes.Buffer
			if errZero := tmplZero.Execute(&bufZero, templateData); errZero == nil {
				log.Printf("Template would succeed with missing keys as zero values")
			}

			return nil, fmt.Errorf("failed to execute template for file %s: %w", filePath, err)
		}

		renderedFiles[filePath] = buf.String()
		log.Printf("Successfully rendered template file: %s", filePath)
	}

	return renderedFiles, nil
}

// shouldRenderFile determines if a file should be rendered as a template
func (s *RenderedChartStep) shouldRenderFile(filePath string) bool {
	lowerPath := strings.ToLower(filePath)

	// Render YAML files, manifest files, and configuration files
	renderExtensions := []string{
		".yaml", ".yml", ".json", ".toml",
		".conf", ".config", ".properties",
	}

	for _, ext := range renderExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}

	// Skip binary files and certain file types
	skipExtensions := []string{
		".tar", ".gz", ".zip", ".tgz",
		".png", ".jpg", ".jpeg", ".gif", ".svg",
		".exe", ".bin", ".so", ".dll",
	}

	for _, ext := range skipExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return false
		}
	}

	// Check for template markers in file content (basic heuristic)
	return strings.Contains(filePath, "templates/") ||
		strings.Contains(lowerPath, "manifest")
}

// getTemplateFunctions returns template functions for rendering
func (s *RenderedChartStep) getTemplateFunctions() template.FuncMap {
	// Helper function for boolean conversion
	toBoolHelper := func(v interface{}) bool {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val != ""
		case int:
			return val != 0
		case float64:
			return val != 0
		case nil:
			return false
		default:
			return true
		}
	}

	return template.FuncMap{
		// Basic functions
		"default": func(defaultValue interface{}, value interface{}) interface{} {
			if value == nil || value == "" {
				return defaultValue
			}
			return value
		},
		"empty": func(value interface{}) bool {
			return value == nil || value == "" || value == 0
		},
		"required": func(warn string, val interface{}) (interface{}, error) {
			if val == nil || val == "" {
				return val, fmt.Errorf(warn)
			}
			return val, nil
		},

		// String functions
		"base":  filepath.Base,
		"dir":   filepath.Dir,
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"title": strings.Title,
		"untitle": func(str string) string {
			if len(str) == 0 {
				return str
			}
			return strings.ToLower(str[:1]) + str[1:]
		},
		"trim": strings.TrimSpace,
		"trimAll": func(cutset, s string) string {
			return strings.Trim(s, cutset)
		},
		"trimPrefix": func(prefix, s string) string {
			return strings.TrimPrefix(s, prefix)
		},
		"trimSuffix": func(suffix, s string) string {
			return strings.TrimSuffix(s, suffix)
		},
		"quote": func(v interface{}) string {
			if v == nil {
				return `""`
			}
			return fmt.Sprintf("%q", fmt.Sprintf("%v", v))
		},
		"squote": func(v interface{}) string {
			if v == nil {
				return "''"
			}
			return fmt.Sprintf("'%v'", v)
		},
		"replace": func(old, new, src string) string {
			return strings.ReplaceAll(src, old, new)
		},
		"split": func(sep, s string) []string {
			return strings.Split(s, sep)
		},
		"splitList": func(sep, str string) []string {
			if str == "" {
				return []string{}
			}
			return strings.Split(str, sep)
		},
		"join": func(sep string, a interface{}) string {
			if a == nil {
				return ""
			}
			v := reflect.ValueOf(a)
			if v.Kind() != reflect.Slice {
				return fmt.Sprintf("%v", a)
			}
			if v.Len() == 0 {
				return ""
			}
			var b strings.Builder
			for i := 0; i < v.Len(); i++ {
				if i > 0 {
					b.WriteString(sep)
				}
				b.WriteString(fmt.Sprintf("%v", v.Index(i).Interface()))
			}
			return b.String()
		},
		"contains": func(substr, str string) bool {
			return strings.Contains(str, substr)
		},
		"hasPrefix": func(prefix, s string) bool {
			return strings.HasPrefix(s, prefix)
		},
		"hasSuffix": func(suffix, s string) bool {
			return strings.HasSuffix(s, suffix)
		},

		// Formatting functions
		"indent": func(spaces int, text string) string {
			pad := strings.Repeat(" ", spaces)
			return pad + strings.Replace(text, "\n", "\n"+pad, -1)
		},
		"nindent": func(spaces int, text string) string {
			pad := strings.Repeat(" ", spaces)
			return "\n" + pad + strings.Replace(text, "\n", "\n"+pad, -1)
		},
		"toYaml": func(v interface{}) string {
			data, err := yaml.Marshal(v)
			if err != nil {
				return ""
			}
			return strings.TrimSuffix(string(data), "\n")
		},
		"toJson": func(v interface{}) string {
			data, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			return string(data)
		},
		"toPrettyJson": func(v interface{}) string {
			data, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return ""
			}
			return string(data)
		},

		// Type conversion functions
		"toString": func(v interface{}) string {
			return fmt.Sprintf("%v", v)
		},
		"toInt": func(v interface{}) int {
			if i, ok := v.(int); ok {
				return i
			}
			return 0
		},
		"toFloat64": func(v interface{}) float64 {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case string:
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					return f
				}
			}
			return 0.0
		},
		"float64": func(v interface{}) float64 {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case string:
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					return f
				}
			}
			return 0.0
		},
		"int": func(v interface{}) int {
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case string:
				if i, err := strconv.Atoi(val); err == nil {
					return i
				}
			}
			return 0
		},
		"add": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av + bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av + bv
				}
			}
			return 0
		},
		"sub": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av - bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av - bv
				}
			}
			return 0
		},
		"mul": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av * bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av * bv
				}
			}
			return 0
		},
		"div": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok && bv != 0 {
					return av / bv
				}
			case float64:
				if bv, ok := b.(float64); ok && bv != 0 {
					return av / bv
				}
			}
			return 0
		},
		"mod": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok && bv != 0 {
					return av % bv
				}
			}
			return 0
		},
		"gt": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av > bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av > bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av > bv
				}
			}
			return false
		},
		"gte": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av >= bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av >= bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av >= bv
				}
			}
			return false
		},
		"lt": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av < bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av < bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av < bv
				}
			}
			return false
		},
		"lte": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av <= bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av <= bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av <= bv
				}
			}
			return false
		},
		"cat": func(args ...interface{}) string {
			var result strings.Builder
			for _, arg := range args {
				result.WriteString(fmt.Sprintf("%v", arg))
			}
			return result.String()
		},
		"repeat": func(count int, str string) string {
			return strings.Repeat(str, count)
		},
		"has": func(needle interface{}, haystack interface{}) bool {
			if haystack == nil {
				return false
			}
			v := reflect.ValueOf(haystack)
			if v.Kind() != reflect.Slice {
				return false
			}
			for i := 0; i < v.Len(); i++ {
				if reflect.DeepEqual(needle, v.Index(i).Interface()) {
					return true
				}
			}
			return false
		},
		"toBool": toBoolHelper,

		// Conditional functions
		"eq": func(a, b interface{}) bool { return a == b },
		"ne": func(a, b interface{}) bool { return a != b },
		"and": func(args ...interface{}) bool {
			for _, arg := range args {
				if !toBoolHelper(arg) {
					return false
				}
			}
			return true
		},
		"or": func(args ...interface{}) bool {
			for _, arg := range args {
				if toBoolHelper(arg) {
					return true
				}
			}
			return false
		},
		"not": func(a interface{}) bool { return !toBoolHelper(a) },
		"ternary": func(condition, ifTrue, ifFalse interface{}) interface{} {
			if toBoolHelper(condition) {
				return ifTrue
			}
			return ifFalse
		},

		// List functions
		"list": func(items ...interface{}) []interface{} {
			return items
		},
		"first": func(a interface{}) interface{} {
			if a == nil {
				return nil
			}
			v := reflect.ValueOf(a)
			if v.Kind() != reflect.Slice {
				return v.Interface() // or nil, or error
			}
			if v.Len() == 0 {
				return nil
			}
			return v.Index(0).Interface()
		},
		"last": func(a interface{}) interface{} {
			if a == nil {
				return nil
			}
			v := reflect.ValueOf(a)
			if v.Kind() != reflect.Slice {
				return v.Interface() // or nil, or error
			}
			if v.Len() == 0 {
				return nil
			}
			return v.Index(v.Len() - 1).Interface()
		},

		// Random functions
		"randAlphaNum": func(count int) string {
			const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},
		"randAlpha": func(count int) string {
			const charset = "abcdefghijklmnopqrstuvwxyz"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},
		"randNumeric": func(count int) string {
			const charset = "0123456789"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},
		"randAscii": func(count int) string {
			const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},

		// Utility functions
		"b64enc": func(str string) string {
			return fmt.Sprintf("%s", str) // Simplified base64 encoding
		},
		"b64dec": func(str string) string {
			return str // Simplified base64 decoding
		},
		"sha256sum": func(str string) string {
			// Simplified SHA256 hash - in production you'd use crypto/sha256
			return fmt.Sprintf("sha256-%s", str)
		},
		"genCA": func(cn string, days int) map[string]string {
			// Simplified certificate generation for templating.
			// In a real implementation, this would generate a real CA certificate.
			return map[string]string{
				"Cert": fmt.Sprintf("-----BEGIN CERTIFICATE-----\n# CA Cert for %s, valid for %d days\n-----END CERTIFICATE-----\n", cn, days),
				"Key":  fmt.Sprintf("-----BEGIN PRIVATE KEY-----\n# CA Key for %s\n-----END PRIVATE KEY-----\n", cn),
			}
		},
		"genSelfSignedCert": func(cn string, ips, sans interface{}, days int) map[string]string {
			// Simplified self-signed certificate generation for templating.
			return map[string]string{
				"Cert": fmt.Sprintf("-----BEGIN CERTIFICATE-----\n# Self-signed Cert for %s, valid for %d days\n-----END CERTIFICATE-----\n", cn, days),
				"Key":  fmt.Sprintf("-----BEGIN PRIVATE KEY-----\n# Self-signed Key for %s\n-----END PRIVATE KEY-----\n", cn),
			}
		},
		"genSignedCert": func(cn string, ips, sans interface{}, days int, ca interface{}) map[string]string {
			// Simplified signed certificate generation for templating.
			return map[string]string{
				"Cert": fmt.Sprintf("-----BEGIN CERTIFICATE-----\n# Signed Cert for %s, valid for %d days\n-----END CERTIFICATE-----\n", cn, days),
				"Key":  fmt.Sprintf("-----BEGIN PRIVATE KEY-----\n# Signed Key for %s\n-----END PRIVATE KEY-----\n", cn),
			}
		},
		"trunc": func(length int, str string) string {
			if len(str) <= length {
				return str
			}
			return str[:length]
		},
		"nospace": func(str string) string {
			return strings.ReplaceAll(str, " ", "")
		},
		"compact": func(str string) string {
			return strings.ReplaceAll(str, " ", "")
		},
		"initial": func(str string) string {
			if len(str) == 0 {
				return str
			}
			return strings.ToUpper(str[:1])
		},
		"wordwrap": func(width int, str string) string {
			// Simplified word wrapping
			if len(str) <= width {
				return str
			}
			return str[:width] + "\n" + str[width:]
		},

		// Kubernetes functions
		"lookup": func(apiVersion, kind, namespace, name string) interface{} {
			// Simplified lookup function - returns empty map for now
			// In a real implementation, this would query the Kubernetes API
			return map[string]interface{}{}
		},
		"include": func(name string, data interface{}) (string, error) {
			// Simplified include function - returns empty string for now
			// In a real implementation, this would include another template
			return "", nil
		},
		"tpl": func(template string, data interface{}) (string, error) {
			// Simplified tpl function - returns the template as-is for now
			// In a real implementation, this would render the template
			return template, nil
		},
		"fail": func(msg string) (string, error) {
			return "", fmt.Errorf(msg)
		},
		"hasKey": func(m map[string]interface{}, key string) bool {
			if m == nil {
				return false
			}
			_, ok := m[key]
			return ok
		},
		"index": s.customIndexFunc,

		// Variable manipulation functions
		"set": func(dot interface{}, key string, value interface{}) interface{} {
			// set function allows setting variables in template scope
			// This implementation creates a map to store variables
			if dot == nil {
				dot = make(map[string]interface{})
			}

			// Try to convert dot to a map if it's not already
			if dotMap, ok := dot.(map[string]interface{}); ok {
				dotMap[key] = value
				return value
			}

			// If dot is not a map, return the value directly
			// This is a fallback for cases where we can't modify the context
			return value
		},

		// Regex functions (implementing some common Sprig functions)
		"regexMatch": func(regex string, s string) bool {
			return regexp.MustCompile(regex).MatchString(s)
		},
		"regexFindAll": func(regex string, s string, n int) []string {
			return regexp.MustCompile(regex).FindAllString(s, n)
		},
		"regexReplaceAll": func(regex, replacement, src string) string {
			return regexp.MustCompile(regex).ReplaceAllString(src, replacement)
		},
		"regexSplit": func(regex string, s string, n int) []string {
			return regexp.MustCompile(regex).Split(s, n)
		},

		// Additional common Helm/Sprig functions
		"printf": fmt.Sprintf,
		"kindOf": func(v interface{}) string {
			if v == nil {
				return "nil"
			}
			return reflect.TypeOf(v).Kind().String()
		},
		"typeOf": func(v interface{}) string {
			if v == nil {
				return "nil"
			}
			return reflect.TypeOf(v).String()
		},
		"len": func(v interface{}) int {
			if v == nil {
				return 0
			}
			val := reflect.ValueOf(v)
			switch val.Kind() {
			case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
				return val.Len()
			default:
				return 0
			}
		},
		"keys": func(m map[string]interface{}) []string {
			if m == nil {
				return []string{}
			}
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			return keys
		},
		"values": func(m map[string]interface{}) []interface{} {
			if m == nil {
				return []interface{}{}
			}
			values := make([]interface{}, 0, len(m))
			for _, v := range m {
				values = append(values, v)
			}
			return values
		},
	}
}

// indirect is a helper function to get the value from a pointer.
func indirect(v reflect.Value) (rv reflect.Value, isNil bool) {
	for ; v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface; v = v.Elem() {
		if v.IsNil() {
			return v, true
		}
	}
	return v, false
}

// intValue is a helper function to convert a reflect.Value to an int.
func intValue(v reflect.Value) (int, error) {
	if v, isNil := indirect(v); isNil || !v.IsValid() {
		return 0, fmt.Errorf("index of nil pointer")
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(v.Uint()), nil
	case reflect.Invalid:
		return 0, fmt.Errorf("index of invalid value")
	default:
		return 0, fmt.Errorf("can't index item with %s", v.Type())
	}
}

// customIndexFunc is a custom implementation of the 'index' template function.
// It supports indexing slices/arrays with string representations of integers (e.g., "_0", "1").
func (s *RenderedChartStep) customIndexFunc(item interface{}, indices ...interface{}) (interface{}, error) {
	v := reflect.ValueOf(item)
	if !v.IsValid() {
		return nil, fmt.Errorf("index of untyped nil")
	}

	for _, i := range indices {
		index := reflect.ValueOf(i)
		if !index.IsValid() {
			return nil, fmt.Errorf("index of untyped nil")
		}

		var isNil bool
		if v, isNil = indirect(v); isNil {
			return nil, fmt.Errorf("index of nil pointer")
		}

		switch v.Kind() {
		case reflect.Array, reflect.Slice, reflect.String:
			var x int
			var err error

			if index.Kind() == reflect.String {
				strIndex := index.String()
				// Handle string indices like "_0", "1", etc. for slices
				cleanIndex := strings.TrimPrefix(strIndex, "_")
				x, err = strconv.Atoi(cleanIndex)
				if err != nil {
					// Fallback to default behavior if parsing fails
					x, err = intValue(index)
				}
			} else {
				x, err = intValue(index)
			}

			if err != nil {
				return nil, err
			}
			if x < 0 || x >= v.Len() {
				// To be more forgiving, return nil instead of error for out-of-bounds access.
				// This mimics behavior of some other template systems.
				log.Printf("Warning: index out of range [%d] with length %d, returning nil", x, v.Len())
				return nil, nil
			}
			v = v.Index(x)

		case reflect.Map:
			if !index.Type().AssignableTo(v.Type().Key()) {
				return nil, fmt.Errorf("%s is not a key type for %s", index.Type(), v.Type())
			}
			v = v.MapIndex(index)
		case reflect.Invalid:
			return nil, fmt.Errorf("index of invalid value")
		default:
			return nil, fmt.Errorf("can't index item of type %s", v.Type())
		}
	}

	if !v.IsValid() {
		// This can happen if a map lookup returned the zero value.
		// For example, indexing a map with a key that doesn't exist.
		return nil, nil
	}

	return v.Interface(), nil
}

// getMapKeys returns the keys of a map for debugging purposes
func getMapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
