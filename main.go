package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	// Maximum size for inline content (5MB)
	MAX_INLINE_SIZE = 5 * 1024 * 1024
	// Maximum size for base64 encoding (1MB)
	MAX_BASE64_SIZE = 1 * 1024 * 1024
)

type FileInfo struct {
	Size        int64     `json:"size"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
	Accessed    time.Time `json:"accessed"`
	IsDirectory bool      `json:"isDirectory"`
	IsFile      bool      `json:"isFile"`
	Permissions string    `json:"permissions"`
}

type FilesystemServer struct {
	allowedDirs []string
	server      *server.MCPServer
}

func NewFilesystemServer(allowedDirs []string) (*FilesystemServer, error) {
	// Normalize and validate directories
	normalized := make([]string, 0, len(allowedDirs))
	for _, dir := range allowedDirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve path %s: %w", dir, err)
		}

		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to access directory %s: %w",
				abs,
				err,
			)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s", abs)
		}

		// Ensure the path ends with a separator to prevent prefix matching issues
		// For example, /tmp/foo should not match /tmp/foobar
		normalized = append(normalized, filepath.Clean(abs)+string(filepath.Separator))
	}

	s := &FilesystemServer{
		allowedDirs: normalized,
		server: server.NewMCPServer(
			"secure-filesystem-server",
			"0.3.0",
			server.WithResourceCapabilities(true, true),
		),
	}

	// Register resource handlers
	s.server.AddResource(mcp.NewResource(
		"file://",
		"File System",
		mcp.WithResourceDescription("Access to files and directories on the local file system"),
	), s.handleReadResource)

	// Register tool handlers
	s.server.AddTool(mcp.NewTool(
		"read_file",
		mcp.WithDescription("Read the complete contents of a file from the file system."),
		mcp.WithString("path",
			mcp.Description("Path to the file to read"),
			mcp.Required(),
		),
	), s.handleReadFile)

	s.server.AddTool(mcp.NewTool(
		"write_file",
		mcp.WithDescription("Create a new file or overwrite an existing file with new content."),
		mcp.WithString("path",
			mcp.Description("Path where to write the file"),
			mcp.Required(),
		),
		mcp.WithString("content",
			mcp.Description("Content to write to the file"),
			mcp.Required(),
		),
	), s.handleWriteFile)

	s.server.AddTool(mcp.NewTool(
		"list_directory",
		mcp.WithDescription("Get a detailed listing of all files and directories in a specified path."),
		mcp.WithString("path",
			mcp.Description("Path of the directory to list"),
			mcp.Required(),
		),
	), s.handleListDirectory)

	s.server.AddTool(mcp.NewTool(
		"create_directory",
		mcp.WithDescription("Create a new directory or ensure a directory exists."),
		mcp.WithString("path",
			mcp.Description("Path of the directory to create"),
			mcp.Required(),
		),
	), s.handleCreateDirectory)

	s.server.AddTool(mcp.NewTool(
		"move_file",
		mcp.WithDescription("Move or rename files and directories."),
		mcp.WithString("source",
			mcp.Description("Source path of the file or directory"),
			mcp.Required(),
		),
		mcp.WithString("destination",
			mcp.Description("Destination path"),
			mcp.Required(),
		),
	), s.handleMoveFile)

	s.server.AddTool(mcp.NewTool(
		"search_files",
		mcp.WithDescription("Recursively search for files and directories matching a pattern."),
		mcp.WithString("path",
			mcp.Description("Starting path for the search"),
			mcp.Required(),
		),
		mcp.WithString("pattern",
			mcp.Description("Search pattern to match against file names"),
			mcp.Required(),
		),
	), s.handleSearchFiles)

	s.server.AddTool(mcp.NewTool(
		"get_file_info",
		mcp.WithDescription("Retrieve detailed metadata about a file or directory."),
		mcp.WithString("path",
			mcp.Description("Path to the file or directory"),
			mcp.Required(),
		),
	), s.handleGetFileInfo)

	s.server.AddTool(mcp.NewTool(
		"list_allowed_directories",
		mcp.WithDescription("Returns the list of directories that this server is allowed to access."),
	), s.handleListAllowedDirectories)

	return s, nil
}

// isPathInAllowedDirs checks if a path is within any of the allowed directories
func (s *FilesystemServer) isPathInAllowedDirs(path string) bool {
	// Ensure path is absolute and clean
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	
	// Add trailing separator to ensure we're checking a directory or a file within a directory
	// and not a prefix match (e.g., /tmp/foo should not match /tmp/foobar)
	if !strings.HasSuffix(absPath, string(filepath.Separator)) {
		// If it's a file, we need to check its directory
		if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
			absPath = filepath.Dir(absPath) + string(filepath.Separator)
		} else {
			absPath = absPath + string(filepath.Separator)
		}
	}

	for _, dir := range s.allowedDirs {
		if strings.HasPrefix(absPath, dir) {
			return true
		}
	}
	return false
}

func (s *FilesystemServer) validatePath(requestedPath string) (string, error) {
	abs, err := filepath.Abs(requestedPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Check if path is within allowed directories
	if !s.isPathInAllowedDirs(abs) {
		return "", fmt.Errorf(
			"access denied - path outside allowed directories: %s",
			abs,
		)
	}

	// Handle symlinks
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		// For new files, check parent directory
		parent := filepath.Dir(abs)
		realParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return "", fmt.Errorf("parent directory does not exist: %s", parent)
		}
		
		if !s.isPathInAllowedDirs(realParent) {
			return "", fmt.Errorf(
				"access denied - parent directory outside allowed directories",
			)
		}
		return abs, nil
	}

	// Check if the real path (after resolving symlinks) is still within allowed directories
	if !s.isPathInAllowedDirs(realPath) {
		return "", fmt.Errorf(
			"access denied - symlink target outside allowed directories",
		)
	}
	
	return realPath, nil
}

func (s *FilesystemServer) getFileStats(path string) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}

	return FileInfo{
		Size:        info.Size(),
		Created:     info.ModTime(), // Note: ModTime used as birth time isn't always available
		Modified:    info.ModTime(),
		Accessed:    info.ModTime(), // Note: Access time isn't always available
		IsDirectory: info.IsDir(),
		IsFile:      !info.IsDir(),
		Permissions: fmt.Sprintf("%o", info.Mode().Perm()),
	}, nil
}

func (s *FilesystemServer) searchFiles(
	rootPath, pattern string,
) ([]string, error) {
	var results []string
	pattern = strings.ToLower(pattern)

	err := filepath.Walk(
		rootPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors and continue
			}

			// Try to validate path
			if _, err := s.validatePath(path); err != nil {
				return nil // Skip invalid paths
			}

			if strings.Contains(strings.ToLower(info.Name()), pattern) {
				results = append(results, path)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// detectMimeType tries to determine the MIME type of a file
func detectMimeType(path string) string {
	// First try by extension
	ext := filepath.Ext(path)
	if ext != "" {
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			return mimeType
		}
	}

	// If that fails, try to read a bit of the file
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream" // Default
	}
	defer file.Close()

	// Read first 512 bytes to detect content type
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		return "application/octet-stream" // Default
	}

	// Use http.DetectContentType
	return http.DetectContentType(buffer[:n])
}

// isTextFile determines if a file is likely a text file based on MIME type
func isTextFile(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "application/javascript" ||
		mimeType == "application/x-javascript" ||
		strings.Contains(mimeType, "+xml") ||
		strings.Contains(mimeType, "+json")
}

// pathToResourceURI converts a file path to a resource URI
func pathToResourceURI(path string) string {
	return "file://" + path
}

// Resource handler
func (s *FilesystemServer) handleReadResource(
	ctx context.Context,
	request mcp.ReadResourceRequest,
) ([]mcp.ResourceContents, error) {
	uri := request.Params.URI
	
	// Check if it's a file:// URI
	if !strings.HasPrefix(uri, "file://") {
		return nil, fmt.Errorf("unsupported URI scheme: %s", uri)
	}
	
	// Extract the path from the URI
	path := strings.TrimPrefix(uri, "file://")
	
	// Validate the path
	validPath, err := s.validatePath(path)
	if err != nil {
		return nil, err
	}
	
	// Get file info
	fileInfo, err := os.Stat(validPath)
	if err != nil {
		return nil, err
	}
	
	// If it's a directory, return a listing
	if fileInfo.IsDir() {
		entries, err := os.ReadDir(validPath)
		if err != nil {
			return nil, err
		}
		
		var result strings.Builder
		result.WriteString(fmt.Sprintf("Directory listing for: %s\n\n", validPath))
		
		for _, entry := range entries {
			entryPath := filepath.Join(validPath, entry.Name())
			entryURI := pathToResourceURI(entryPath)
			
			if entry.IsDir() {
				result.WriteString(fmt.Sprintf("[DIR]  %s (%s)\n", entry.Name(), entryURI))
			} else {
				info, err := entry.Info()
				if err == nil {
					result.WriteString(fmt.Sprintf("[FILE] %s (%s) - %d bytes\n", 
						entry.Name(), entryURI, info.Size()))
				} else {
					result.WriteString(fmt.Sprintf("[FILE] %s (%s)\n", entry.Name(), entryURI))
				}
			}
		}
		
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: "text/plain",
				Text:     result.String(),
			},
		}, nil
	}
	
	// It's a file, determine how to handle it
	mimeType := detectMimeType(validPath)
	
	// Check file size
	if fileInfo.Size() > MAX_INLINE_SIZE {
		// File is too large to inline, return a reference instead
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: "text/plain",
				Text:     fmt.Sprintf("File is too large to display inline (%d bytes). Use the read_file tool to access specific portions.", fileInfo.Size()),
			},
		}, nil
	}
	
	// Read the file content
	content, err := os.ReadFile(validPath)
	if err != nil {
		return nil, err
	}
	
	// Handle based on content type
	if isTextFile(mimeType) {
		// It's a text file, return as text
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri,
				MIMEType: mimeType,
				Text:     string(content),
			},
		}, nil
	} else {
		// It's a binary file
		if fileInfo.Size() <= MAX_BASE64_SIZE {
			// Small enough for base64 encoding
			return []mcp.ResourceContents{
				mcp.BlobResourceContents{
					URI:      uri,
					MIMEType: mimeType,
					Blob:     base64.StdEncoding.EncodeToString(content),
				},
			}, nil
		} else {
			// Too large for base64, return a reference
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      uri,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("Binary file (%s, %d bytes). Use the read_file tool to access specific portions.", mimeType, fileInfo.Size()),
				},
			}, nil
		}
	}
}

// Tool handlers

func (s *FilesystemServer) handleReadFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path must be a string")
	}

	validPath, err := s.validatePath(path)
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
	
	if info.IsDir() {
		// For directories, return a resource reference instead
		resourceURI := pathToResourceURI(validPath)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("This is a directory. Use the resource URI to browse its contents: %s", resourceURI),
				},
				mcp.EmbeddedResource{
					Type: "resource",
					Resource: mcp.TextResourceContents{
						URI:      resourceURI,
						MIMEType: "text/plain",
						Text:     fmt.Sprintf("Directory: %s", validPath),
					},
				},
			},
		}, nil
	}

	// Determine MIME type
	mimeType := detectMimeType(validPath)
	
	// Check file size
	if info.Size() > MAX_INLINE_SIZE {
		// File is too large to inline, return a resource reference
		resourceURI := pathToResourceURI(validPath)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("File is too large to display inline (%d bytes). Access it via resource URI: %s", info.Size(), resourceURI),
				},
				mcp.EmbeddedResource{
					Type: "resource",
					Resource: mcp.TextResourceContents{
						URI:      resourceURI,
						MIMEType: "text/plain",
						Text:     fmt.Sprintf("Large file: %s (%s, %d bytes)", validPath, mimeType, info.Size()),
					},
				},
			},
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

	// Handle based on content type
	if isTextFile(mimeType) {
		// It's a text file, return as text
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: string(content),
				},
			},
		}, nil
	} else {
		// It's a binary file
		resourceURI := pathToResourceURI(validPath)
		
		if info.Size() <= MAX_BASE64_SIZE {
			// Small enough for base64 encoding
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Binary file: %s (%s, %d bytes)", validPath, mimeType, info.Size()),
					},
					mcp.EmbeddedResource{
						Type: "resource",
						Resource: mcp.BlobResourceContents{
							URI:      resourceURI,
							MIMEType: mimeType,
							Blob:     base64.StdEncoding.EncodeToString(content),
						},
					},
				},
			}, nil
		} else {
			// Too large for base64, return a reference
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Binary file: %s (%s, %d bytes). Access it via resource URI: %s", validPath, mimeType, info.Size(), resourceURI),
					},
					mcp.EmbeddedResource{
						Type: "resource",
						Resource: mcp.TextResourceContents{
							URI:      resourceURI,
							MIMEType: "text/plain",
							Text:     fmt.Sprintf("Binary file: %s (%s, %d bytes)", validPath, mimeType, info.Size()),
						},
					},
				},
			}, nil
		}
	}
}

func (s *FilesystemServer) handleWriteFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path must be a string")
	}
	content, ok := request.Params.Arguments["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content must be a string")
	}

	validPath, err := s.validatePath(path)
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
					Text: "Error: Cannot write to a directory",
				},
			},
			IsError: true,
		}, nil
	}

	// Create parent directories if they don't exist
	parentDir := filepath.Dir(validPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error creating parent directories: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	if err := os.WriteFile(validPath, []byte(content), 0644); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error writing file: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Get file info for the response
	info, err := os.Stat(validPath)
	if err != nil {
		// File was written but we couldn't get info
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Successfully wrote to %s", path),
				},
			},
		}, nil
	}

	resourceURI := pathToResourceURI(validPath)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Successfully wrote %d bytes to %s", info.Size(), path),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("File: %s (%d bytes)", validPath, info.Size()),
				},
			},
		},
	}, nil
}

func (s *FilesystemServer) handleListDirectory(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path must be a string")
	}

	validPath, err := s.validatePath(path)
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
					Text: "Error: Path is not a directory",
				},
			},
			IsError: true,
		}, nil
	}

	entries, err := os.ReadDir(validPath)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error reading directory: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Directory listing for: %s\n\n", validPath))
	
	for _, entry := range entries {
		entryPath := filepath.Join(validPath, entry.Name())
		resourceURI := pathToResourceURI(entryPath)
		
		if entry.IsDir() {
			result.WriteString(fmt.Sprintf("[DIR]  %s (%s)\n", entry.Name(), resourceURI))
		} else {
			info, err := entry.Info()
			if err == nil {
				result.WriteString(fmt.Sprintf("[FILE] %s (%s) - %d bytes\n", 
					entry.Name(), resourceURI, info.Size()))
			} else {
				result.WriteString(fmt.Sprintf("[FILE] %s (%s)\n", entry.Name(), resourceURI))
			}
		}
	}

	// Return both text content and embedded resource
	resourceURI := pathToResourceURI(validPath)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result.String(),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("Directory: %s", validPath),
				},
			},
		},
	}, nil
}

func (s *FilesystemServer) handleCreateDirectory(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path must be a string")
	}

	validPath, err := s.validatePath(path)
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

	// Check if path already exists
	if info, err := os.Stat(validPath); err == nil {
		if info.IsDir() {
			resourceURI := pathToResourceURI(validPath)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Directory already exists: %s", path),
					},
					mcp.EmbeddedResource{
						Type: "resource",
						Resource: mcp.TextResourceContents{
							URI:      resourceURI,
							MIMEType: "text/plain",
							Text:     fmt.Sprintf("Directory: %s", validPath),
						},
					},
				},
			}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error: Path exists but is not a directory: %s", path),
				},
			},
			IsError: true,
		}, nil
	}

	if err := os.MkdirAll(validPath, 0755); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error creating directory: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	resourceURI := pathToResourceURI(validPath)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Successfully created directory %s", path),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("Directory: %s", validPath),
				},
			},
		},
	}, nil
}

func (s *FilesystemServer) handleMoveFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	source, ok := request.Params.Arguments["source"].(string)
	if !ok {
		return nil, fmt.Errorf("source must be a string")
	}
	destination, ok := request.Params.Arguments["destination"].(string)
	if !ok {
		return nil, fmt.Errorf("destination must be a string")
	}

	validSource, err := s.validatePath(source)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error with source path: %v", err),
				},
			},
			IsError: true,
		}, nil
	}
	
	// Check if source exists
	if _, err := os.Stat(validSource); os.IsNotExist(err) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error: Source does not exist: %s", source),
				},
			},
			IsError: true,
		}, nil
	}
	
	validDest, err := s.validatePath(destination)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error with destination path: %v", err),
				},
			},
			IsError: true,
		}, nil
	}
	
	// Create parent directory for destination if it doesn't exist
	destDir := filepath.Dir(validDest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error creating destination directory: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	if err := os.Rename(validSource, validDest); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error moving file: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	resourceURI := pathToResourceURI(validDest)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf(
					"Successfully moved %s to %s",
					source,
					destination,
				),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("Moved file: %s", validDest),
				},
			},
		},
	}, nil
}

func (s *FilesystemServer) handleSearchFiles(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path must be a string")
	}
	pattern, ok := request.Params.Arguments["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("pattern must be a string")
	}

	validPath, err := s.validatePath(path)
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
					Text: "Error: Search path must be a directory",
				},
			},
			IsError: true,
		}, nil
	}

	results, err := s.searchFiles(validPath, pattern)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error searching files: %v",
						err),
				},
			},
			IsError: true,
		}, nil
	}

	if len(results) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("No files found matching pattern '%s' in %s", pattern, path),
				},
			},
		}, nil
	}

	// Format results with resource URIs
	var formattedResults strings.Builder
	formattedResults.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	
	for _, result := range results {
		resourceURI := pathToResourceURI(result)
		info, err := os.Stat(result)
		if err == nil {
			if info.IsDir() {
				formattedResults.WriteString(fmt.Sprintf("[DIR]  %s (%s)\n", result, resourceURI))
			} else {
				formattedResults.WriteString(fmt.Sprintf("[FILE] %s (%s) - %d bytes\n", 
					result, resourceURI, info.Size()))
			}
		} else {
			formattedResults.WriteString(fmt.Sprintf("%s (%s)\n", result, resourceURI))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: formattedResults.String(),
			},
		},
	}, nil
}

func (s *FilesystemServer) handleGetFileInfo(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, ok := request.Params.Arguments["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path must be a string")
	}

	validPath, err := s.validatePath(path)
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

	info, err := s.getFileStats(validPath)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting file info: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Get MIME type for files
	mimeType := "directory"
	if info.IsFile {
		mimeType = detectMimeType(validPath)
	}

	resourceURI := pathToResourceURI(validPath)
	
	// Determine file type text
	var fileTypeText string
	if info.IsDirectory {
		fileTypeText = "Directory"
	} else {
		fileTypeText = "File"
	}
	
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf(
					"File information for: %s\n\nSize: %d bytes\nCreated: %s\nModified: %s\nAccessed: %s\nIsDirectory: %v\nIsFile: %v\nPermissions: %s\nMIME Type: %s\nResource URI: %s",
					validPath,
					info.Size,
					info.Created.Format(time.RFC3339),
					info.Modified.Format(time.RFC3339),
					info.Accessed.Format(time.RFC3339),
					info.IsDirectory,
					info.IsFile,
					info.Permissions,
					mimeType,
					resourceURI,
				),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("%s: %s (%s, %d bytes)", 
						fileTypeText, 
						validPath, 
						mimeType, 
						info.Size),
				},
			},
		},
	}, nil
}

func (s *FilesystemServer) handleListAllowedDirectories(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// Remove the trailing separator for display purposes
	displayDirs := make([]string, len(s.allowedDirs))
	for i, dir := range s.allowedDirs {
		displayDirs[i] = strings.TrimSuffix(dir, string(filepath.Separator))
	}

	var result strings.Builder
	result.WriteString("Allowed directories:\n\n")
	
	for _, dir := range displayDirs {
		resourceURI := pathToResourceURI(dir)
		result.WriteString(fmt.Sprintf("%s (%s)\n", dir, resourceURI))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result.String(),
			},
		},
	}, nil
}

func (s *FilesystemServer) Serve() error {
	return server.ServeStdio(s.server)
}

func main() {
	// Parse command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(
			os.Stderr,
			"Usage: %s <allowed-directory> [additional-directories...]\n",
			os.Args[0],
		)
		os.Exit(1)
	}

	// Create and start the server
	fs, err := NewFilesystemServer(os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Serve requests
	if err := fs.Serve(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
