package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
)

// DockerImageInfo contains detailed information about a Docker image
type DockerImageInfo struct {
	Name         string         `json:"name"`
	Tag          string         `json:"tag"`
	Architecture string         `json:"architecture"`
	Layers       []LayerInfo    `json:"layers"`
	Manifest     *ImageManifest `json:"manifest,omitempty"`
	Config       *ImageConfig   `json:"config,omitempty"`
	TotalSize    int64          `json:"total_size"`
	CreatedAt    time.Time      `json:"created_at"`
}

// LayerInfo contains information about a specific layer
type LayerInfo struct {
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	MediaType  string `json:"media_type"`
	Downloaded bool   `json:"downloaded"`
	Progress   int    `json:"progress"` // 0-100
	LocalPath  string `json:"local_path,omitempty"`
}

// ImageManifest represents Docker image manifest
type ImageManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Config        ManifestConfig  `json:"config"`
	Layers        []ManifestLayer `json:"layers"`
}

// ManifestConfig represents the config section in manifest
type ManifestConfig struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// ManifestLayer represents a layer in the manifest
type ManifestLayer struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// ImageConfig represents Docker image configuration
type ImageConfig struct {
	Architecture string                 `json:"architecture"`
	OS           string                 `json:"os"`
	Config       map[string]interface{} `json:"config"`
	RootFS       RootFS                 `json:"rootfs"`
	History      []HistoryEntry         `json:"history"`
	Created      time.Time              `json:"created"`
}

// RootFS represents the root filesystem info
type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// HistoryEntry represents a history entry in the image config
type HistoryEntry struct {
	Created    time.Time `json:"created"`
	CreatedBy  string    `json:"created_by"`
	EmptyLayer bool      `json:"empty_layer,omitempty"`
	Comment    string    `json:"comment,omitempty"`
}

// ContainerRuntime represents the detected container runtime
type ContainerRuntime string

const (
	RuntimeK8s     ContainerRuntime = "k8s"
	RuntimeK3s     ContainerRuntime = "k3s"
	RuntimeDocker  ContainerRuntime = "docker"
	RuntimeUnknown ContainerRuntime = "unknown"
)

// ImageInfoResponse represents the response from the image info API
type ImageInfoResponse struct {
	Images []ImageInfo `json:"images"`
	Name   string      `json:"name"`
}

// ImageInfo represents a single image info from the API
type ImageInfo struct {
	Node         string      `json:"node"`
	Name         string      `json:"name"`
	Architecture string      `json:"architecture"`
	Variant      string      `json:"variant"`
	OS           string      `json:"os"`
	LayersData   []LayerData `json:"layersData"`
}

// LayerData represents layer information from the API
type LayerData struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
}

// GetDockerImageInfo retrieves detailed information about a Docker image from registry
func GetDockerImageInfo(imageName string) (*DockerImageInfo, error) {
	glog.Infof("Getting Docker image info for: %s", imageName)

	// Check if it's development environment
	if isDevelopmentEnvironment() {
		glog.Infof("Development environment detected, using direct registry access")
		return getDockerImageInfoFromRegistry(imageName)
	}

	// Production environment - use API
	glog.Infof("Production environment detected, using API access")
	return getDockerImageInfoFromAPI(imageName)
}

// getDockerImageInfoFromRegistry gets image info directly from registry
func getDockerImageInfoFromRegistry(imageName string) (*DockerImageInfo, error) {
	// Parse image name and tag
	name, tag := parseImageNameAndTag(imageName)

	// Get image manifest
	manifest, err := getImageManifest(name, tag)
	if err != nil {
		glog.Errorf("Failed to get image manifest: %v", err)
		return nil, fmt.Errorf("failed to get image manifest: %w", err)
	}

	// Get image config
	config, err := getImageConfig(name, manifest.Config.Digest)
	if err != nil {
		glog.Warningf("Failed to get image config: %v", err)
		// Continue without config as it's not critical
	}

	// Build layer information
	layers := make([]LayerInfo, len(manifest.Layers))
	var totalSize int64

	for i, layer := range manifest.Layers {
		layers[i] = LayerInfo{
			Digest:    layer.Digest,
			Size:      layer.Size,
			MediaType: layer.MediaType,
		}
		totalSize += layer.Size
	}

	imageInfo := &DockerImageInfo{
		Name:      name,
		Tag:       tag,
		Layers:    layers,
		Manifest:  manifest,
		Config:    config,
		TotalSize: totalSize,
		CreatedAt: time.Now(),
	}

	if config != nil {
		imageInfo.Architecture = config.Architecture
		imageInfo.CreatedAt = config.Created
	}

	return imageInfo, nil
}

// getDockerImageInfoFromAPI gets image info from the app service API
func getDockerImageInfoFromAPI(imageName string) (*DockerImageInfo, error) {
	// Get app service host and port from environment
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if appServiceHost == "" || appServicePort == "" {
		return nil, fmt.Errorf("app service host or port not configured")
	}

	// Build request URL
	url := fmt.Sprintf("http://%s:%s/app-service/v1/apps/image-info", appServiceHost, appServicePort)

	// Prepare request body
	requestBody := map[string]interface{}{
		"name":   "image-info",
		"images": []string{imageName},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var apiResponse ImageInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if we got any image info
	if len(apiResponse.Images) == 0 {
		return nil, fmt.Errorf("no image info found for %s", imageName)
	}

	// Convert API response to DockerImageInfo
	imageInfo := apiResponse.Images[0]
	layers := make([]LayerInfo, len(imageInfo.LayersData))
	var totalSize int64

	for i, layer := range imageInfo.LayersData {
		layers[i] = LayerInfo{
			Digest:    layer.Digest,
			Size:      layer.Size,
			MediaType: layer.MediaType,
		}
		totalSize += layer.Size
	}

	return &DockerImageInfo{
		Name:         imageInfo.Name,
		Architecture: imageInfo.Architecture,
		Layers:       layers,
		TotalSize:    totalSize,
		CreatedAt:    time.Now(),
	}, nil
}

// GetLayerDownloadProgress checks the download progress of a specific layer locally
func GetLayerDownloadProgress(layerDigest string) (*LayerInfo, error) {
	glog.Infof("Checking layer download progress for: %s", layerDigest)

	// Detect container runtime
	runtime := detectContainerRuntime()
	glog.Infof("Detected container runtime: %s", runtime)

	layerInfo := &LayerInfo{
		Digest: layerDigest,
	}

	switch runtime {
	case RuntimeK8s:
		return getK8sLayerProgress(layerInfo)
	case RuntimeK3s:
		return getK3sLayerProgress(layerInfo)
	case RuntimeDocker:
		return getDockerLayerProgress(layerInfo)
	default:
		return getGenericLayerProgress(layerInfo)
	}
}

// parseImageNameAndTag splits image name into name and tag components
func parseImageNameAndTag(imageName string) (string, string) {
	if strings.Contains(imageName, ":") {
		parts := strings.SplitN(imageName, ":", 2)
		return parts[0], parts[1]
	}
	return imageName, "latest"
}

// getImageManifest retrieves the image manifest from Docker registry
func getImageManifest(imageName, tag string) (*ImageManifest, error) {
	// Handle different registry formats
	registryURL := buildRegistryURL(imageName, tag)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", registryURL+"/manifests/"+tag, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set appropriate headers for Docker Registry API
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var manifest ImageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// getImageConfig retrieves the image configuration
func getImageConfig(imageName, configDigest string) (*ImageConfig, error) {
	registryURL := buildRegistryURL(imageName, "")

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", registryURL+"/blobs/"+configDigest, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create config request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read config response: %w", err)
	}

	var config ImageConfig
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// buildRegistryURL constructs the appropriate registry URL for the image
func buildRegistryURL(imageName, tag string) string {
	// Handle Docker Hub images
	if !strings.Contains(imageName, "/") || (!strings.Contains(imageName, ".") && strings.Count(imageName, "/") == 1) {
		if !strings.Contains(imageName, "/") {
			imageName = "library/" + imageName
		}
		return "https://registry-1.docker.io/v2/" + imageName
	}

	// Handle custom registry
	parts := strings.SplitN(imageName, "/", 2)
	if len(parts) == 2 {
		registry := parts[0]
		repo := parts[1]
		return "https://" + registry + "/v2/" + repo
	}

	return "https://registry-1.docker.io/v2/" + imageName
}

// detectContainerRuntime attempts to detect the local container runtime
func detectContainerRuntime() ContainerRuntime {
	// Check for k3s
	if isK3sRunning() {
		return RuntimeK3s
	}

	// Check for k8s (containerd/cri-o)
	if isK8sRunning() {
		return RuntimeK8s
	}

	// Check for Docker
	if isDockerRunning() {
		return RuntimeDocker
	}

	return RuntimeUnknown
}

// isK3sRunning checks if k3s is running on the system
func isK3sRunning() bool {
	// Check if k3s binary exists and is running
	if _, err := exec.LookPath("k3s"); err == nil {
		cmd := exec.Command("pgrep", "-f", "k3s")
		return cmd.Run() == nil
	}

	// Check for k3s-specific directories
	k3sPaths := []string{
		"/var/lib/rancher/k3s",
		"/etc/rancher/k3s",
	}

	for _, path := range k3sPaths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}

// isK8sRunning checks if standard k8s (kubelet/containerd) is running
func isK8sRunning() bool {
	// Check if kubelet is running
	if cmd := exec.Command("pgrep", "-f", "kubelet"); cmd.Run() == nil {
		return true
	}

	// Check if containerd is running
	if cmd := exec.Command("pgrep", "-f", "containerd"); cmd.Run() == nil {
		return true
	}

	// Check for k8s-specific directories
	k8sPaths := []string{
		"/var/lib/kubelet",
		"/etc/kubernetes",
		"/var/lib/containerd",
	}

	for _, path := range k8sPaths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}

// isDockerRunning checks if Docker daemon is running
func isDockerRunning() bool {
	if _, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command("docker", "version")
		return cmd.Run() == nil
	}
	return false
}

// getK8sLayerProgress checks layer progress in k8s environment
func getK8sLayerProgress(layerInfo *LayerInfo) (*LayerInfo, error) {
	// Try containerd first
	if progress, err := getContainerdLayerProgress(layerInfo.Digest); err == nil {
		layerInfo.Downloaded = progress.Downloaded
		layerInfo.Progress = progress.Progress
		layerInfo.LocalPath = progress.LocalPath
		return layerInfo, nil
	}

	// Fallback to checking filesystem directly
	return getContainerdFilesystemProgress(layerInfo)
}

// getK3sLayerProgress checks layer progress in k3s environment
func getK3sLayerProgress(layerInfo *LayerInfo) (*LayerInfo, error) {
	// k3s uses containerd, but in a different location
	k3sContainerdRoot := "/var/lib/rancher/k3s/agent/containerd"

	// Check if layer exists in k3s containerd
	if progress, err := getK3sContainerdLayerProgress(layerInfo.Digest, k3sContainerdRoot); err == nil {
		layerInfo.Downloaded = progress.Downloaded
		layerInfo.Progress = progress.Progress
		layerInfo.LocalPath = progress.LocalPath
		return layerInfo, nil
	}

	// Fallback to direct filesystem check
	return getK3sFilesystemProgress(layerInfo, k3sContainerdRoot)
}

// getDockerLayerProgress checks layer progress in Docker environment
func getDockerLayerProgress(layerInfo *LayerInfo) (*LayerInfo, error) {
	// Use docker inspect to check layer status
	cmd := exec.Command("docker", "system", "df", "-v")
	output, err := cmd.Output()
	if err != nil {
		glog.Warningf("Failed to run docker system df: %v", err)
		return getDockerFilesystemProgress(layerInfo)
	}

	// Parse docker system df output to find layer info
	if progress := parseDockerSystemDF(string(output), layerInfo.Digest); progress != nil {
		layerInfo.Downloaded = progress.Downloaded
		layerInfo.Progress = progress.Progress
		layerInfo.LocalPath = progress.LocalPath
		return layerInfo, nil
	}

	return getDockerFilesystemProgress(layerInfo)
}

// getGenericLayerProgress provides a generic fallback for unknown runtimes
func getGenericLayerProgress(layerInfo *LayerInfo) (*LayerInfo, error) {
	// Try common container storage locations
	commonPaths := []string{
		"/var/lib/docker",
		"/var/lib/containerd",
		"/var/lib/rancher/k3s",
	}

	for _, basePath := range commonPaths {
		if progress, err := checkLayerInPath(layerInfo.Digest, basePath); err == nil {
			layerInfo.Downloaded = progress.Downloaded
			layerInfo.Progress = progress.Progress
			layerInfo.LocalPath = progress.LocalPath
			return layerInfo, nil
		}
	}

	// No layer found
	layerInfo.Downloaded = false
	layerInfo.Progress = 0
	return layerInfo, nil
}

// Helper function to get containerd layer progress
func getContainerdLayerProgress(digest string) (*LayerInfo, error) {
	cmd := exec.Command("ctr", "content", "ls")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ctr content ls: %w", err)
	}

	return parseContainerdContentList(string(output), digest)
}

// Helper function for k3s containerd
func getK3sContainerdLayerProgress(digest, containerdRoot string) (*LayerInfo, error) {
	cmd := exec.Command("k3s", "ctr", "content", "ls")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run k3s ctr content ls: %w", err)
	}

	return parseContainerdContentList(string(output), digest)
}

// Parse containerd content list output
func parseContainerdContentList(output, digest string) (*LayerInfo, error) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, digest) {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				layer := &LayerInfo{
					Digest:     digest,
					Downloaded: true,
					Progress:   100,
				}

				if size, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					layer.Size = size
				}

				return layer, nil
			}
		}
	}

	return &LayerInfo{
		Digest:     digest,
		Downloaded: false,
		Progress:   0,
	}, nil
}

// Filesystem-based progress checks
func getContainerdFilesystemProgress(layerInfo *LayerInfo) (*LayerInfo, error) {
	return checkLayerInPath(layerInfo.Digest, "/var/lib/containerd")
}

func getK3sFilesystemProgress(layerInfo *LayerInfo, containerdRoot string) (*LayerInfo, error) {
	return checkLayerInPath(layerInfo.Digest, containerdRoot)
}

func getDockerFilesystemProgress(layerInfo *LayerInfo) (*LayerInfo, error) {
	return checkLayerInPath(layerInfo.Digest, "/var/lib/docker")
}

// checkLayerInPath searches for a layer in the specified base path
func checkLayerInPath(digest, basePath string) (*LayerInfo, error) {
	// Remove sha256: prefix if present
	cleanDigest := strings.TrimPrefix(digest, "sha256:")

	// Common patterns for layer storage
	patterns := []string{
		filepath.Join(basePath, "**", cleanDigest),
		filepath.Join(basePath, "**", cleanDigest[:12]), // Short digest
		filepath.Join(basePath, "overlay2", cleanDigest, "**"),
		filepath.Join(basePath, "image", "overlay2", "layerdb", "sha256", cleanDigest),
	}

	for _, pattern := range patterns {
		if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
			// Found layer, check if it's complete
			for _, match := range matches {
				if info, err := os.Stat(match); err == nil {
					return &LayerInfo{
						Digest:     digest,
						Downloaded: true,
						Progress:   100,
						Size:       info.Size(),
						LocalPath:  match,
					}, nil
				}
			}
		}
	}

	return &LayerInfo{
		Digest:     digest,
		Downloaded: false,
		Progress:   0,
	}, nil
}

// parseDockerSystemDF parses docker system df output to find layer information
func parseDockerSystemDF(output, digest string) *LayerInfo {
	lines := strings.Split(output, "\n")
	cleanDigest := strings.TrimPrefix(digest, "sha256:")

	for _, line := range lines {
		if strings.Contains(line, cleanDigest) || strings.Contains(line, cleanDigest[:12]) {
			// Parse the line to extract size and status information
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				layer := &LayerInfo{
					Digest:     digest,
					Downloaded: true,
					Progress:   100,
				}

				// Try to parse size from the output
				for _, field := range fields {
					if matched, _ := regexp.MatchString(`^\d+(\.\d+)?[KMGT]?B$`, field); matched {
						if size := parseHumanReadableSize(field); size > 0 {
							layer.Size = size
							break
						}
					}
				}

				return layer
			}
		}
	}

	return nil
}

// parseHumanReadableSize converts human-readable size strings to bytes
func parseHumanReadableSize(sizeStr string) int64 {
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([KMGT]?)B?$`)
	matches := re.FindStringSubmatch(strings.ToUpper(sizeStr))

	if len(matches) != 3 {
		return 0
	}

	size, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	unit := matches[2]
	switch unit {
	case "K":
		size *= 1024
	case "M":
		size *= 1024 * 1024
	case "G":
		size *= 1024 * 1024 * 1024
	case "T":
		size *= 1024 * 1024 * 1024 * 1024
	}

	return int64(size)
}
