package oauth

import (
	"slices"
	"strings"
)

// The scopes a grant can hold. read is always there: a connection that could
// not read would have nothing to do. write is what the owner can switch off on
// the consent page.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
	// scopeOfflineAccess is accepted in a request because clients ask for it
	// to get a refresh token. Claude always gets one, so it is not kept: it
	// says nothing about what the token may do.
	scopeOfflineAccess = "offline_access"
)

// parseScope reads a space-separated scope parameter into the scopes it asks
// for, read always first. ok is false if it names anything else.
func parseScope(raw string) (scopes []string, ok bool) {
	write := false
	for _, s := range strings.Fields(raw) {
		switch s {
		case ScopeRead, scopeOfflineAccess:
		case ScopeWrite:
			write = true
		default:
			return nil, false
		}
	}
	return grantedScopes(write), true
}

// grantedScopes is what a grant holds: read, and write if it was given.
func grantedScopes(write bool) []string {
	if write {
		return []string{ScopeRead, ScopeWrite}
	}
	return []string{ScopeRead}
}

// within reports whether every scope in asked is also in held.
func within(asked, held []string) bool {
	for _, s := range asked {
		if !slices.Contains(held, s) {
			return false
		}
	}
	return true
}
