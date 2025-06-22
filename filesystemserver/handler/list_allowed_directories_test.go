package handler

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListAllowedDirectories(t *testing.T) {
	// Setup multiple temporary directories for the test
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create a handler with multiple allowed directories
	allowedDirs := resolveAllowedDirs(t, tmpDir1, tmpDir2)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("list allowed directories", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{},
			},
		}

		res, err := fsHandler.HandleListAllowedDirectories(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains the allowed directories
		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Allowed directories:")
		assert.Contains(t, textContent.Text, tmpDir1)
		assert.Contains(t, textContent.Text, tmpDir2)
		assert.Contains(t, textContent.Text, "file://")
	})

	t.Run("single allowed directory", func(t *testing.T) {
		singleDir := t.TempDir()
		singleAllowedDirs := resolveAllowedDirs(t, singleDir)
		singleFsHandler, err := NewFilesystemHandler(singleAllowedDirs)
		require.NoError(t, err)

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{},
			},
		}

		res, err := singleFsHandler.HandleListAllowedDirectories(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains the single allowed directory
		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Allowed directories:")
		assert.Contains(t, textContent.Text, singleDir)
		assert.Contains(t, textContent.Text, "file://")
	})
}
