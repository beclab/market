package utils

import (
	"encoding/json"
	"log"
	"market/internal/v2/types"
	"reflect"
	"time"
)

// convertApplicationInfoEntryToMap converts ApplicationInfoEntry to map for task creation
func ConvertApplicationInfoEntryToMap(entry *types.ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		log.Printf("DEBUG: convertApplicationInfoEntryToMap called with nil entry")
		return make(map[string]interface{})
	}

	log.Printf("DEBUG: Converting ApplicationInfoEntry to map for app: %s", entry.Name)
	log.Printf("DEBUG: convertApplicationInfoEntryToMap - entry.SupportArch: %+v (length: %d)", entry.SupportArch, len(entry.SupportArch))

	result := map[string]interface{}{
		"id":          entry.ID,
		"name":        entry.Name,
		"cfgType":     entry.CfgType,
		"chartName":   entry.ChartName,
		"icon":        entry.Icon,
		"appID":       entry.AppID,
		"version":     entry.Version,
		"apiVersion":  entry.ApiVersion,
		"categories":  entry.Categories,
		"versionName": entry.VersionName,

		"promoteImage":   entry.PromoteImage,
		"promoteVideo":   entry.PromoteVideo,
		"subCategory":    entry.SubCategory,
		"locale":         entry.Locale,
		"developer":      entry.Developer,
		"requiredMemory": entry.RequiredMemory,
		"requiredDisk":   entry.RequiredDisk,
		"supportArch":    entry.SupportArch,
		"requiredGPU":    entry.RequiredGPU,
		"requiredCPU":    entry.RequiredCPU,
		"rating":         entry.Rating,
		"target":         entry.Target,

		"submitter":     entry.Submitter,
		"doc":           entry.Doc,
		"website":       entry.Website,
		"featuredImage": entry.FeaturedImage,
		"sourceCode":    entry.SourceCode,

		"modelSize": entry.ModelSize,
		"namespace": entry.Namespace,
		"onlyAdmin": entry.OnlyAdmin,

		"lastCommitHash": entry.LastCommitHash,
		"createTime":     entry.CreateTime,
		"updateTime":     entry.UpdateTime,
		"appLabels":      entry.AppLabels,

		"screenshots": entry.Screenshots,
		"tags":        entry.Tags,
		"updated_at":  entry.UpdatedAt,
	}
	// marshal basic fields
	if _, err := json.Marshal(result); err != nil {
		log.Printf("DEBUG: marshal basic fields: %v", err)
	}

	if entry.Description != nil {
		descCopy := make(map[string]string)
		for k, v := range entry.Description {
			descCopy[k] = v
		}
		result["description"] = descCopy
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal description: %v", err)
		}
	}

	if entry.Title != nil {
		titleCopy := make(map[string]string)
		for k, v := range entry.Title {
			titleCopy[k] = v
		}
		result["title"] = titleCopy
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal title: %v", err)
		}
	}

	if entry.FullDescription != nil {
		fullDescCopy := make(map[string]string)
		for k, v := range entry.FullDescription {
			fullDescCopy[k] = v
		}
		result["fullDescription"] = fullDescCopy
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal fullDescription: %v", err)
		}
	}

	if entry.UpgradeDescription != nil {
		upgradeDescCopy := make(map[string]string)
		for k, v := range entry.UpgradeDescription {
			upgradeDescCopy[k] = v
		}
		result["upgradeDescription"] = upgradeDescCopy
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal upgradeDescription: %v", err)
		}
	}

	if entry.SupportClient != nil {
		result["supportClient"] = deepSafeCopy(entry.SupportClient)
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal supportClient: %v", err)
		}
	}

	if entry.Permission != nil {
		result["permission"] = deepSafeCopy(entry.Permission)
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal permission: %v", err)
		}
	}

	if entry.Middleware != nil {
		result["middleware"] = deepSafeCopy(entry.Middleware)
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal middleware: %v", err)
		}
	}

	if entry.Options != nil {
		result["options"] = deepSafeCopy(entry.Options)
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal options: %v", err)
		}
	}

	if entry.Entrances != nil {
		safeEntrances := make([]map[string]interface{}, 0, len(entry.Entrances))
		for _, entrance := range entry.Entrances {
			if safeEntrance := deepSafeCopy(entrance); safeEntrance != nil {
				safeEntrances = append(safeEntrances, safeEntrance)
			}
		}
		if len(safeEntrances) > 0 {
			result["entrances"] = safeEntrances
			if _, err := json.Marshal(result); err != nil {
				log.Printf("DEBUG: marshal entrances: %v", err)
			}
		}
	}

	if entry.License != nil {
		safeLicenses := make([]map[string]interface{}, 0, len(entry.License))
		for _, license := range entry.License {
			if safeLicense := deepSafeCopy(license); safeLicense != nil {
				safeLicenses = append(safeLicenses, safeLicense)
			}
		}
		if len(safeLicenses) > 0 {
			result["license"] = safeLicenses
			if _, err := json.Marshal(result); err != nil {
				log.Printf("DEBUG: marshal license: %v", err)
			}
		}
	}

	if entry.Legal != nil {
		safeLegals := make([]map[string]interface{}, 0, len(entry.Legal))
		for _, legal := range entry.Legal {
			if safeLegal := deepSafeCopy(legal); safeLegal != nil {
				safeLegals = append(safeLegals, safeLegal)
			}
		}
		if len(safeLegals) > 0 {
			result["legal"] = safeLegals
			if _, err := json.Marshal(result); err != nil {
				log.Printf("DEBUG: marshal legal: %v", err)
			}
		}
	}

	if entry.I18n != nil {
		result["i18n"] = deepSafeCopy(entry.I18n)
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal i18n: %v", err)
		}
	}

	if entry.Count != nil {
		result["count"] = safeCopyCount(entry.Count)
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal count: %v", err)
		}
	}

	if entry.VersionHistory != nil {
		safeVersionHistory := make([]map[string]interface{}, 0, len(entry.VersionHistory))
		for _, versionInfo := range entry.VersionHistory {
			if versionInfo != nil {
				versionInfoMap := map[string]interface{}{
					"appName":            versionInfo.AppName,
					"version":            versionInfo.Version,
					"versionName":        versionInfo.VersionName,
					"upgradeDescription": versionInfo.UpgradeDescription,
				}
				if versionInfo.MergedAt != nil {
					versionInfoMap["mergedAt"] = versionInfo.MergedAt.Format(time.RFC3339)
				}
				safeVersionHistory = append(safeVersionHistory, versionInfoMap)
			}
		}
		if len(safeVersionHistory) > 0 {
			result["versionHistory"] = safeVersionHistory
			if _, err := json.Marshal(result); err != nil {
				log.Printf("DEBUG: marshal versionHistory: %v", err)
			}
		}
	}

	if entry.SubCharts != nil {
		safeSubCharts := make([]map[string]interface{}, 0, len(entry.SubCharts))
		for _, subChart := range entry.SubCharts {
			if subChart != nil {
				subChartMap := map[string]interface{}{
					"name":   subChart.Name,
					"shared": subChart.Shared,
				}
				safeSubCharts = append(safeSubCharts, subChartMap)
			}
		}
		if len(safeSubCharts) > 0 {
			result["subCharts"] = safeSubCharts
			if _, err := json.Marshal(result); err != nil {
				log.Printf("DEBUG: marshal subCharts: %v", err)
			}
		}
	}

	if entry.Metadata != nil {
		log.Printf("DEBUG: Processing Metadata field, length: %d", len(entry.Metadata))
		metadataCopy := convertMetadataToMap(entry.Metadata)
		result["metadata"] = metadataCopy
		log.Printf("DEBUG: Metadata copy completed, length: %d", len(metadataCopy))
		if _, err := json.Marshal(result); err != nil {
			log.Printf("DEBUG: marshal metadata: %v", err)
		}
	}

	return result
}

// deepSafeCopy creates a deep copy of map[string]interface{} avoiding circular references
func deepSafeCopy(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	// Use a visited map to detect circular references, key is uintptr (map pointer)
	visited := make(map[uintptr]bool)
	return deepSafeCopyWithVisited(src, visited)
}

// deepSafeCopyWithVisited creates a deep copy with circular reference detection
func deepSafeCopyWithVisited(src map[string]interface{}, visited map[uintptr]bool) map[string]interface{} {
	if src == nil {
		return nil
	}
	// Use reflect to get the unique pointer of the map
	ptr := reflect.ValueOf(src).Pointer()
	if visited[ptr] {
		log.Printf("DEBUG: Detected circular reference in deepSafeCopy, skipping")
		return nil
	}
	visited[ptr] = true
	defer delete(visited, ptr)

	dst := make(map[string]interface{})
	for k, v := range src {
		// Skip potential circular reference keys
		if k == "source_data" || k == "raw_data" || k == "app_info" || k == "parent" || k == "self" ||
			k == "circular_ref" || k == "back_ref" || k == "loop" {
			log.Printf("DEBUG: Skipping potential circular reference key: %s", k)
			continue
		}

		switch val := v.(type) {
		case string, int, int64, float64, bool:
			dst[k] = val
		case []string:
			// Copy string slice
			dst[k] = append([]string{}, val...)
		case []interface{}:
			// Only copy simple types from interface slice
			safeSlice := make([]interface{}, 0, len(val))
			for _, item := range val {
				switch item.(type) {
				case string, int, int64, float64, bool:
					safeSlice = append(safeSlice, item)
				default:
					log.Printf("DEBUG: Skipping complex slice item in deepSafeCopy for key %s", k)
				}
			}
			if len(safeSlice) > 0 {
				dst[k] = safeSlice
			}
		case map[string]interface{}:
			// Recursively copy nested map with visited tracking
			if nestedCopy := deepSafeCopyWithVisited(val, visited); nestedCopy != nil {
				dst[k] = nestedCopy
			}
		case []map[string]interface{}:
			// Copy slice of maps with visited tracking
			safeSlice := make([]map[string]interface{}, 0, len(val))
			for _, item := range val {
				if itemCopy := deepSafeCopyWithVisited(item, visited); itemCopy != nil {
					safeSlice = append(safeSlice, itemCopy)
				}
			}
			if len(safeSlice) > 0 {
				dst[k] = safeSlice
			}
		default:
			// Skip complex types and log for debugging
			log.Printf("DEBUG: Skipping complex type in deepSafeCopy for key %s: %T", k, v)
		}
	}
	return dst
}

// safeCopyCount safely copies the Count field which is interface{} type
func safeCopyCount(count interface{}) interface{} {
	if count == nil {
		return nil
	}

	switch val := count.(type) {
	case string, int, int64, float64, bool:
		// Simple types are safe to copy directly
		return val
	case []string:
		// String slice is safe to copy
		return append([]string{}, val...)
	case map[string]interface{}:
		// Use deepSafeCopy for map[string]interface{}
		return deepSafeCopy(val)
	case []map[string]interface{}:
		// Handle slice of maps
		safeSlice := make([]map[string]interface{}, 0, len(val))
		for _, item := range val {
			if safeItem := deepSafeCopy(item); safeItem != nil {
				safeSlice = append(safeSlice, safeItem)
			}
		}
		if len(safeSlice) > 0 {
			return safeSlice
		}
		return nil
	case []interface{}:
		// Handle interface slice with simple types only
		safeSlice := make([]interface{}, 0, len(val))
		for _, item := range val {
			switch item.(type) {
			case string, int, int64, float64, bool:
				safeSlice = append(safeSlice, item)
			default:
				log.Printf("DEBUG: Skipping complex Count slice item: %T", item)
			}
		}
		if len(safeSlice) > 0 {
			return safeSlice
		}
		return nil
	default:
		// For any other type, log and return nil to be safe
		log.Printf("DEBUG: Skipping complex Count type: %T", val)
		return nil
	}
}

// convertMetadataToMap converts metadata to map[string]interface{} for compatibility
func convertMetadataToMap(metadata map[string]interface{}) map[string]interface{} {
	safeMetadata := make(map[string]interface{})
	for k, v := range metadata {
		// Skip any potential circular references and complex nested structures
		if k != "source_data" && k != "raw_data" && k != "app_info" && k != "parent" && k != "self" {
			// Only copy simple types to avoid circular references
			switch val := v.(type) {
			case string, int, int64, float64, bool, []string:
				log.Printf("DEBUG: Metadata[%s] = %v (type: %T)", k, v, v)
				safeMetadata[k] = v
			case []interface{}:
				// Only allow simple types in slices
				safeSlice := make([]interface{}, 0, len(val))
				for _, item := range val {
					switch item.(type) {
					case string, int, int64, float64, bool:
						safeSlice = append(safeSlice, item)
					default:
						log.Printf("DEBUG: Skipping complex slice item in Metadata[%s]", k)
					}
				}
				if len(safeSlice) > 0 {
					safeMetadata[k] = safeSlice
				}
			case map[string]interface{}:
				// Create a shallow copy of nested maps, excluding problematic keys
				nestedCopy := make(map[string]interface{})
				for nk, nv := range val {
					if nk != "source_data" && nk != "raw_data" && nk != "app_info" && nk != "parent" && nk != "self" {
						// Only allow simple types in nested maps
						switch nv.(type) {
						case string, int, int64, float64, bool, []string:
							nestedCopy[nk] = nv
						default:
							log.Printf("DEBUG: Skipping complex nested value in Metadata[%s][%s]", k, nk)
						}
					}
				}
				if len(nestedCopy) > 0 {
					safeMetadata[k] = nestedCopy
				}
			default:
				log.Printf("DEBUG: Skipping Metadata[%s] with complex type %T", k, v)
			}
		} else {
			log.Printf("DEBUG: Skipping Metadata[%s] to avoid circular reference", k)
		}
	}
	return safeMetadata
}
