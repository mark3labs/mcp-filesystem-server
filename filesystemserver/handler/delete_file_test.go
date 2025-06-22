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

func TestHandleDeleteFile(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("delete a file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "test_file.txt")
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)

		// Verify file exists before deletion
		_, err = os.Stat(filePath)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": filePath,
				},
			},
		}

		res, err := fsHandler.HandleDeleteFile(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify file was deleted
		_, err = os.Stat(filePath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("delete an empty directory with recursive=true", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "empty_directory")
		err := os.Mkdir(dirPath, 0755)
		require.NoError(t, err)

		// Verify directory exists before deletion
		_, err = os.Stat(dirPath)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path":      dirPath,
					"recursive": true,
				},
			},
		}

		res, err := fsHandler.HandleDeleteFile(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify directory was deleted
		_, err = os.Stat(dirPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("delete a directory with contents using recursive=true", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "directory_with_contents")
		err := os.Mkdir(dirPath, 0755)
		require.NoError(t, err)

		// Create a file inside the directory
		filePath := filepath.Join(dirPath, "nested_file.txt")
		err = os.WriteFile(filePath, []byte("nested content"), 0644)
		require.NoError(t, err)

		// Create a subdirectory
		subDirPath := filepath.Join(dirPath, "subdirectory")
		err = os.Mkdir(subDirPath, 0755)
		require.NoError(t, err)

		// Verify directory and contents exist before deletion
		_, err = os.Stat(dirPath)
		require.NoError(t, err)
		_, err = os.Stat(filePath)
		require.NoError(t, err)
		_, err = os.Stat(subDirPath)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path":      dirPath,
					"recursive": true,
				},
			},
		}

		res, err := fsHandler.HandleDeleteFile(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify directory and all contents were deleted
		_, err = os.Stat(dirPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("try to delete directory without recursive flag", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "directory_no_recursive")
		err := os.Mkdir(dirPath, 0755)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": dirPath,
				},
			},
		}

		res, err := fsHandler.HandleDeleteFile(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)

		// Verify directory still exists
		_, err = os.Stat(dirPath)
		require.NoError(t, err)
	})

	t.Run("try to delete non-existent file", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "non_existent_file.txt")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": nonExistentPath,
				},
			},
		}

		res, err := fsHandler.HandleDeleteFile(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})

	t.Run("path is in a non-allowed directory", func(t *testing.T) {
		otherDir := t.TempDir()

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": filepath.Join(otherDir, "some_file.txt"),
				},
			},
		}

		res, err := fsHandler.HandleDeleteFile(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})
}
