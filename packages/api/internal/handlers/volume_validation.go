package handlers

import (
	"path/filepath"
	"strings"
)

// allowedMountPrefixes defines safe mount path prefixes.
// Paths must start with one of these prefixes and have at least one subdirectory.
var allowedMountPrefixes = []string{
	"/workspace/",
	"/data/",
	"/mnt/",
	"/volumes/",
}

// ValidateMountPath validates that a mount path is safe and allowed.
// Returns an error message if invalid, or empty string if valid.
func ValidateMountPath(path string) string {
	// Must be absolute
	if !strings.HasPrefix(path, "/") {
		return "Mount path must be absolute"
	}

	// Must be canonical (no .., //, or trailing /)
	clean := filepath.Clean(path)
	if clean != path {
		return "Mount path must be canonical (no '..' or '//')"
	}

	// Check for .. traversal (even after Clean, double check)
	if strings.Contains(path, "..") {
		return "Mount path must be canonical (no '..' or '//')"
	}

	// Must start with allowed prefix
	hasAllowedPrefix := false
	for _, prefix := range allowedMountPrefixes {
		if strings.HasPrefix(path, prefix) {
			hasAllowedPrefix = true
			break
		}
	}
	if !hasAllowedPrefix {
		return "Mount path must start with /workspace/, /data/, /mnt/, or /volumes/"
	}

	// Must have path component after prefix (e.g., /workspace/x OK, /workspace alone rejected)
	// The prefix already ends with /, so we just need to check there's something after it
	for _, prefix := range allowedMountPrefixes {
		if strings.HasPrefix(path, prefix) {
			remainder := strings.TrimPrefix(path, prefix)
			if remainder == "" {
				return "Mount path must include subdirectory after prefix"
			}
			break
		}
	}

	return ""
}
