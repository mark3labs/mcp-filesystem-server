package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleReadMultipleFiles(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	// Create test files
	file1Path := filepath.Join(tmpDir, "file1.txt")
	file1Content := "This is the content of file 1"
	err = os.WriteFile(file1Path, []byte(file1Content), 0644)
	require.NoError(t, err)

	file2Path := filepath.Join(tmpDir, "file2.txt")
	file2Content := "This is the content of file 2"
	err = os.WriteFile(file2Path, []byte(file2Content), 0644)
	require.NoError(t, err)

	// Create a directory
	dirPath := filepath.Join(tmpDir, "test_directory")
	err = os.Mkdir(dirPath, 0755)
	require.NoError(t, err)

	t.Run("read multiple text files", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{file1Path, file2Path},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains content from both files
		require.GreaterOrEqual(t, len(res.Content), 4) // At least 2 headers + 2 content blocks

		// Convert all content to strings for easier checking
		var contentTexts []string
		for _, content := range res.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				contentTexts = append(contentTexts, textContent.Text)
			}
		}

		allText := strings.Join(contentTexts, "\n")
		assert.Contains(t, allText, "--- File: "+file1Path+" ---")
		assert.Contains(t, allText, "--- File: "+file2Path+" ---")
		assert.Contains(t, allText, file1Content)
		assert.Contains(t, allText, file2Content)
	})

	t.Run("read single file", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{file1Path},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the response contains content from the file
		require.GreaterOrEqual(t, len(res.Content), 2) // At least 1 header + 1 content block

		var contentTexts []string
		for _, content := range res.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				contentTexts = append(contentTexts, textContent.Text)
			}
		}

		allText := strings.Join(contentTexts, "\n")
		assert.Contains(t, allText, "--- File: "+file1Path+" ---")
		assert.Contains(t, allText, file1Content)
	})

	t.Run("try to read a directory", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{dirPath},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Should get a message about it being a directory
		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "is a directory")
		assert.Contains(t, textContent.Text, "Use list_directory tool")
	})

	t.Run("try to read non-existent file", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "non_existent.txt")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{nonExistentPath},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError) // The operation succeeds but individual files may have errors

		// Should get an error message about the file not existing
		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Error accessing")
		assert.Contains(t, textContent.Text, nonExistentPath)
	})

	t.Run("mix of valid and invalid files", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "non_existent.txt")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{file1Path, nonExistentPath, file2Path},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Should have content for valid files and error messages for invalid ones
		require.GreaterOrEqual(t, len(res.Content), 5) // At least 2 headers + 2 content blocks + 1 error

		var contentTexts []string
		for _, content := range res.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				contentTexts = append(contentTexts, textContent.Text)
			}
		}

		allText := strings.Join(contentTexts, "\n")
		assert.Contains(t, allText, "--- File: "+file1Path+" ---")
		assert.Contains(t, allText, "--- File: "+file2Path+" ---")
		assert.Contains(t, allText, file1Content)
		assert.Contains(t, allText, file2Content)
		assert.Contains(t, allText, "Error accessing")
		assert.Contains(t, allText, nonExistentPath)
	})

	t.Run("no files specified", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)

		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "No files specified to read")
	})

	t.Run("too many files", func(t *testing.T) {
		// Create a slice with more than 50 files (the maximum)
		var manyPaths []string
		for i := 0; i < 51; i++ {
			manyPaths = append(manyPaths, filepath.Join(tmpDir, "file.txt"))
		}

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": manyPaths,
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)

		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Too many files requested")
		assert.Contains(t, textContent.Text, "Maximum is 50")
	})

	t.Run("path in non-allowed directory", func(t *testing.T) {
		otherDir := t.TempDir()
		otherFile := filepath.Join(otherDir, "other.txt")

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"paths": []string{otherFile},
				},
			},
		}

		res, err := fsHandler.HandleReadMultipleFiles(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError) // The operation succeeds but individual files may have errors

		require.Len(t, res.Content, 1)
		textContent := res.Content[0].(mcp.TextContent)
		assert.Contains(t, textContent.Text, "Error with path")
		assert.Contains(t, textContent.Text, otherFile)
	})
}
