package filesystemserver_test

import (
	"testing"

	"github.com/mark3labs/mcp-filesystem-server/filesystemserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regression test for invalid schema => missing items in array definition
func TestReadMultipleFilesSchema(t *testing.T) {
	fsserver, err := filesystemserver.NewFilesystemServer([]string{t.TempDir()})
	require.NoError(t, err)

	mcpClient := startTestClient(t, fsserver)

	tool := getTool(t, mcpClient, "read_multiple_files")
	require.NotNil(t, tool)

	// make sure that the tool has the correct schema
	paths, ok := tool.InputSchema.Properties["paths"]
	assert.True(t, ok)
	pathsMap, ok := paths.(map[string]any)
	assert.True(t, ok)
	_, ok = pathsMap["items"]
	assert.True(t, ok)
}
