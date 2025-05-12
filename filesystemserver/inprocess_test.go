package filesystemserver_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-filesystem-server/filesystemserver"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInProcess(t *testing.T) {
	fss, err := filesystemserver.NewFilesystemServer([]string{"."})
	require.NoError(t, err)

	mcpClient, err := client.NewInProcessClient(fss)
	require.NoError(t, err)

	err = mcpClient.Start(context.Background())
	require.NoError(t, err)

	// Initialize the client
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}
	result, err := mcpClient.Initialize(context.Background(), initRequest)
	require.NoError(t, err)
	assert.Equal(t, "secure-filesystem-server", result.ServerInfo.Name)
	assert.Equal(t, filesystemserver.Version, result.ServerInfo.Version)

	// just check for a specific tool
	toolListResult, err := mcpClient.ListTools(context.Background(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	assert.NotEmpty(t, toolListResult.Tools)
	found := false
	for _, tool := range toolListResult.Tools {
		if tool.Name == "read_file" {
			found = true
			break
		}
	}
	assert.True(t, found, "read_file tool not found in the list of tools")

	err = mcpClient.Close()
	require.NoError(t, err)
}
