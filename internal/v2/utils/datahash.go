package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"market/internal/v2/types"

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
	GetTags() []interface{}
}

// Snapshot data structures for lock-free hash calculation
type SourceDataSnapshot struct {
	AppStateLatest []interface{}
	AppInfoLatest  []interface{}
	Others         *OthersSnapshot
}

type OthersSnapshot struct {
	Topics     []interface{}
	TopicLists []interface{}
	Recommends []interface{}
	Pages      []interface{}
	Tags       []interface{}
}

type UserDataSnapshot struct {
	Hash    string
	Sources map[string]*SourceDataSnapshot
}

func (s *UserDataSnapshot) GetSources() map[string]SourceDataInterface {
	result := make(map[string]SourceDataInterface)
	for sourceID, sourceData := range s.Sources {
		result[sourceID] = sourceData
	}
	return result
}

func (s *UserDataSnapshot) GetHash() string {
	return s.Hash
}

func (s *UserDataSnapshot) SetHash(hash string) {
	s.Hash = hash
}

func (s *SourceDataSnapshot) GetAppStateLatest() []interface{} {
	return s.AppStateLatest
}

func (s *SourceDataSnapshot) GetAppInfoLatest() []interface{} {
	return s.AppInfoLatest
}

func (s *SourceDataSnapshot) GetOthers() OthersInterface {
	if s.Others == nil {
		return nil
	}
	return s.Others
}

func (s *OthersSnapshot) GetTopics() []interface{} {
	return s.Topics
}

func (s *OthersSnapshot) GetTopicLists() []interface{} {
	return s.TopicLists
}

func (s *OthersSnapshot) GetRecommends() []interface{} {
	return s.Recommends
}

func (s *OthersSnapshot) GetPages() []interface{} {
	return s.Pages
}

func (s *OthersSnapshot) GetTags() []interface{} {
	return s.Tags
}

// createUserDataSnapshot creates a snapshot of user data for hash calculation
func CreateUserDataSnapshot(userID string, userData *types.UserData) (*UserDataSnapshot, error) {
	// Create a lightweight snapshot for hash calculation
	snapshot := &UserDataSnapshot{
		Hash:    userData.Hash,
		Sources: make(map[string]*SourceDataSnapshot),
	}

	// Convert each source data to snapshot format
	for sourceID, sourceData := range userData.Sources {
		if sourceData == nil {
			continue
		}

		sourceSnapshot := &SourceDataSnapshot{
			AppStateLatest: make([]interface{}, len(sourceData.AppStateLatest)),
			AppInfoLatest:  make([]interface{}, len(sourceData.AppInfoLatest)),
		}

		// Convert AppStateLatest with safe copy
		for i, data := range sourceData.AppStateLatest {
			sourceSnapshot.AppStateLatest[i] = createSafeAppStateLatestCopy(data)
		}

		// Convert AppInfoLatest with safe copy
		for i, data := range sourceData.AppInfoLatest {
			sourceSnapshot.AppInfoLatest[i] = createSafeAppInfoLatestCopy(data)
		}

		// Convert Others data
		if sourceData.Others != nil {
			othersSnapshot := &OthersSnapshot{
				Topics:     make([]interface{}, len(sourceData.Others.Topics)),
				TopicLists: make([]interface{}, len(sourceData.Others.TopicLists)),
				Recommends: make([]interface{}, len(sourceData.Others.Recommends)),
				Pages:      make([]interface{}, len(sourceData.Others.Pages)),
				Tags:       make([]interface{}, len(sourceData.Others.Tags)),
			}

			// Convert each Others field with safe copy
			for i, topic := range sourceData.Others.Topics {
				othersSnapshot.Topics[i] = createSafeTopicCopy(topic)
			}
			for i, topicList := range sourceData.Others.TopicLists {
				othersSnapshot.TopicLists[i] = createSafeTopicListCopy(topicList)
			}
			for i, recommend := range sourceData.Others.Recommends {
				othersSnapshot.Recommends[i] = createSafeRecommendCopy(recommend)
			}
			for i, page := range sourceData.Others.Pages {
				othersSnapshot.Pages[i] = createSafePageCopy(page)
			}
			for i, tag := range sourceData.Others.Tags {
				othersSnapshot.Tags[i] = createSafeTagCopy(tag)
			}

			sourceSnapshot.Others = othersSnapshot
		}

		snapshot.Sources[sourceID] = sourceSnapshot
	}

	return snapshot, nil
}

// createSafeTagCopy creates a safe copy of Tag to avoid circular references
func createSafeTagCopy(tag *types.Tag) map[string]interface{} {
	if tag == nil {
		return nil
	}

	return map[string]interface{}{
		"_id":        tag.ID,
		"name":       tag.Name,
		"title":      tag.Title,
		"icon":       tag.Icon,
		"sort":       tag.Sort,
		"source":     tag.Source,
		"createdAt":  tag.CreatedAt,
		"updated_at": tag.UpdatedAt,
	}
}

// createSafePageCopy creates a safe copy of Page to avoid circular references
func createSafePageCopy(page *types.Page) map[string]interface{} {
	if page == nil {
		return nil
	}

	return map[string]interface{}{
		"category":   page.Category,
		"content":    page.Content,
		"createdAt":  page.CreatedAt,
		"updated_at": page.UpdatedAt,
	}
}

// createSafeRecommendCopy creates a safe copy of Recommend to avoid circular references
func createSafeRecommendCopy(recommend *types.Recommend) map[string]interface{} {
	if recommend == nil {
		return nil
	}

	copy := map[string]interface{}{
		"name":        recommend.Name,
		"description": recommend.Description,
		"content":     recommend.Content,
		"createdAt":   recommend.CreatedAt,
		"updated_at":  recommend.UpdatedAt,
	}

	// Add Data field if present
	if recommend.Data != nil {
		copy["data"] = map[string]interface{}{
			"title":       recommend.Data.Title,
			"description": recommend.Data.Description,
		}
	}

	// Add Source field if present
	if recommend.Source != "" {
		copy["source"] = recommend.Source
	}

	return copy
}

// createSafeTopicListCopy creates a safe copy of TopicList to avoid circular references
func createSafeTopicListCopy(topicList *types.TopicList) map[string]interface{} {
	if topicList == nil {
		return nil
	}

	return map[string]interface{}{
		"name":        topicList.Name,
		"type":        topicList.Type,
		"description": topicList.Description,
		"content":     topicList.Content,
		"title":       topicList.Title,
		"source":      topicList.Source,
		"createdAt":   topicList.CreatedAt,
		"updated_at":  topicList.UpdatedAt,
	}
}

// createSafeTopicCopy creates a safe copy of Topic to avoid circular references
func createSafeTopicCopy(topic *types.Topic) map[string]interface{} {
	if topic == nil {
		return nil
	}

	return map[string]interface{}{
		"_id":        topic.ID,
		"name":       topic.Name,
		"data":       topic.Data,
		"source":     topic.Source,
		"createdAt":  topic.CreatedAt,
		"updated_at": topic.UpdatedAt,
	}
}

// createSafeAppStateLatestCopy creates a safe copy of AppStateLatestData to avoid circular references
func createSafeAppStateLatestCopy(data *types.AppStateLatestData) map[string]interface{} {
	if data == nil {
		return nil
	}

	return map[string]interface{}{
		"type":    data.Type,
		"version": data.Version,
		"status": map[string]interface{}{
			"name":               data.Status.Name,
			"state":              data.Status.State,
			"updateTime":         data.Status.UpdateTime,
			"statusTime":         data.Status.StatusTime,
			"lastTransitionTime": data.Status.LastTransitionTime,
			"progress":           data.Status.Progress,
			"entranceStatuses":   data.Status.EntranceStatuses,
		},
	}
}

// createSafeAppInfoLatestCopy creates a safe copy of AppInfoLatestData to avoid circular references
func createSafeAppInfoLatestCopy(data *types.AppInfoLatestData) map[string]interface{} {
	if data == nil {
		return nil
	}

	safeCopy := map[string]interface{}{
		"type":             data.Type,
		"timestamp":        data.Timestamp,
		"version":          data.Version,
		"raw_package":      data.RawPackage,
		"rendered_package": data.RenderedPackage,
	}

	// Only include basic information from RawData to avoid cycles
	if data.RawData != nil {
		safeCopy["raw_data"] = createSafeApplicationInfoEntryCopy(data.RawData)
	}

	// Only include basic information from AppInfo to avoid cycles
	if data.AppInfo != nil {
		safeCopy["app_info"] = createSafeAppInfoCopy(data.AppInfo)
	}

	// Include Values if they exist
	if data.Values != nil {
		safeCopy["values"] = data.Values
	}

	// Include AppSimpleInfo if it exists
	if data.AppSimpleInfo != nil {
		safeCopy["app_simple_info"] = createSafeAppSimpleInfoCopy(data.AppSimpleInfo)
	}

	return safeCopy
}

// createSafeAppInfoCopy creates a safe copy of AppInfo to avoid circular references
func createSafeAppInfoCopy(appInfo *types.AppInfo) map[string]interface{} {
	if appInfo == nil {
		return nil
	}

	safeCopy := map[string]interface{}{}

	// Only include basic information from AppEntry to avoid cycles
	if appInfo.AppEntry != nil {
		safeCopy["app_entry"] = createSafeApplicationInfoEntryCopy(appInfo.AppEntry)
	}

	// Only include basic information from ImageAnalysis to avoid cycles
	if appInfo.ImageAnalysis != nil {
		safeCopy["image_analysis"] = map[string]interface{}{
			"app_id":       appInfo.ImageAnalysis.AppID,
			"user_id":      appInfo.ImageAnalysis.UserID,
			"source_id":    appInfo.ImageAnalysis.SourceID,
			"analyzed_at":  appInfo.ImageAnalysis.AnalyzedAt,
			"total_images": appInfo.ImageAnalysis.TotalImages,
			// Skip Images map to avoid cycles
		}
	}

	return safeCopy
}

// createSafeAppSimpleInfoCopy creates a safe copy of AppSimpleInfo to avoid circular references
func createSafeAppSimpleInfoCopy(info *types.AppSimpleInfo) map[string]interface{} {
	if info == nil {
		return nil
	}

	return map[string]interface{}{
		"app_id":          info.AppID,
		"app_name":        info.AppName,
		"app_icon":        info.AppIcon,
		"app_description": info.AppDescription,
		"app_version":     info.AppVersion,
		"app_title":       info.AppTitle,
		"categories":      info.Categories,
	}
}

// createSafeApplicationInfoEntryCopy creates a safe copy of ApplicationInfoEntry to avoid circular references
func createSafeApplicationInfoEntryCopy(entry *types.ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		return nil
	}

	return map[string]interface{}{
		"id":                 entry.ID,
		"name":               entry.Name,
		"cfgType":            entry.CfgType,
		"chartName":          entry.ChartName,
		"icon":               entry.Icon,
		"description":        entry.Description,
		"appID":              entry.AppID,
		"title":              entry.Title,
		"version":            entry.Version,
		"apiVersion":         entry.ApiVersion,
		"categories":         entry.Categories,
		"versionName":        entry.VersionName,
		"fullDescription":    entry.FullDescription,
		"upgradeDescription": entry.UpgradeDescription,
		"promoteImage":       entry.PromoteImage,
		"promoteVideo":       entry.PromoteVideo,
		"subCategory":        entry.SubCategory,
		"locale":             entry.Locale,
		"developer":          entry.Developer,
		"requiredMemory":     entry.RequiredMemory,
		"requiredDisk":       entry.RequiredDisk,
		"supportArch":        entry.SupportArch,
		"requiredGPU":        entry.RequiredGPU,
		"requiredCPU":        entry.RequiredCPU,
		"rating":             entry.Rating,
		"target":             entry.Target,
		"submitter":          entry.Submitter,
		"doc":                entry.Doc,
		"website":            entry.Website,
		"featuredImage":      entry.FeaturedImage,
		"sourceCode":         entry.SourceCode,
		"modelSize":          entry.ModelSize,
		"namespace":          entry.Namespace,
		"onlyAdmin":          entry.OnlyAdmin,
		"lastCommitHash":     entry.LastCommitHash,
		"createTime":         entry.CreateTime,
		"updateTime":         entry.UpdateTime,
		"appLabels":          entry.AppLabels,
		"screenshots":        entry.Screenshots,
		"tags":               entry.Tags,
		"updated_at":         entry.UpdatedAt,

		"supportClient":  convertToStringMapDW(entry.SupportClient),
		"permission":     convertToStringMapDW(entry.Permission),
		"middleware":     convertToStringMapDW(entry.Middleware),
		"options":        convertToStringMapDW(entry.Options),
		"i18n":           convertToStringMapDW(entry.I18n),
		"metadata":       convertToStringMapDW(entry.Metadata),
		"count":          entry.Count,
		"entrances":      entry.Entrances,
		"license":        entry.License,
		"legal":          entry.Legal,
		"versionHistory": entry.VersionHistory,
		"subCharts":      entry.SubCharts,
	}
}

func convertToStringMapDW(val interface{}) map[string]interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		return v
	case map[interface{}]interface{}:
		converted := make(map[string]interface{})
		for k, v2 := range v {
			if ks, ok := k.(string); ok {
				converted[ks] = v2
			}
		}
		return converted
	case nil:
		return nil
	default:
		return nil
	}
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

	// 3. Others data (Topics, TopicLists, Recommends, Pages, Tags)
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

		// Tags
		tags := others.GetTags()
		if len(tags) > 0 {
			marshalWithSizeCheck(tags, "Tags")
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
