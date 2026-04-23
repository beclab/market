package helper

import (
	"os"
	"strings"
)

// IsPublicEnvironment reports whether the binary runs in the "public"
// deployment mode that is used outside of Olares user clusters. In that
// mode several modules (history, task, ...) skip their PostgreSQL-backed
// initialisation.
func IsPublicEnvironment() bool {
	return os.Getenv("isPublic") == "true"
}

// IsDevelopmentEnvironment reports whether the binary runs locally in a
// developer environment. The default (unset) is treated as development so
// that fresh checkouts behave the same as `GO_ENV=dev`.
func IsDevelopmentEnvironment() bool {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	return env == "dev" || env == "development" || env == ""
}

// IsAccountFromHeader reports whether the user account should be sourced
// from the request header rather than from the cluster identity.
func IsAccountFromHeader() bool {
	return os.Getenv("IS_ACCOUNT_FROM_HEADER") == "true"
}
