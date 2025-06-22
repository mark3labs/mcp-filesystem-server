package handler

import (
	"fmt"
	"os"
	"path/filepath"
)

type FilesystemHandler struct {
	allowedDirs []string
}

func NewFilesystemHandler(allowedDirs []string) (*FilesystemHandler, error) {
	// Normalize and validate directories
	normalized := make([]string, 0, len(allowedDirs))
	for _, dir := range allowedDirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path %s: %w", dir, err)
		}

		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to access directory %s: %w",
				abs,
				err,
			)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s", abs)
		}

		// Ensure the path ends with a separator to prevent prefix matching issues
		// For example, /tmp/foo should not match /tmp/foobar
		normalized = append(normalized, filepath.Clean(abs)+string(filepath.Separator))
	}
	return &FilesystemHandler{
		allowedDirs: normalized,
	}, nil
}

// pathToResourceURI converts a file path to a resource URI
func pathToResourceURI(path string) string {
	return "file://" + path
}
