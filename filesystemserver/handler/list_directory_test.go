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

func TestHandleListDirectory(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	// Create test directory structure
	subDir := filepath.Join(tmpDir, "subdirectory")
	err = os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(tmpDir, "test_file.txt")
	err = os.WriteFile(testFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	t.Run("list directory with files and subdirectories", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": tmpDir,
				},
			},
		}

		res, err := fsHandler.HandleListDirectory(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains directory listing
		require.Len(t, res.Content, 2)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Directory listing for:")
		assert.Contains(t, textContent.Text, tmpDir)
		assert.Contains(t, textContent.Text, "[DIR]  subdirectory")
		assert.Contains(t, textContent.Text, "[FILE] test_file.txt")
		assert.Contains(t, textContent.Text, "11 bytes") // Length of "hello world"
		assert.Contains(t, textContent.Text, "file://")

		// Verify embedded resource
		embeddedResource := res.Content[1].(mcp.EmbeddedResource)
		assert.Equal(t, "resource", embeddedResource.Type)
	})

	t.Run("list empty directory", func(t *testing.T) {
		emptyDir := filepath.Join(tmpDir, "empty_directory")
		err := os.Mkdir(emptyDir, 0755)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": emptyDir,
				},
			},
		}

		res, err := fsHandler.HandleListDirectory(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains directory listing for empty directory
		require.Len(t, res.Content, 2)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Directory listing for:")
		assert.Contains(t, textContent.Text, emptyDir)
	})

	t.Run("try to list a file instead of directory", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": testFile,
				},
			},
		}

		res, err := fsHandler.HandleListDirectory(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)

		// Verify error message
		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Path is not a directory")
	})

	t.Run("try to list non-existent directory", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "non_existent_directory")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": nonExistentPath,
				},
			},
		}

		res, err := fsHandler.HandleListDirectory(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})

	t.Run("path is in a non-allowed directory", func(t *testing.T) {
		otherDir := t.TempDir()

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": otherDir,
				},
			},
		}

		res, err := fsHandler.HandleListDirectory(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})
}
