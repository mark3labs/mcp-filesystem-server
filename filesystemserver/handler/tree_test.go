package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTree(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	// Create test directory structure
	// /tmpDir/
	//   ├── file1.txt
	//   ├── subdir1/
	//   │   ├── file2.txt
	//   │   └── subdir2/
	//   │       └── file3.txt
	//   └── emptydir/

	file1Path := filepath.Join(tmpDir, "file1.txt")
	err = os.WriteFile(file1Path, []byte("content1"), 0644)
	require.NoError(t, err)

	subdir1Path := filepath.Join(tmpDir, "subdir1")
	err = os.Mkdir(subdir1Path, 0755)
	require.NoError(t, err)

	file2Path := filepath.Join(subdir1Path, "file2.txt")
	err = os.WriteFile(file2Path, []byte("content2"), 0644)
	require.NoError(t, err)

	subdir2Path := filepath.Join(subdir1Path, "subdir2")
	err = os.Mkdir(subdir2Path, 0755)
	require.NoError(t, err)

	file3Path := filepath.Join(subdir2Path, "file3.txt")
	err = os.WriteFile(file3Path, []byte("content3"), 0644)
	require.NoError(t, err)

	emptydirPath := filepath.Join(tmpDir, "emptydir")
	err = os.Mkdir(emptydirPath, 0755)
	require.NoError(t, err)

	t.Run("tree with default depth", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": tmpDir,
				},
			},
		}

		res, err := fsHandler.HandleTree(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains tree structure
		require.Len(t, res.Content, 2)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Directory tree for")
		assert.Contains(t, textContent.Text, "max depth: 3")

		// Parse the JSON to verify structure
		lines := textContent.Text
		assert.Contains(t, lines, "file1.txt")
		assert.Contains(t, lines, "subdir1")
		assert.Contains(t, lines, "file2.txt")
		assert.Contains(t, lines, "subdir2")
		assert.Contains(t, lines, "file3.txt")
		assert.Contains(t, lines, "emptydir")

		// Verify embedded resource
		embeddedResource := res.Content[1].(mcp.EmbeddedResource)
		assert.Equal(t, "resource", embeddedResource.Type)
		assert.Equal(t, "application/json", embeddedResource.Resource.(mcp.TextResourceContents).MIMEType)
	})

	t.Run("tree with custom depth", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path":  tmpDir,
					"depth": 2.0, // Only go 2 levels deep
				},
			},
		}

		res, err := fsHandler.HandleTree(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "max depth: 2")

		// Should include file1.txt, subdir1, file2.txt, subdir2, emptydir
		// but NOT file3.txt (which is at depth 3)
		assert.Contains(t, textContent.Text, "file1.txt")
		assert.Contains(t, textContent.Text, "subdir1")
		assert.Contains(t, textContent.Text, "file2.txt")
		assert.Contains(t, textContent.Text, "subdir2")
		assert.Contains(t, textContent.Text, "emptydir")
		// file3.txt should not be included at depth 2
		assert.NotContains(t, textContent.Text, "file3.txt")
	})

	t.Run("tree with depth 1", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path":  tmpDir,
					"depth": 1.0, // Only show immediate children
				},
			},
		}

		res, err := fsHandler.HandleTree(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "max depth: 1")

		// Should only include immediate children
		assert.Contains(t, textContent.Text, "file1.txt")
		assert.Contains(t, textContent.Text, "subdir1")
		assert.Contains(t, textContent.Text, "emptydir")
		// Should not include nested files
		assert.NotContains(t, textContent.Text, "file2.txt")
		assert.NotContains(t, textContent.Text, "subdir2")
		assert.NotContains(t, textContent.Text, "file3.txt")
	})

	t.Run("tree of empty directory", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": emptydirPath,
				},
			},
		}

		res, err := fsHandler.HandleTree(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Directory tree for")

		// Parse JSON to verify it's a directory with no children
		jsonStart := textContent.Text[strings.Index(textContent.Text, "{"):]
		var tree FileNode
		err = json.Unmarshal([]byte(jsonStart), &tree)
		require.NoError(t, err)
		assert.Equal(t, "directory", tree.Type)
		assert.Equal(t, "emptydir", tree.Name)
		assert.Nil(t, tree.Children)
	})

	t.Run("try to tree a file instead of directory", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": file1Path,
				},
			},
		}

		res, err := fsHandler.HandleTree(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)

		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "not a directory")
	})

	t.Run("try to tree non-existent directory", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "non_existent_directory")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"path": nonExistentPath,
				},
			},
		}

		res, err := fsHandler.HandleTree(ctx, req)
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

		res, err := fsHandler.HandleTree(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})
}
