package filesystemserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadfile_Valid(t *testing.T) {
	// prepare temp directory
	dir := t.TempDir()
	content := "test-content"
	err := os.WriteFile(filepath.Join(dir, "test"), []byte(content), 0644)
	require.NoError(t, err)

	handler, err := NewFilesystemHandler([]string{dir})
	require.NoError(t, err)
	request := mcp.CallToolRequest{}
	request.Params.Name = "read_file"
	request.Params.Arguments = map[string]any{
		"path": filepath.Join(dir, "test"),
	}

	result, err := handler.handleReadFile(context.Background(), request)
	require.NoError(t, err)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, content, result.Content[0].(mcp.TextContent).Text)
}

func TestReadfile_Invalid(t *testing.T) {
	dir := t.TempDir()
	handler, err := NewFilesystemHandler([]string{dir})
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "read_file"
	request.Params.Arguments = map[string]any{
		"path": filepath.Join(dir, "test"),
	}

	result, err := handler.handleReadFile(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, fmt.Sprint(result.Content[0]), "no such file or directory")
}

func TestReadfile_NoAccess(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	handler, err := NewFilesystemHandler([]string{dir1})
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "read_file"
	request.Params.Arguments = map[string]any{
		"path": filepath.Join(dir2, "test"),
	}

	result, err := handler.handleReadFile(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, fmt.Sprint(result.Content[0]), "access denied - path outside allowed directories")
}

func TestReadMultipleFiles_Valid(t *testing.T) {
	dir := t.TempDir()
	content1 := "test-content-1"
	content2 := "test-content-2"
	err := os.WriteFile(filepath.Join(dir, "test1"), []byte(content1), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "test2"), []byte(content2), 0644)
	require.NoError(t, err)

	handler, err := NewFilesystemHandler([]string{dir})
	require.NoError(t, err)
	request := mcp.CallToolRequest{}
	request.Params.Name = "read_multiple_files"
	request.Params.Arguments = map[string]any{
		"paths": []any{
			filepath.Join(dir, "test1"),
			filepath.Join(dir, "test2"),
		},
	}

	result, err := handler.handleReadMultipleFiles(context.Background(), request)
	require.NoError(t, err)
	assert.Len(t, result.Content, 4) // 2 file headers + 2 file contents
	assert.Equal(t, fmt.Sprintf("--- File: %s ---", filepath.Join(dir, "test1")), result.Content[0].(mcp.TextContent).Text)
	assert.Equal(t, content1, result.Content[1].(mcp.TextContent).Text)
	assert.Equal(t, fmt.Sprintf("--- File: %s ---", filepath.Join(dir, "test2")), result.Content[2].(mcp.TextContent).Text)
	assert.Equal(t, content2, result.Content[3].(mcp.TextContent).Text)
}

func TestReadMultipleFiles_MissingPaths(t *testing.T) {
	dir := t.TempDir()
	handler, err := NewFilesystemHandler([]string{dir})
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "read_multiple_files"
	request.Params.Arguments = map[string]any{}

	_, err = handler.handleReadMultipleFiles(context.Background(), request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paths parameter is required")
}

func TestReadMultipleFiles_InvalidPathType(t *testing.T) {
	dir := t.TempDir()
	handler, err := NewFilesystemHandler([]string{dir})
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "read_multiple_files"
	request.Params.Arguments = map[string]any{
		"paths": []any{123}, // Invalid type (number instead of string)
	}

	_, err = handler.handleReadMultipleFiles(context.Background(), request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "each path must be a string")
}

func TestReadMultipleFiles_InvalidPathsType(t *testing.T) {
	dir := t.TempDir()
	handler, err := NewFilesystemHandler([]string{dir})
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "read_multiple_files"
	request.Params.Arguments = map[string]any{
		"paths": "not-an-array", // Invalid type (string instead of array)
	}

	_, err = handler.handleReadMultipleFiles(context.Background(), request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paths must be an array of strings")
}
