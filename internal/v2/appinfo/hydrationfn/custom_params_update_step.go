package hydrationfn

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CustomParamsUpdateStep handles updating custom parameters
// CustomParamsUpdateStep 处理更新自定义参数
type CustomParamsUpdateStep struct {
	stepName string
}

// NewCustomParamsUpdateStep creates a new custom parameters update step
// NewCustomParamsUpdateStep 创建新的自定义参数更新步骤
func NewCustomParamsUpdateStep() *CustomParamsUpdateStep {
	return &CustomParamsUpdateStep{
		stepName: "CustomParamsUpdate",
	}
}

// GetStepName returns the name of the step
// GetStepName 返回步骤名称
func (s *CustomParamsUpdateStep) GetStepName() string {
	return s.stepName
}

// CanSkip determines if this step can be skipped
// CanSkip 确定是否可以跳过此步骤
func (s *CustomParamsUpdateStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// // 如果没有自定义参数需要更新，则跳过此步骤
	// if task.AppData == nil {
	// 	return true
	// }

	// // 检查是否有Values字段
	// values, ok := task.AppData["values"]
	// if !ok || values == nil {
	// 	return true
	// }

	return false
}

// Execute performs the custom parameters update
// Execute 执行自定义参数更新
func (s *CustomParamsUpdateStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Starting custom parameters update for app: %s", task.AppID)

	// 1. 获取原始chart包路径
	rawPackage := task.SourceChartURL
	if rawPackage == "" {
		return fmt.Errorf("source chart path is empty for app: %s", task.AppID)
	}

	// 处理 file:// 协议的 URL 路径
	if strings.HasPrefix(rawPackage, "file://") {
		rawPackage = strings.TrimPrefix(rawPackage, "file://")
	}

	log.Printf("Debug: Raw package path: %s", rawPackage)

	// 检查文件是否存在
	fileInfo, err := os.Stat(rawPackage)
	if err != nil {
		return fmt.Errorf("chart file does not exist or is not accessible: %v", err)
	}

	// 检查文件大小
	if fileInfo.Size() == 0 {
		return fmt.Errorf("chart file is empty")
	}

	// 检查文件权限
	if fileInfo.Mode()&0400 == 0 {
		return fmt.Errorf("chart file is not readable")
	}

	// 检查文件类型
	if !strings.HasSuffix(rawPackage, ".tgz") && !strings.HasSuffix(rawPackage, ".tar.gz") {
		log.Printf("Warning: Chart file may not be in correct format: %s", rawPackage)
	}

	// 2. 创建临时目录用于解压和修改
	tempDir, err := ioutil.TempDir("", "chart-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 3. 解压chart包到临时目录
	extractDir := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create extract directory: %v", err)
	}

	if err := s.extractChart(rawPackage, extractDir); err != nil {
		return fmt.Errorf("failed to extract chart: %v", err)
	}

	// 4. 获取自定义参数并更新（如果有）
	if values, ok := task.AppData["values"]; ok && values != nil {
		log.Printf("Found custom values, updating chart parameters")
		if err := s.updateChartValues(extractDir, values); err != nil {
			return fmt.Errorf("failed to update chart values: %v", err)
		}
	} else {
		log.Printf("No custom values found, skipping parameter update")
	}

	// 5. 重新打包chart
	version := task.AppVersion
	if version == "" {
		version = "latest"
	}
	newChartName := fmt.Sprintf("%s-%s", task.AppName, version)

	// 获取 CHART_ROOT 环境变量
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// 构建新的chart路径
	newChartPath := filepath.Join(chartRoot, task.UserID, task.SourceID, newChartName, fmt.Sprintf("%s.tgz", newChartName))
	log.Printf("Debug: New chart will be saved to: %s", newChartPath)

	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(newChartPath), 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %v", err)
	}

	if err := s.packageChart(extractDir, newChartPath); err != nil {
		return fmt.Errorf("failed to package chart: %v", err)
	}

	// 6. 更新任务中的包路径
	task.ChartData["local_path"] = newChartPath

	log.Printf("Custom parameters update completed for app: %s, new chart path: %s", task.AppID, newChartPath)
	return nil
}

// extractChart 解压chart包
func (s *CustomParamsUpdateStep) extractChart(chartPath, targetDir string) error {
	// 使用tar命令解压chart包
	log.Printf("Debug: Extracting chart from %s to %s", chartPath, targetDir)
	cmd := exec.Command("tar", "-xzf", chartPath, "-C", targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract chart: %v, output: %s", err, string(output))
	}
	log.Printf("Debug: Chart extracted successfully")
	return nil
}

// updateChartValues 更新chart中的参数
func (s *CustomParamsUpdateStep) updateChartValues(chartDir string, values interface{}) error {
	// 遍历chart目录中的所有yaml文件
	err := filepath.Walk(chartDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只处理yaml文件
		if !info.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			// 读取文件内容
			content, err := ioutil.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %v", path, err)
			}

			// 更新文件内容
			updatedContent, err := s.updateYamlContent(string(content), values)
			if err != nil {
				return fmt.Errorf("failed to update content in file %s: %v", path, err)
			}

			// 写回文件
			if err := ioutil.WriteFile(path, []byte(updatedContent), info.Mode()); err != nil {
				return fmt.Errorf("failed to write updated content to file %s: %v", path, err)
			}
		}
		return nil
	})

	return err
}

// updateYamlContent 更新YAML文件内容
func (s *CustomParamsUpdateStep) updateYamlContent(content string, values interface{}) (string, error) {
	// TODO: 实现YAML内容的更新逻辑
	// 1. 解析YAML内容
	// 2. 根据values更新相应的值
	// 3. 重新生成YAML内容
	return content, nil
}

// packageChart 打包chart
func (s *CustomParamsUpdateStep) packageChart(sourceDir, targetPath string) error {
	// 使用tar命令打包chart
	cmd := exec.Command("tar", "-czf", targetPath, "-C", sourceDir, ".")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to package chart: %v", err)
	}
	return nil
}
