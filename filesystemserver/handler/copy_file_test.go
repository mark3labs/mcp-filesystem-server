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

func TestHandleCopyFile(t *testing.T) {
	// Setup a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a handler with the temp dir as an allowed path
	allowedDirs := resolveAllowedDirs(t, tmpDir)
	fsHandler, err := NewFilesystemHandler(allowedDirs)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a source file
	sourceFilePath := filepath.Join(tmpDir, "source.txt")
	err = os.WriteFile(sourceFilePath, []byte("hello world"), 0644)
	require.NoError(t, err)

	// Create a source directory
	sourceDirPath := filepath.Join(tmpDir, "source_dir")
	err = os.Mkdir(sourceDirPath, 0755)
	require.NoError(t, err)

	// Create a file inside the source directory
	nestedFilePath := filepath.Join(sourceDirPath, "nested.txt")
	err = os.WriteFile(nestedFilePath, []byte("nested hello"), 0644)
	require.NoError(t, err)

	t.Run("copy a single file", func(t *testing.T) {
		destinationPath := filepath.Join(tmpDir, "destination.txt")
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"source":      sourceFilePath,
					"destination": destinationPath,
				},
			},
		}

		res, err := fsHandler.HandleCopyFile(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the file was copied
		_, err = os.Stat(destinationPath)
		require.NoError(t, err)

		content, err := os.ReadFile(destinationPath)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(content))
	})

	t.Run("copy a directory", func(t *testing.T) {
		destinationPath := filepath.Join(tmpDir, "destination_dir")
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"source":      sourceDirPath,
					"destination": destinationPath,
				},
			},
		}

		res, err := fsHandler.HandleCopyFile(ctx, req)
		require.NoError(t, err)
		require.False(t, res.IsError)

		// Verify the directory was copied
		_, err = os.Stat(destinationPath)
		require.NoError(t, err)

		// Verify the nested file was copied
		nestedDestPath := filepath.Join(destinationPath, "nested.txt")
		_, err = os.Stat(nestedDestPath)
		require.NoError(t, err)

		content, err := os.ReadFile(nestedDestPath)
		require.NoError(t, err)
		assert.Equal(t, "nested hello", string(content))
	})

	t.Run("source does not exist", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"source":      filepath.Join(tmpDir, "non-existent-file.txt"),
					"destination": filepath.Join(tmpDir, "destination.txt"),
				},
			},
		}

		res, err := fsHandler.HandleCopyFile(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})

	t.Run("destination is in a non-allowed directory", func(t *testing.T) {
		// Setup a temporary directory for the test
		otherDir := t.TempDir()

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"source":      sourceFilePath,
					"destination": filepath.Join(otherDir, "destination.txt"),
				},
			},
		}

		res, err := fsHandler.HandleCopyFile(ctx, req)
		require.NoError(t, err)
		require.True(t, res.IsError)
	})
}
