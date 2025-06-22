package handler

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
)

func (fs *FilesystemHandler) HandleDeleteFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return nil, err
	}

	// Handle empty or relative paths like "." or "./" by converting to absolute path
	if path == "." || path == "./" {
		// Get current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error resolving current directory: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
		path = cwd
	}

	validPath, err := fs.validatePath(path)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Check if path exists
	info, err := os.Stat(validPath)
	if os.IsNotExist(err) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error: Path does not exist: %s", path),
				},
			},
			IsError: true,
		}, nil
	} else if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error accessing path: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Extract recursive parameter (optional, default: false)
	recursive := false
	if recursiveParam, err := request.RequireBool("recursive"); err == nil {
		recursive = recursiveParam
	}

	// Check if it's a directory and handle accordingly
	if info.IsDir() {
		if !recursive {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error: %s is a directory. Use recursive=true to delete directories.", path),
					},
				},
				IsError: true,
			}, nil
		}

		// It's a directory and recursive is true, so remove it
		if err := os.RemoveAll(validPath); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error deleting directory: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Successfully deleted directory %s", path),
				},
			},
		}, nil
	}

	// It's a file, delete it
	if err := os.Remove(validPath); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error deleting file: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Successfully deleted file %s", path),
			},
		},
	}, nil
}
