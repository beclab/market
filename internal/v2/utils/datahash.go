package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/golang/glog"
)

// Hash calculation limits to prevent performance issues
const (
	MaxDataComponentSize = 10 * 1024 * 1024 // 10MB per component
	MaxTotalDataSize     = 50 * 1024 * 1024 // 50MB total
)

// Note: Import types would be needed, but since this is in utils package,
// we need to define interfaces to avoid circular imports

// UserDataInterface represents the UserData structure for hash calculation
type UserDataInterface interface {
	GetSources() map[string]SourceDataInterface
	GetHash() string
	SetHash(hash string)
}

// SourceDataInterface represents the SourceData structure for hash calculation
type SourceDataInterface interface {
	GetAppStateLatest() []interface{}
	GetAppInfoLatest() []interface{}
	GetOthers() OthersInterface
}

// OthersInterface represents the Others structure for hash calculation
type OthersInterface interface {
	GetTopics() []interface{}
	GetTopicLists() []interface{}
	GetRecommends() []interface{}
	GetPages() []interface{}
}

// CalculateUserDataHash calculates hash for entire user data
func CalculateUserDataHash(userData UserDataInterface) (string, error) {
	sources := userData.GetSources()
	if len(sources) == 0 {
		glog.V(2).Infof("No sources found in user data")
		return "", nil
	}

	// Get sorted source IDs for consistent hashing
	sourceIDs := make([]string, 0, len(sources))
	for sourceID := range sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)

	// Calculate hash for each source and collect them
	sourceHashes := make([]string, 0, len(sourceIDs))
	totalSize := 0

	for _, sourceID := range sourceIDs {
		sourceData := sources[sourceID]
		sourceHash, size, err := CalculateSourceDataHashWithSize(sourceData)
		if err != nil {
			glog.Errorf("Failed to calculate hash for source %s: %v", sourceID, err)
			continue
		}
		if sourceHash != "" {
			sourceHashes = append(sourceHashes, sourceHash)
			totalSize += size

			// Check total size limit
			if totalSize > MaxTotalDataSize {
				glog.Warningf("Total data size (%d bytes) exceeds limit (%d bytes), truncating hash calculation",
					totalSize, MaxTotalDataSize)
				break
			}
		}
	}

	if len(sourceHashes) == 0 {
		glog.V(2).Infof("No valid source hashes calculated")
		return "", nil
	}

	// Concatenate all source hashes and create final hash
	concatenated := strings.Join(sourceHashes, "")
	finalHash := sha256.Sum256([]byte(concatenated))
	return hex.EncodeToString(finalHash[:]), nil
}

// CalculateSourceDataHash calculates hash for a single source
func CalculateSourceDataHash(sourceData SourceDataInterface) (string, error) {
	hash, _, err := CalculateSourceDataHashWithSize(sourceData)
	return hash, err
}

// CalculateSourceDataHashWithSize calculates hash for a single source and returns data size
func CalculateSourceDataHashWithSize(sourceData SourceDataInterface) (string, int, error) {
	// Extract the data components that need to be hashed
	var dataComponents []string
	totalSize := 0

	// Helper function to safely marshal and check size
	marshalWithSizeCheck := func(data interface{}, name string) bool {
		if jsonBytes, err := json.Marshal(data); err == nil {
			size := len(jsonBytes)
			if size > MaxDataComponentSize {
				glog.Warningf("Skipping %s: size (%d bytes) exceeds limit (%d bytes)", name, size, MaxDataComponentSize)
				return false
			}
			dataComponents = append(dataComponents, string(jsonBytes))
			totalSize += size
			return true
		} else {
			glog.Warningf("Failed to marshal %s: %v", name, err)
			return false
		}
	}

	// 1. AppStateLatest
	appStateLatest := sourceData.GetAppStateLatest()
	if len(appStateLatest) > 0 {
		marshalWithSizeCheck(appStateLatest, "AppStateLatest")
	}

	// 2. AppInfoLatest
	appInfoLatest := sourceData.GetAppInfoLatest()
	if len(appInfoLatest) > 0 {
		marshalWithSizeCheck(appInfoLatest, "AppInfoLatest")
	}

	// 3. Others data (Topics, TopicLists, Recommends, Pages)
	others := sourceData.GetOthers()
	if others != nil {
		// Topics
		topics := others.GetTopics()
		if len(topics) > 0 {
			marshalWithSizeCheck(topics, "Topics")
		}

		// TopicLists
		topicLists := others.GetTopicLists()
		if len(topicLists) > 0 {
			marshalWithSizeCheck(topicLists, "TopicLists")
		}

		// Recommends
		recommends := others.GetRecommends()
		if len(recommends) > 0 {
			marshalWithSizeCheck(recommends, "Recommends")
		}

		// Pages
		pages := others.GetPages()
		if len(pages) > 0 {
			marshalWithSizeCheck(pages, "Pages")
		}
	}

	if len(dataComponents) == 0 {
		glog.V(2).Infof("No data components found for hash calculation")
		return "", 0, nil
	}

	// Concatenate all components and create hash
	concatenated := strings.Join(dataComponents, "")
	hash := sha256.Sum256([]byte(concatenated))
	return hex.EncodeToString(hash[:]), totalSize, nil
}
