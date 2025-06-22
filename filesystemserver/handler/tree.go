package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
)

func (fs *FilesystemHandler) HandleTree(
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

	// Extract depth parameter (optional, default: 3)
	depth := 3 // Default value
	if depthParam, err := request.RequireFloat("depth"); err == nil {
		depth = int(depthParam)
	}

	// Extract follow_symlinks parameter (optional, default: false)
	followSymlinks := false // Default value
	if followParam, err := request.RequireBool("follow_symlinks"); err == nil {
		followSymlinks = followParam
	}

	// Validate the path is within allowed directories
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

	// Check if it's a directory
	info, err := os.Stat(validPath)
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

	if !info.IsDir() {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Error: The specified path is not a directory",
				},
			},
			IsError: true,
		}, nil
	}

	// Build the tree structure
	tree, err := fs.buildTree(validPath, depth, 0, followSymlinks)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error building directory tree: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error generating JSON: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Create resource URI for the directory
	resourceURI := pathToResourceURI(validPath)

	// Return the result
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Directory tree for %s (max depth: %d):\n\n%s", validPath, depth, string(jsonData)),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "application/json",
					Text:     string(jsonData),
				},
			},
		},
	}, nil
}

// buildTree builds a tree representation of the filesystem starting at the given path
func (fs *FilesystemHandler) buildTree(path string, maxDepth int, currentDepth int, followSymlinks bool) (*FileNode, error) {
	// Validate the path
	validPath, err := fs.validatePath(path)
	if err != nil {
		return nil, err
	}

	// Get file info
	info, err := os.Stat(validPath)
	if err != nil {
		return nil, err
	}

	// Create the node
	node := &FileNode{
		Name:     filepath.Base(validPath),
		Path:     validPath,
		Modified: info.ModTime(),
	}

	// Set type and size
	if info.IsDir() {
		node.Type = "directory"

		// If we haven't reached the max depth, process children
		if currentDepth < maxDepth {
			// Read directory entries
			entries, err := os.ReadDir(validPath)
			if err != nil {
				return nil, err
			}

			// Process each entry
			for _, entry := range entries {
				entryPath := filepath.Join(validPath, entry.Name())

				// Handle symlinks
				if entry.Type()&os.ModeSymlink != 0 {
					if !followSymlinks {
						// Skip symlinks if not following them
						continue
					}

					// Resolve symlink
					linkDest, err := filepath.EvalSymlinks(entryPath)
					if err != nil {
						// Skip invalid symlinks
						continue
					}

					// Validate the symlink destination is within allowed directories
					if !fs.isPathInAllowedDirs(linkDest) {
						// Skip symlinks pointing outside allowed directories
						continue
					}

					entryPath = linkDest
				}

				// Recursively build child node
				childNode, err := fs.buildTree(entryPath, maxDepth, currentDepth+1, followSymlinks)
				if err != nil {
					// Skip entries with errors
					continue
				}

				// Add child to the current node
				node.Children = append(node.Children, childNode)
			}
		}
	} else {
		node.Type = "file"
		node.Size = info.Size()
	}

	return node, nil
}
