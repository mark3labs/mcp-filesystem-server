package handler

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// handleModifyFile handles the modify_file tool request
func (fs *FilesystemHandler) HandleModifyFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// Extract arguments
	path, err := request.RequireString("path")
	if err != nil {
		return nil, err
	}

	find, err := request.RequireString("find")
	if err != nil {
		return nil, err
	}

	replace, err := request.RequireString("replace")
	if err != nil {
		return nil, err
	}

	// Extract optional arguments with defaults
	allOccurrences := true // Default value
	if val, err := request.RequireBool("all_occurrences"); err == nil {
		allOccurrences = val
	}

	useRegex := false // Default value
	if val, err := request.RequireBool("regex"); err == nil {
		useRegex = val
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

	// Validate path is within allowed directories
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
	if info, err := os.Stat(validPath); err == nil && info.IsDir() {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Error: Cannot modify a directory",
				},
			},
			IsError: true,
		}, nil
	}

	// Check if file exists
	if _, err := os.Stat(validPath); os.IsNotExist(err) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error: File not found: %s", path),
				},
			},
			IsError: true,
		}, nil
	}

	// Read file content
	content, err := os.ReadFile(validPath)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error reading file: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	originalContent := string(content)
	modifiedContent := ""
	replacementCount := 0

	// Perform the replacement
	if useRegex {
		re, err := regexp.Compile(find)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error: Invalid regular expression: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if allOccurrences {
			modifiedContent = re.ReplaceAllString(originalContent, replace)
			replacementCount = len(re.FindAllString(originalContent, -1))
		} else {
			matched := re.FindStringIndex(originalContent)
			if matched != nil {
				replacementCount = 1
				modifiedContent = originalContent[:matched[0]] + replace + originalContent[matched[1]:]
			} else {
				modifiedContent = originalContent
				replacementCount = 0
			}
		}
	} else {
		if allOccurrences {
			replacementCount = strings.Count(originalContent, find)
			modifiedContent = strings.ReplaceAll(originalContent, find, replace)
		} else {
			if index := strings.Index(originalContent, find); index != -1 {
				replacementCount = 1
				modifiedContent = originalContent[:index] + replace + originalContent[index+len(find):]
			} else {
				modifiedContent = originalContent
				replacementCount = 0
			}
		}
	}

	// Write modified content back to file
	if err := os.WriteFile(validPath, []byte(modifiedContent), 0644); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error writing to file: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Create response
	resourceURI := pathToResourceURI(validPath)

	// Get file info for the response
	info, err := os.Stat(validPath)
	if err != nil {
		// File was written but we couldn't get info
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("File modified successfully. Made %d replacement(s).", replacementCount),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("File modified successfully. Made %d replacement(s) in %s (file size: %d bytes)",
					replacementCount, path, info.Size()),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("Modified file: %s (%d bytes)", validPath, info.Size()),
				},
			},
		},
	}, nil
}