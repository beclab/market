package validate

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckResourceLimit(t *testing.T) {
	chartPath := "./testdata/calibre"
	cfg, err := getAppConfigFromCfgFile(chartPath)
	if err != nil {
		t.Error("get cfg failed: ", err)
	}
	err = CheckResourceLimit(chartPath, cfg)
	assert.NotNil(t, err)
}

func TestCheckResourceNamespace(t *testing.T) {
	chartPath := "./testdata/calibre"
	resources, err := getResourceListFromChart(chartPath)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Error("get resources failed: ", err)
	}
	err = checkResourceNamespace(resources)
	assert.NotNil(t, err)
}
