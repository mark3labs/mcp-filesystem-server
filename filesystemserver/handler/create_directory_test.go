package handler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCreateDirectory(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("create a new directory", func(t *testing.T) {
		newDirPath := filepath.Join(tmpDir, "new_directory")
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": newDirPath,
				},
			},
		}

		res, err := fsHandler.HandleCreateDirectory(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the directory was created
		info, err := os.Stat(newDirPath)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("directory already exists", func(t *testing.T) {
		existingDirPath := filepath.Join(tmpDir, "existing_directory")
		err := os.Mkdir(existingDirPath, 0755)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": existingDirPath,
				},
			},
		}

		res, err := fsHandler.HandleCreateDirectory(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError) // Should not be an error, just a message that it already exists
	})

	t.Run("path exists but is not a directory", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "existing_file.txt")
		err := os.WriteFile(filePath, []byte("content"), 0644)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": filePath,
				},
			},
		}

		res, err := fsHandler.HandleCreateDirectory(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})

	t.Run("path is in a non-allowed directory", func(t *testing.T) {
		otherDir := t.TempDir()

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": filepath.Join(otherDir, "new_directory"),
				},
			},
		}

		res, err := fsHandler.HandleCreateDirectory(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})
}
