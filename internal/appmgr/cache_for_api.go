package appmgr

import (
	"encoding/json"
	"market/internal/models"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
)

// Define a global variable to cache data
var cacheAppTypes *models.ListResultD
var cacheApplications []*models.ApplicationInfo
var cacheTopApplications []*models.ApplicationInfo
var cacheI18n = make(map[string]map[string]models.I18n)
var mu sync.Mutex

func updateCacheAppTypes() {

	res, err := getAppTypes()
	if err != nil {
		glog.Warningf("update cache app types failed: %s", err.Error())
		return
	}
	cacheAppTypes = res

	glog.Infof("-------> AppTypes: %s", cacheAppTypes.TotalCount)
}

func ReadCacheAppTypes() *models.ListResultD {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()
	return cacheAppTypes
}

func updateCacheApplications() {

	res, err := getApps("0", "0", "", "")
	if err != nil {
		glog.Warningf("update cache Applications failed: %s", err.Error())
		return
	}

	var appWithStatusList []*models.ApplicationInfo
	for _, item := range res.Items {
		info := &models.ApplicationInfo{}
		err := json.Unmarshal(item, info)
		if err != nil {
			glog.Warningf("err:%s", err.Error())
			continue
		}

		appWithStatusList = append(appWithStatusList, info)
	}

	cacheApplications = appWithStatusList

	glog.Infof("-------> Applications: %s", len(cacheApplications))
}

// containsCategory checks if the given category exists in the Categories slice
func containsCategory(categories interface{}, category string) bool {
	if categoryList, ok := categories.([]string); ok {
		for _, cat := range categoryList {
			if cat == category {
				return true
			}
		}
	}
	return false
}

// deepCopyApplications performs a deep copy of the given applications slice
func deepCopyApplications(apps []*models.ApplicationInfo) []*models.ApplicationInfo {
	var copiedApps []*models.ApplicationInfo
	for _, app := range apps {
		var copiedApp models.ApplicationInfo
		data, _ := json.Marshal(app)
		json.Unmarshal(data, &copiedApp)
		copiedApps = append(copiedApps, &copiedApp)
	}
	return copiedApps
}

// ReadCacheApplications retrieves applications from cache based on category and type
func ReadCacheApplications(page, size int, category, ty string) ([]*models.ApplicationInfo, int64) {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	var filteredApps []*models.ApplicationInfo
	var totalCount int64 // Counter for total matching applications

	// Filter applications based on category and cfgType
	for _, app := range cacheApplications {
		// Check if the app's Categories contains the specified category
		if containsCategory(app.Categories, category) && app.CfgType == ty {
			filteredApps = append(filteredApps, app)
			totalCount++ // Increment the count for each matching application
		}
	}

	// If page and size are both 0, return all filtered applications
	if page == 0 && size == 0 {
		return deepCopyApplications(filteredApps), totalCount
	}

	// Implement pagination
	start := page * size
	end := start + size
	if start > len(filteredApps) {
		return []*models.ApplicationInfo{}, totalCount // Return empty slice if start index is out of range
	}
	if end > len(filteredApps) {
		end = len(filteredApps) // Adjust end index if it exceeds the length
	}

	return deepCopyApplications(filteredApps[start:end]), totalCount // Return the paginated result and total count
}

func updateCacheTopApplications() {
	res, err := getTopApps("", "", "")
	if err != nil {
		glog.Warningf("update cache Top Applications failed: %s", err.Error())
		return
	}

	var appWithStatusList []*models.ApplicationInfo
	for _, item := range res.Items {
		info := &models.ApplicationInfo{}
		err := json.Unmarshal(item, info)
		if err != nil {
			glog.Warningf("err:%s", err.Error())
			continue
		}

		appWithStatusList = append(appWithStatusList, info)
	}

	cacheTopApplications = appWithStatusList

	glog.Infof("-------> TopApplications: %s", len(cacheTopApplications))
}

func ReadCacheTopApps(category, ty string, size int) ([]*models.ApplicationInfo, int64) {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	var filteredApps []*models.ApplicationInfo
	var totalCount int64

	for _, app := range cacheTopApplications {
		if app.CfgType != ty {
			continue
		}

		categories, ok := app.Categories.([]string)
		if !ok {
			continue
		}

		categoryMatch := false
		for _, cat := range categories {
			if cat == category {
				categoryMatch = true
				break
			}
		}

		if !categoryMatch {
			continue
		}

		filteredApps = append(filteredApps, app)
		totalCount++

		if size > 0 && len(filteredApps) >= size {
			break
		}
	}

	return deepCopyApplications(filteredApps), totalCount
}

func deepCopyApplication(app *models.ApplicationInfo) *models.ApplicationInfo {

	var copyApp models.ApplicationInfo

	data, err := json.Marshal(app)
	if err != nil {
		glog.Fatalf("Error marshaling application: %v", err)
	}

	err = json.Unmarshal(data, &copyApp)
	if err != nil {
		glog.Fatalf("Error unmarshaling application: %v", err)
	}

	return &copyApp
}

func ReadCacheApplication(name string) *models.ApplicationInfo {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	for _, app := range cacheApplications {
		if app.Name == name {
			return deepCopyApplication(app)
		}
	}
	return nil
}

func ReadCacheApplicationsWithMap(names []string) map[string]*models.ApplicationInfo {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	result := make(map[string]*models.ApplicationInfo)

	for _, name := range names {
		for _, app := range cacheApplications {
			if app.Name == name {
				copyApp := deepCopyApplication(app)
				result[name] = copyApp
				break
			}
		}
	}

	return result
}

func updateCacheI18n() {
	for _, app := range cacheApplications {
		i18nData := getAppI18n(app.ChartName, app.Locale)

		cacheI18n[app.ChartName] = i18nData
	}

	glog.Infof("-------> i18n: %s", len(cacheI18n))
}

func ReadCacheI18n(chartName string, locale []string) map[string]models.I18n {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	if i18nData, exists := cacheI18n[chartName]; exists {

		result := make(map[string]models.I18n)

		for _, loc := range locale {
			if i18n, ok := i18nData[loc]; ok {
				result[loc] = i18n
			}
		}

		return result
	}

	return nil
}

// SearchFromCache searches applications in cache based on a name condition
func SearchFromCache(page, size int, name string) (infos []*models.ApplicationInfo, count int64) {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	wildcardName := getWildcardName(name)
	var matchedApps []*models.ApplicationInfo

	// Iterate over cacheApplications to find matches
	for _, app := range cacheApplications {
		if matchesWildcard(app.Name, wildcardName) ||
			matchesWildcard(app.Title, wildcardName) ||
			matchesWildcard(app.Description, wildcardName) ||
			matchesWildcard(app.FullDescription, wildcardName) ||
			matchesWildcard(app.UpgradeDescription, wildcardName) ||
			matchesWildcard(app.Submitter, wildcardName) ||
			matchesWildcard(app.Developer, wildcardName) {
			matchedApps = append(matchedApps, app)
		}
	}

	// Calculate total count
	count = int64(len(matchedApps))

	// If page and size are both 0, return all matched applications
	if page == 0 && size == 0 {
		return deepCopyApplications(matchedApps), count
	}

	// Implement pagination
	start := page * size
	end := start + size
	if start > len(matchedApps) {
		return []*models.ApplicationInfo{}, count // Return empty slice if start index is out of range
	}
	if end > len(matchedApps) {
		end = len(matchedApps) // Adjust end index if it exceeds the length
	}

	// Return the paginated result
	return deepCopyApplications(matchedApps[start:end]), count
}

// matchesWildcard checks if the input string matches the wildcard pattern
func matchesWildcard(input, pattern string) bool {
	// Replace wildcard '*' with a regex pattern
	pattern = strings.ReplaceAll(pattern, "*", ".*")
	matched, _ := regexp.MatchString("^"+pattern+"$", input)
	return matched
}

// getWildcardName converts the input name to a wildcard pattern
func getWildcardName(name string) string {
	// Convert the input name to a wildcard pattern
	return strings.ReplaceAll(name, " ", "*") // Example: replace spaces with '*'
}

func update() {
	mu.Lock() // Lock to ensure thread safety
	updateCacheAppTypes()
	updateCacheApplications()
	updateCacheTopApplications()
	updateCacheI18n()
	mu.Unlock()
}

// Define a function to periodically call method a
func startCacheUpdater() {
	ticker := time.NewTicker(5 * time.Minute) // Every 5 minutes
	defer ticker.Stop()

	update()
	for range ticker.C { // Use for range to receive from ticker.C
		update()
	}
}

func CacheCenterStart() {
	go startCacheUpdater() // Start the goroutine to update the cache periodically
}
