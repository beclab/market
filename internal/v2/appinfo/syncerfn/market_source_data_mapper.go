package syncerfn

import (
	"sort"
	"strconv"
	"time"

	"market/internal/v2/types"
)

// BuildMarketSourceData converts /appstore/info payload into the single JSONB
// snapshot persisted in market_sources.data.
func BuildMarketSourceData(resp *AppStoreInfoResponse) *types.MarketSourceData {
	if resp == nil {
		return nil
	}

	result := &types.MarketSourceData{
		Apps: make([]string, 0),
		Others: types.Others{
			Hash:       resp.Hash,
			Version:    resp.Version,
			Topics:     make([]*types.Topic, 0),
			TopicLists: make([]*types.TopicList, 0),
			Recommends: make([]*types.Recommend, 0),
			Pages:      make([]*types.Page, 0),
			Tops:       make([]*types.AppStoreTopItem, 0),
			Latest:     append([]string(nil), resp.Data.Latest...),
			Tags:       make([]*types.Tag, 0),
		},
	}

	if !resp.LastUpdated.IsZero() {
		result.LastUpdated = resp.LastUpdated.UTC().Format(time.RFC3339Nano)
	}

	for appID, appRaw := range resp.Data.Apps {
		if appID == "" {
			continue
		}
		appMap, ok := appRaw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := appMap["id"].(string)
		if id != "" && id != appID {
			continue
		}
		result.Apps = append(result.Apps, appID)
	}
	sort.Strings(result.Apps)

	for _, topic := range resp.Data.Topics {
		if isZeroRemoteTopic(topic) {
			continue
		}
		mapped := &types.Topic{
			ID:        topic.ID,
			Name:      topic.Name,
			Data:      make(map[string]*types.TopicData),
			Source:    parseIntSource(topic.Source),
			UpdatedAt: parseRFC3339Loose(topic.UpdatedAt),
			CreatedAt: parseRFC3339Loose(topic.CreatedAt),
		}
		for lang, td := range topic.Data {
			copied := td
			mapped.Data[lang] = &types.TopicData{
				Group:           copied.Group,
				Title:           copied.Title,
				Des:             copied.Des,
				IconImg:         copied.IconImg,
				DetailImg:       copied.DetailImg,
				RichText:        copied.RichText,
				MobileDetailImg: copied.MobileDetailImg,
				MobileRichText:  copied.MobileRichText,
				BackgroundColor: copied.BackgroundColor,
				Apps:            copied.Apps,
				IsDelete:        copied.IsDelete,
			}
		}
		result.Topics = append(result.Topics, mapped)
	}

	for _, topicList := range resp.Data.TopicLists {
		if isZeroRemoteTopicList(topicList) {
			continue
		}
		result.TopicLists = append(result.TopicLists, &types.TopicList{
			Name:        topicList.Name,
			Type:        topicList.Type,
			Description: topicList.Description,
			Content:     topicList.Content,
			Title:       toStringMap(topicList.Title),
			Source:      parseIntSource(topicList.Source),
			UpdatedAt:   parseRFC3339Loose(topicList.UpdatedAt),
			CreatedAt:   parseRFC3339Loose(topicList.CreatedAt),
		})
	}

	for _, recommend := range resp.Data.Recommends {
		if isZeroRemoteRecommend(recommend) {
			continue
		}
		mapped := &types.Recommend{
			Name:        recommend.Name,
			Description: recommend.Description,
			Content:     recommend.Content,
			Source:      parseIntSource(recommend.Source),
			UpdatedAt:   parseRFC3339Loose(recommend.UpdatedAt),
			CreatedAt:   parseRFC3339Loose(recommend.CreatedAt),
		}
		if recommend.Data != nil {
			mapped.Data = &types.RecommendData{
				Title:       toStringMap(recommend.Data.Title),
				Description: toStringMap(recommend.Data.Description),
			}
		}
		result.Recommends = append(result.Recommends, mapped)
	}

	for _, page := range resp.Data.Pages {
		if isZeroRemotePage(page) {
			continue
		}
		result.Pages = append(result.Pages, &types.Page{
			Category:  page.Category,
			Content:   page.Content,
			Source:    parseIntSource(page.Source),
			UpdatedAt: parseRFC3339Loose(page.UpdatedAt),
			CreatedAt: parseRFC3339Loose(page.CreatedAt),
		})
	}

	for _, top := range resp.Data.Tops {
		appID := top.AppID
		if appID == "" {
			appID = top.LegacyAppID
		}
		if appID == "" {
			continue
		}
		result.Tops = append(result.Tops, &types.AppStoreTopItem{
			AppID: appID,
			Rank:  int(top.Rank),
		})
	}

	for _, tag := range resp.Data.Tags {
		if isZeroRemoteTag(tag) {
			continue
		}
		result.Tags = append(result.Tags, &types.Tag{
			ID:        tag.ID,
			Name:      tag.Name,
			Title:     toStringMap(tag.Title),
			Icon:      tag.Icon,
			Sort:      tag.Sort,
			Source:    parseIntSource(tag.Source),
			UpdatedAt: parseRFC3339Loose(tag.UpdatedAt),
			CreatedAt: parseRFC3339Loose(tag.CreatedAt),
		})
	}

	return result
}

func isZeroRemoteTopic(topic RemoteTopic) bool {
	return topic.ID == "" && topic.Name == "" && len(topic.Data) == 0
}

func isZeroRemoteTopicList(topicList RemoteTopicList) bool {
	return topicList.Name == "" &&
		topicList.Type == "" &&
		topicList.Description == "" &&
		topicList.Content == "" &&
		len(topicList.Title) == 0
}

func isZeroRemoteRecommend(recommend RemoteRecommend) bool {
	return recommend.Name == "" &&
		recommend.Description == "" &&
		recommend.Content == "" &&
		recommend.Data == nil
}

func isZeroRemotePage(page RemotePage) bool {
	return page.Category == "" && page.Content == ""
}

func isZeroRemoteTag(tag RemoteTag) bool {
	return tag.ID == "" &&
		tag.Name == "" &&
		tag.Icon == "" &&
		tag.Sort == 0 &&
		len(tag.Title) == 0
}

func toStringMap(in map[string]interface{}) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		switch v := value.(type) {
		case string:
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseRFC3339Loose(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed
	}
	return time.Time{}
}

func parseIntSource(raw string) int {
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}
