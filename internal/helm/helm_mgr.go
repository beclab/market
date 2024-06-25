package helm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/golang/glog"
)

var (
	chartMuseumURL = "http://chartmuseum:8080"
)

func UploadChart(chartFilePath string) error {
	file, err := os.Open(chartFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("chart", chartFilePath)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}
	err = writer.Close()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/charts", chartMuseumURL)
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	response, err := client.Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		dump, e := httputil.DumpRequest(request, true)
		if e == nil {
			glog.Warning("url:", url, string(dump))
		}
		return fmt.Errorf("chart upload failed with status: %s", response.Status)
	}

	return nil
}

func DeleteChart(chartName, chartVersion string) error {
	url := fmt.Sprintf("%s/%s/%s", chartMuseumURL, chartName, chartVersion)
	request, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	response, err := client.Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusOK {
		dump, e := httputil.DumpRequest(request, true)
		if e == nil {
			glog.Warning("url:", url, string(dump))
		}
		return fmt.Errorf("chart deletion failed with status: %s", response.Status)
	}

	return nil
}

func CheckChartExist(chartName, chartVersion string) (bool, error) {
	url := fmt.Sprintf("%s/%s", chartMuseumURL, chartName)
	if chartVersion != "" {
		url = fmt.Sprintf("%s/%s", url, chartVersion)
	}

	request, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, err
	}

	client := &http.Client{}
	response, err := client.Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}

	if response.StatusCode == http.StatusOK {
		return true, nil
	} else if response.StatusCode == http.StatusNotFound {
		dump, e := httputil.DumpRequest(request, true)
		if e == nil {
			glog.Warning("url:", url, string(dump))
		}
		return false, nil
	}

	return false, fmt.Errorf("chart existence check failed with status: %s", response.Status)
}

type Chart struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func DeleteChartByName(chartName string) error {
	versions, err := ListChartVersions(chartName)
	if err != nil {
		glog.Warningf("Failed to list chart versions: %v", err)
		return err
	}

	for _, version := range versions {
		err := DeleteChart(chartName, version)
		if err != nil {
			glog.Warningf("Failed to delete chart %s version %s: %v", chartName, version, err)
			return err
		} else {
			glog.Infof("Chart %s version %s deleted successfully!", chartName, version)
		}
	}

	return nil
}

func ListChartVersions(chartName string) ([]string, error) {
	url := fmt.Sprintf("%s/%s", chartMuseumURL, chartName)

	response, err := http.Get(url)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		dump, e := httputil.DumpRequest(response.Request, true)
		if e == nil {
			glog.Warning("url:", url, string(dump))
		}
		return nil, fmt.Errorf("failed to list chart versions with status: %s", response.Status)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var charts []Chart
	err = json.Unmarshal(body, &charts)
	if err != nil {
		return nil, err
	}

	versions := make([]string, len(charts))
	for i, chart := range charts {
		versions[i] = chart.Version
	}

	return versions, nil
}
