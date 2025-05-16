package filesystemserver_test

import (
	"testing"

	"github.com/mark3labs/mcp-filesystem-server/filesystemserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInProcess(t *testing.T) {
	fss, err := filesystemserver.NewFilesystemServer([]string{"."})
	require.NoError(t, err)

	mcpClient := startTestClient(t, fss)

	// just check for a specific tool
	tool := getTool(t, mcpClient, "read_file")
	assert.NotNil(t, tool, "read_file tool not found in the list of tools")
}
