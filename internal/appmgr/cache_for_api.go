package appmgr

import (
	"encoding/json"
	"market/internal/models"
	"market/internal/appservice"
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

// containsCategory checks if the given category exists in the Categories slice, ignoring case
func containsCategory(categories []string, category string) bool {
	glog.Infof("---------->on containsCategory, categories: %v, category: %s", categories, category)

	// If category is an empty string, return true
	if category == "" {
		return true
	}

	for _, cat := range categories {
		if strings.EqualFold(cat, category) { // Use EqualFold to ignore case
			glog.Infof("---------->on containsCategory true")
			return true
		}
	}
	glog.Infof("---------->on containsCategory false")
	return false
}

// matchesType checks if the app's CfgType matches any of the specified types, ignoring case
func matchesType(appType string, ty string) bool {
	if ty == "" {
		return true // If ty is empty, consider it a match
	}

	// Split the ty string into a slice of types
	types := strings.Split(ty, ",")
	for _, t := range types {
		if strings.EqualFold(appType, t) {
			return true // Return true if a match is found, ignoring case
		}
	}
	return false // Return false if no matches are found
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

// Helper function to process application variants based on admin status
func processAppVariants(app *models.ApplicationInfo, isAdmin bool) *models.ApplicationInfo {
	// Create a deep copy to avoid modifying the cache
	appCopy := deepCopyApplication(app)
	
	// For non-admin users, if app has user variant, optimize app info
	if !isAdmin && appCopy.Variants != nil {
		if userVariant, exists := appCopy.Variants["user"]; exists {
			// Keep original important information
			originalID := appCopy.Id
			originalStatus := appCopy.Status
			originalProgress := appCopy.Progress
			originalI18n := appCopy.I18n
			originalChartName := appCopy.ChartName
			originalCreateTime := appCopy.CreateTime
			originalUpdateTime := appCopy.UpdateTime
			originalInstallTime := appCopy.InstallTime
			originalUid := appCopy.Uid
			originalNamespace := appCopy.Namespace
			
			// Use fields from the variant
			appCopy.Title = userVariant.Title
			appCopy.Description = userVariant.Description
			appCopy.FullDescription = userVariant.FullDescription
			appCopy.UpgradeDescription = userVariant.UpgradeDescription
			appCopy.PromoteImage = userVariant.PromoteImage
			appCopy.PromoteVideo = userVariant.PromoteVideo
			appCopy.Categories = userVariant.Categories
			appCopy.SubCategory = userVariant.SubCategory
			appCopy.Icon = userVariant.Icon
			appCopy.FeatureImage = userVariant.FeatureImage
			appCopy.Rating = userVariant.Rating
			appCopy.Developer = userVariant.Developer
			appCopy.Submitter = userVariant.Submitter
			appCopy.Doc = userVariant.Doc
			appCopy.Website = userVariant.Website
			appCopy.SourceCode = userVariant.SourceCode
			appCopy.License = userVariant.License
			appCopy.Legal = userVariant.Legal
			appCopy.Permission = userVariant.Permission
			appCopy.Middleware = userVariant.Middleware
			appCopy.Options = userVariant.Options
			
			// If variant has i18n, merge i18n information
			if userVariant.I18n != nil && len(userVariant.I18n) > 0 {
				// If original i18n is empty, use variant's i18n directly
				if appCopy.I18n == nil || len(appCopy.I18n) == 0 {
					appCopy.I18n = userVariant.I18n
				} else {
					// Otherwise merge i18n info, prioritizing variant
					for locale, i18nInfo := range userVariant.I18n {
						appCopy.I18n[locale] = i18nInfo
					}
				}
			} else {
				// Restore original i18n
				appCopy.I18n = originalI18n
			}
			
			// If variant has entrances, use variant's entrances
			if userVariant.Entrances != nil && len(userVariant.Entrances) > 0 {
				appCopy.Entrances = userVariant.Entrances
			}
			
			// Restore original fixed information
			appCopy.Id = originalID
			appCopy.Status = originalStatus
			appCopy.Progress = originalProgress
			appCopy.ChartName = originalChartName
			appCopy.CreateTime = originalCreateTime
			appCopy.UpdateTime = originalUpdateTime
			appCopy.InstallTime = originalInstallTime
			appCopy.Uid = originalUid
			appCopy.Namespace = originalNamespace
			
			glog.Infof("Application %s's information optimized for normal users", appCopy.Name)
		}
	}
	
	return appCopy
}

// Helper function to determine if the user is an admin
func isUserAdmin(token string) bool {
	// Get admin username
	adminUsername, adminErr := appservice.GetAdminUsername(token)
	if adminErr != nil {
		glog.Warningf("Failed to get admin username: %s", adminErr.Error())
		return false
	}
	
	// Get current user info
	userInfoStr, userInfoErr := appservice.GetUserInfo(token)
	if userInfoErr != nil {
		glog.Warningf("Failed to get user info: %s", userInfoErr.Error())
		return false
	}
	
	var userInfo struct {
		Role     string `json:"role"`
		Username string `json:"username"`
	}
	
	if err := json.Unmarshal([]byte(userInfoStr), &userInfo); err != nil {
		glog.Warningf("Failed to parse user info: %s", err.Error())
		return false
	}
	
	// Check if user is admin
	return userInfo.Username == adminUsername
}

// Update ReadCacheApplications to process variants with token
func ReadCacheApplications(page, size int, category, ty, token string) ([]*models.ApplicationInfo, int64) {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	// Check admin status once per request
	isAdmin := isUserAdmin(token)

	var filteredApps []*models.ApplicationInfo
	var totalCount int64 // Counter for total matching applications

	// Filter applications based on category and cfgType
	for _, app := range cacheApplications {
		// Check if the app's Categories contains the specified category
		categoryMatch := containsCategory(app.Categories, category)

		// Check if the app's CfgType matches any of the specified types
		typeMatch := matchesType(app.CfgType, ty)

		if categoryMatch && typeMatch {
			filteredApps = append(filteredApps, app)
			totalCount++ // Increment the count for each matching application
		}
	}

	// If page and size are both 0, return all filtered applications
	if page == 0 && size == 0 {
		// Process each app for variants
		var processedApps []*models.ApplicationInfo
		for _, app := range filteredApps {
			processedApps = append(processedApps, processAppVariants(app, isAdmin))
		}
		return processedApps, totalCount
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

	// Process paginated apps for variants
	var processedApps []*models.ApplicationInfo
	for _, app := range filteredApps[start:end] {
		processedApps = append(processedApps, processAppVariants(app, isAdmin))
	}

	glog.Infof("---------->on ReadCacheApplications: %d", len(processedApps))
	return processedApps, totalCount // Return the paginated result and total count
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

// Update ReadCacheTopApps to process variants with token
func ReadCacheTopApps(category, ty string, size int, token string) ([]*models.ApplicationInfo, int64) {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	// Check admin status once per request
	isAdmin := isUserAdmin(token)

	var filteredApps []*models.ApplicationInfo
	var totalCount int64

	for _, app := range cacheTopApplications {
		// Use matchesType to check if the app's CfgType matches any of the specified types
		if !matchesType(app.CfgType, ty) {
			continue
		}

		// Use containsCategory to check if the app's categories contain the specified category
		if !containsCategory(app.Categories, category) {
			continue
		}

		// Add the app to the filtered list
		filteredApps = append(filteredApps, app)
		totalCount++

		// Check if we have reached the desired size
		if size > 0 && len(filteredApps) >= size {
			break
		}
	}

	// Process each app for variants
	var processedApps []*models.ApplicationInfo
	for _, app := range filteredApps {
		processedApps = append(processedApps, processAppVariants(app, isAdmin))
	}

	glog.Infof("---------->on ReadCacheTopApps: %d", len(processedApps))

	return processedApps, totalCount
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

// Update ReadCacheApplication to process variants with token
func ReadCacheApplication(name, token string) *models.ApplicationInfo {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	// Check admin status once per request
	isAdmin := isUserAdmin(token)

	for _, app := range cacheApplications {
		if app.Name == name {
			// Process the app for variants
			processedApp := processAppVariants(app, isAdmin)
			
			glog.Infof("---------->on ReadCacheApplication: %s", processedApp.Name)

			return processedApp
		}
	}
	return nil
}

// Update ReadCacheApplicationsWithMap to process variants with token
func ReadCacheApplicationsWithMap(names []string, token string) map[string]*models.ApplicationInfo {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	// Check admin status once per request
	isAdmin := isUserAdmin(token)

	result := make(map[string]*models.ApplicationInfo)

	// If names is an empty array, return all data
	if len(names) == 0 {
		for _, app := range cacheApplications {
			// Process the app for variants
			processedApp := processAppVariants(app, isAdmin)
			result[app.Name] = processedApp
		}
		glog.Infof("---------->on ReadCacheApplicationsWithMap: Returning all applications, count: %d", len(result))
		return result
	}

	// Otherwise, return the specified applications by name
	for _, name := range names {
		for _, app := range cacheApplications {
			if app.Name == name {
				// Process the app for variants
				processedApp := processAppVariants(app, isAdmin)
				result[name] = processedApp
				break
			}
		}
	}

	glog.Infof("---------->on ReadCacheApplicationsWithMap: %d", len(result))

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

// Update SearchFromCache to process variants with token
func SearchFromCache(page, size int, name, token string) (infos []*models.ApplicationInfo, count int64) {
	mu.Lock() // Lock to ensure thread safety
	defer mu.Unlock()

	// Check admin status once per request
	isAdmin := isUserAdmin(token)

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
		// Process each app for variants
		var processedApps []*models.ApplicationInfo
		for _, app := range matchedApps {
			processedApps = append(processedApps, processAppVariants(app, isAdmin))
		}
		return processedApps, count
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

	// Process paginated apps for variants
	var processedApps []*models.ApplicationInfo
	for _, app := range matchedApps[start:end] {
		processedApps = append(processedApps, processAppVariants(app, isAdmin))
	}

	return processedApps, count
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
