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

func TestHandleGetFileInfo(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("get file info for a file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "test_file.txt")
		fileContent := "Hello, world!"
		err := os.WriteFile(filePath, []byte(fileContent), 0644)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": filePath,
				},
			},
		}

		res, err := fsHandler.HandleGetFileInfo(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains file information
		require.Len(t, res.Content, 2)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "File information for:")
		assert.Contains(t, textContent.Text, filePath)
		assert.Contains(t, textContent.Text, "IsFile: true")
		assert.Contains(t, textContent.Text, "IsDirectory: false")
		assert.Contains(t, textContent.Text, "Size: 13 bytes") // Length of "Hello, world!"
	})

	t.Run("get file info for a directory", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "test_directory")
		err := os.Mkdir(dirPath, 0755)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": dirPath,
				},
			},
		}

		res, err := fsHandler.HandleGetFileInfo(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains directory information
		require.Len(t, res.Content, 2)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "File information for:")
		assert.Contains(t, textContent.Text, dirPath)
		assert.Contains(t, textContent.Text, "IsFile: false")
		assert.Contains(t, textContent.Text, "IsDirectory: true")
		assert.Contains(t, textContent.Text, "MIME Type: directory")
	})

	t.Run("file does not exist", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "non_existent_file.txt")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": nonExistentPath,
				},
			},
		}

		res, err := fsHandler.HandleGetFileInfo(ctx, req)
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

		res, err := fsHandler.HandleGetFileInfo(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})
}
