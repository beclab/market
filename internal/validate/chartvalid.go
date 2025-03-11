package validate

import (
	"fmt"
	"market/internal/constants"
	"market/internal/models"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"market/internal/appservice"
)

func CheckChartFolder(folder string) error {
	folderName := path.Base(folder)
	if !isValidFolderName(folderName) {
		return fmt.Errorf("invalid folder name: '%s' must '^[a-z0-9]{1,30}$'", folder)
	}

	if !dirExists(folder) {
		return fmt.Errorf("folder does not exist: '%s'", folder)
	}

	chartFile := filepath.Join(folder, "Chart.yaml")
	if !fileExists(chartFile) {
		return fmt.Errorf("missing Chart.yaml in folder: '%s'", folder)
	}

	chartContent, err := os.ReadFile(chartFile)
	if err != nil {
		return fmt.Errorf("failed to read Chart.yaml in folder '%s': %v", folder, err)
	}
	var chart models.Chart
	if err := yaml.Unmarshal(chartContent, &chart); err != nil {
		return fmt.Errorf("failed to parse Chart.yaml in folder '%s': %v", folder, err)
	}

	if err := isValidChartFields(chart); err != nil {
		return err
	}

	valuesFile := filepath.Join(folder, "values.yaml")
	if !fileExists(valuesFile) {
		return fmt.Errorf("missing values.yaml in folder: '%s'", folder)
	}

	templatesDir := filepath.Join(folder, "templates")
	if !dirExists(templatesDir) {
		return fmt.Errorf("missing templates folder in folder: '%s'", folder)
	}

	appCfgFile := filepath.Join(folder, constants.AppCfgFileName)
	if !fileExists(appCfgFile) {
		return fmt.Errorf("missing %s in folder: '%s'", constants.AppCfgFileName, folder)
	}

	appCfgContent, err := os.ReadFile(appCfgFile)
	if err != nil {
		return fmt.Errorf("failed to read %s in folder '%s': %v", constants.AppCfgFileName, folder, err)
	}
	
	renderedContent, err := appservice.RenderManifest(string(appCfgContent), token)
	if err != nil {
		return fmt.Errorf("failed to render %s in folder '%s': %v", constants.AppCfgFileName, folder, err)
	}
	
	var appConf models.AppConfiguration
	if err := yaml.Unmarshal([]byte(renderedContent), &appConf); err != nil {
		return fmt.Errorf("failed to parse %s in folder '%s': %v", constants.AppCfgFileName, folder, err)
	}

	if err := isValidMetadataFields(appConf.Metadata, chart, folderName); err != nil {
		return err
	}

	if !checkCategories(appConf.Metadata.Categories) {
		return fmt.Errorf("categories %v invalid, must in %v", appConf.Metadata.Categories, validCategoriesSlice)
	}

	if checkReservedWord(folderName) {
		return fmt.Errorf("foldername %s in reserved foldername list, invalid", folderName)
	}

	return nil
}

func checkReservedWord(str string) bool {
	reservedWords := []string{
		"user", "system", "space", "default", "os", "kubesphere", "kube",
		"kubekey", "kubernetes", "gpu", "tapr", "bfl", "bytetrade",
		"project", "pod",
	}

	for _, word := range reservedWords {
		if strings.EqualFold(str, word) {
			return true
		}
	}

	return false
}

func isValidFolderName(name string) bool {
	match, _ := regexp.MatchString("^[a-z0-9]{1,30}$", name)
	return match
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return (err == nil || os.IsExist(err)) && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return (err == nil || os.IsExist(err)) && info.IsDir()
}

func isValidChartFields(chart models.Chart) error {
	if chart.APIVersion == "" {
		return fmt.Errorf("apiVersion field empty in %s in chart '%s'", constants.AppCfgFileName, chart)
	}

	if chart.Name == "" {
		return fmt.Errorf("name field empty in %s in chart '%s'", constants.AppCfgFileName, chart)
	}

	if chart.Version == "" {
		return fmt.Errorf("version field empty in %s in chart '%s'", constants.AppCfgFileName, chart)
	}

	return nil
}

func isValidMetadataFields(metadata models.AppMetaData, chart models.Chart, folder string) error {
	if chart.Name != folder || metadata.Name != folder {
		return fmt.Errorf("name in Chart.yaml:%s, chartFolder:%s, name in %s:%s must same",
			chart.Name, folder, constants.AppCfgFileName, metadata.Name)
	}

	if metadata.Version != chart.Version {
		return fmt.Errorf("version in %s:%s, version in Chart.yaml:%s must same",
			constants.AppCfgFileName, metadata.Version, chart.Version)
	}

	return nil
}
