package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
)

func (fs *FilesystemHandler) HandleReadMultipleFiles(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	pathsSlice, err := request.RequireStringSlice("paths")
	if err != nil {
		return nil, err
	}

	if len(pathsSlice) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "No files specified to read",
				},
			},
			IsError: true,
		}, nil
	}

	// Maximum number of files to read in a single request
	const maxFiles = 50
	if len(pathsSlice) > maxFiles {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Too many files requested. Maximum is %d files per request.", maxFiles),
				},
			},
			IsError: true,
		}, nil
	}

	// Process each file
	var results []mcp.Content
	for _, path := range pathsSlice {
		// Handle empty or relative paths like "." or "./" by converting to absolute path
		if path == "." || path == "./" {
			// Get current working directory
			cwd, err := os.Getwd()
			if err != nil {
				results = append(results, mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error resolving current directory for path '%s': %v", path, err),
				})
				continue
			}
			path = cwd
		}

		validPath, err := fs.validatePath(path)
		if err != nil {
			results = append(results, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Error with path '%s': %v", path, err),
			})
			continue
		}

		// Check if it's a directory
		info, err := os.Stat(validPath)
		if err != nil {
			results = append(results, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Error accessing '%s': %v", path, err),
			})
			continue
		}

		if info.IsDir() {
			// For directories, return a resource reference instead
			resourceURI := pathToResourceURI(validPath)
			results = append(results, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("'%s' is a directory. Use list_directory tool or resource URI: %s", path, resourceURI),
			})
			continue
		}

		// Determine MIME type
		mimeType := detectMimeType(validPath)

		// Check file size
		if info.Size() > MAX_INLINE_SIZE {
			// File is too large to inline, return a resource reference
			resourceURI := pathToResourceURI(validPath)
			results = append(results, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("File '%s' is too large to display inline (%d bytes). Access it via resource URI: %s",
					path, info.Size(), resourceURI),
			})
			continue
		}

		// Read file content
		content, err := os.ReadFile(validPath)
		if err != nil {
			results = append(results, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Error reading file '%s': %v", path, err),
			})
			continue
		}

		// Add file header
		results = append(results, mcp.TextContent{
			Type: "text",
			Text: fmt.Sprintf("--- File: %s ---", path),
		})

		// Check if it's a text file
		if isTextFile(mimeType) {
			// It's a text file, return as text
			results = append(results, mcp.TextContent{
				Type: "text",
				Text: string(content),
			})
		} else if isImageFile(mimeType) {
			// It's an image file, return as image content
			if info.Size() <= MAX_BASE64_SIZE {
				results = append(results, mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Image file: %s (%s, %d bytes)", path, mimeType, info.Size()),
				})
				results = append(results, mcp.ImageContent{
					Type:     "image",
					Data:     base64.StdEncoding.EncodeToString(content),
					MIMEType: mimeType,
				})
			} else {
				// Too large for base64, return a reference
				resourceURI := pathToResourceURI(validPath)
				results = append(results, mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Image file '%s' is too large to display inline (%d bytes). Access it via resource URI: %s",
						path, info.Size(), resourceURI),
				})
			}
		} else {
			// It's another type of binary file
			resourceURI := pathToResourceURI(validPath)

			if info.Size() <= MAX_BASE64_SIZE {
				// Small enough for base64 encoding
				results = append(results, mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Binary file: %s (%s, %d bytes)", path, mimeType, info.Size()),
				})
				results = append(results, mcp.EmbeddedResource{
					Type: "resource",
					Resource: mcp.BlobResourceContents{
						URI:      resourceURI,
						MIMEType: mimeType,
						Blob:     base64.StdEncoding.EncodeToString(content),
					},
				})
			} else {
				// Too large for base64, return a reference
				results = append(results, mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Binary file '%s' (%s, %d bytes). Access it via resource URI: %s",
						path, mimeType, info.Size(), resourceURI),
				})
			}
		}
	}

	return &mcp.CallToolResult{
		Content: results,
	}, nil
}