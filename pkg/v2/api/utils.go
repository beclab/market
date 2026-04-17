package api

import (
	"market/internal/v2/types"
	"strings"
)

// isNonMarketSource checks whether the source does not use the market.xxx format.
func isNonMarketSource(source string) bool {
	return !strings.HasPrefix(strings.TrimSpace(source), types.AppMarketSourcePrefix)
}

// isNotCloneInstance checks whether the instance name fails to follow
// the rawAppName + suffix naming rule.
func isNotCloneInstance(name string, rawAppName string) bool {
	if rawAppName == "" || name == "" {
		return true
	}
	if !strings.HasPrefix(name, rawAppName) {
		return true
	}
	return len(name) <= len(rawAppName)
}
