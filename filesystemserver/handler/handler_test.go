package handler

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// resolveAllowedDirs generates a list of allowed paths, including their resolved symlinks.
// This ensures both the original paths and their symlink-resolved counterparts are included,
// which is useful when paths may be symlinks (e.g., t.TempDir() on some Unix systems).
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
