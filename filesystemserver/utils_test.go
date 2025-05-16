package filesystemserver_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-filesystem-server/filesystemserver"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestClient(t *testing.T, fss *server.MCPServer) client.MCPClient {
	t.Helper()

	mcpClient, err := client.NewInProcessClient(fss)
	require.NoError(t, err)
	t.Cleanup(func() { mcpClient.Close() })

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

	return mcpClient
}

func getTool(t *testing.T, mcpClient client.MCPClient, toolName string) *mcp.Tool {
	result, err := mcpClient.ListTools(context.Background(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	for _, tool := range result.Tools {
		if tool.Name == toolName {
			return &tool
		}
	}
	require.Fail(t, "Tool not found", toolName)
	return nil
}
