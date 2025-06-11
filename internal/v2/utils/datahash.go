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
// hash计算限制以防止性能问题
const (
	MaxDataComponentSize = 10 * 1024 * 1024 // 10MB per component
	MaxTotalDataSize     = 50 * 1024 * 1024 // 50MB total
)

// Note: Import types would be needed, but since this is in utils package,
// we need to define interfaces to avoid circular imports
// 注意：需要导入types，但由于这是在utils包中，我们需要定义接口以避免循环导入

// UserDataInterface represents the UserData structure for hash calculation
// UserDataInterface 表示用于hash计算的UserData结构
type UserDataInterface interface {
	GetSources() map[string]SourceDataInterface
	GetHash() string
	SetHash(hash string)
}

// SourceDataInterface represents the SourceData structure for hash calculation
// SourceDataInterface 表示用于hash计算的SourceData结构
type SourceDataInterface interface {
	GetAppStateLatest() []interface{}
	GetAppInfoLatest() []interface{}
	GetOthers() OthersInterface
}

// OthersInterface represents the Others structure for hash calculation
// OthersInterface 表示用于hash计算的Others结构
type OthersInterface interface {
	GetTopics() []interface{}
	GetTopicLists() []interface{}
	GetRecommends() []interface{}
	GetPages() []interface{}
}

// CalculateUserDataHash calculates hash for entire user data
// CalculateUserDataHash 计算整个用户数据的hash
func CalculateUserDataHash(userData UserDataInterface) (string, error) {
	sources := userData.GetSources()
	if len(sources) == 0 {
		glog.V(2).Infof("No sources found in user data")
		return "", nil
	}

	// Get sorted source IDs for consistent hashing
	// 获取排序后的源ID以确保hash的一致性
	sourceIDs := make([]string, 0, len(sources))
	for sourceID := range sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)

	// Calculate hash for each source and collect them
	// 计算每个源的hash并收集它们
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
			// 检查总大小限制
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
	// 拼接所有源hash并创建最终hash
	concatenated := strings.Join(sourceHashes, "")
	finalHash := sha256.Sum256([]byte(concatenated))
	return hex.EncodeToString(finalHash[:]), nil
}

// CalculateSourceDataHash calculates hash for a single source
// CalculateSourceDataHash 计算单个源数据的hash
func CalculateSourceDataHash(sourceData SourceDataInterface) (string, error) {
	hash, _, err := CalculateSourceDataHashWithSize(sourceData)
	return hash, err
}

// CalculateSourceDataHashWithSize calculates hash for a single source and returns data size
// CalculateSourceDataHashWithSize 计算单个源数据的hash并返回数据大小
func CalculateSourceDataHashWithSize(sourceData SourceDataInterface) (string, int, error) {
	// Extract the data components that need to be hashed
	// 提取需要hash的数据组件
	var dataComponents []string
	totalSize := 0

	// Helper function to safely marshal and check size
	// 安全序列化并检查大小的辅助函数
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
	// 拼接所有组件并创建hash
	concatenated := strings.Join(dataComponents, "")
	hash := sha256.Sum256([]byte(concatenated))
	return hex.EncodeToString(hash[:]), totalSize, nil
}
