package helper

import (
	"os"
)

// IsPublicEnvironment reports whether the binary runs in the "public"
// deployment mode that is used outside of Olares user clusters. In that
// mode several modules (history, task, ...) skip their PostgreSQL-backed
// initialisation.
func IsPublicEnvironment() bool {
	return os.Getenv("isPublic") == "true"
}

// IsAccountFromHeader reports whether the user account should be sourced
// from the request header rather than from the cluster identity.
func IsAccountFromHeader() bool {
	return os.Getenv("IS_ACCOUNT_FROM_HEADER") == "true"
}
