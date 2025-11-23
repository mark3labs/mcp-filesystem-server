package handler

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// resolveAllowedDirs generates a list of allowed paths, including their resolved counterparts.
// This ensures both the original paths and their resolved versions are included,
// which handles path normalization across platforms (symlinks on Unix, 8.3 short names on Windows).
func resolveAllowedDirs(t *testing.T, dirs ...string) []string {
	t.Helper()
	allowedDirs := make([]string, 0)
	for _, dir := range dirs {
		allowedDirs = append(allowedDirs, dir)

		resolvedPath, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err, "Failed to resolve symlinks for directory: %s", dir)

		if resolvedPath != dir {
			allowedDirs = append(allowedDirs, resolvedPath)
		}
	}
	return allowedDirs
}
