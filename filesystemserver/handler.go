package filesystemserver

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/djherbis/times"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gobwas/glob"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// Maximum size for inline content (5MB)
	MAX_INLINE_SIZE = 5 * 1024 * 1024
	// Maximum size for base64 encoding (1MB)
	MAX_BASE64_SIZE = 1 * 1024 * 1024
	// Maximum number of search results to return (prevent excessive output)
	MAX_SEARCH_RESULTS = 1000
	// Maximum file size in bytes to search within (10MB)
	MAX_SEARCHABLE_SIZE = 10 * 1024 * 1024
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

// FileNode represents a node in the file tree
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"` // "file" or "directory"
	Size     int64       `json:"size,omitempty"`
	Modified time.Time   `json:"modified,omitempty"`
	Children []*FileNode `json:"children,omitempty"`
}

// SearchResult represents a single match in a file
type SearchResult struct {
	FilePath    string
	LineNumber  int
	LineContent string
	ResourceURI string
}

type FilesystemHandler struct {
	allowedDirs []string
}

func NewFilesystemHandler(allowedDirs []string) (*FilesystemHandler, error) {
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
	return &FilesystemHandler{
		allowedDirs: normalized,
	}, nil
}

// isPathInAllowedDirs checks if a path is within any of the allowed directories
func (fs *FilesystemHandler) isPathInAllowedDirs(path string) bool {
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

	// Check if the path is within any of the allowed directories
	for _, dir := range fs.allowedDirs {
		if strings.HasPrefix(absPath, dir) {
			return true
		}
	}
	return false
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

func (fs *FilesystemHandler) validatePath(requestedPath string) (string, error) {
	// Always convert to absolute path first
	abs, err := filepath.Abs(requestedPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Check if path is within allowed directories
	if !fs.isPathInAllowedDirs(abs) {
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

		if !fs.isPathInAllowedDirs(realParent) {
			return "", fmt.Errorf(
				"access denied - parent directory outside allowed directories",
			)
		}
		return abs, nil
	}

	// Check if the real path (after resolving symlinks) is still within allowed directories
	if !fs.isPathInAllowedDirs(realPath) {
		return "", fmt.Errorf(
			"access denied - symlink target outside allowed directories",
		)
	}

	return realPath, nil
}

func (fs *FilesystemHandler) getFileStats(path string) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}

	timespec := times.Get(info)

	return FileInfo{
		Size:        info.Size(),
		Created:     timespec.BirthTime(),
		Modified:    timespec.ModTime(),
		Accessed:    timespec.AccessTime(),
		IsDirectory: info.IsDir(),
		IsFile:      !info.IsDir(),
		Permissions: fmt.Sprintf("%o", info.Mode().Perm()),
	}, nil
}

func (fs *FilesystemHandler) searchFiles(
	rootPath, pattern string,
) ([]string, error) {
	var results []string
	globPattern := glob.MustCompile(pattern)

	err := filepath.Walk(
		rootPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors and continue
			}

			// Try to validate path
			if _, err := fs.validatePath(path); err != nil {
				return nil // Skip invalid paths
			}

			if globPattern.Match(info.Name()) {
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

// searchWithinFiles searches for a substring within file contents
func (fs *FilesystemHandler) searchWithinFiles(
	rootPath, substring string, maxDepth int, maxResults int,
) ([]SearchResult, error) {
	var results []SearchResult
	resultCount := 0
	currentDepth := 0

	// Walk the directory tree
	err := filepath.Walk(
		rootPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors and continue
			}

			// Check if we've reached the maximum number of results
			if resultCount >= maxResults {
				return filepath.SkipDir
			}

			// Try to validate path
			validPath, err := fs.validatePath(path)
			if err != nil {
				return nil // Skip invalid paths
			}

			// Skip directories, only search files
			if info.IsDir() {
				// Calculate depth for this directory
				relPath, err := filepath.Rel(rootPath, path)
				if err != nil {
					return nil // Skip on error
				}

				// Count separators to determine depth (empty or "." means we're at rootPath)
				if relPath == "" || relPath == "." {
					currentDepth = 0
				} else {
					currentDepth = strings.Count(relPath, string(filepath.Separator)) + 1
				}

				// Skip directories beyond max depth if specified
				if maxDepth > 0 && currentDepth >= maxDepth {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip files that are too large
			if info.Size() > MAX_SEARCHABLE_SIZE {
				return nil
			}

			// Determine MIME type and skip non-text files
			mimeType := detectMimeType(validPath)
			if !isTextFile(mimeType) {
				return nil
			}

			// Open the file and search for the substring
			file, err := os.Open(validPath)
			if err != nil {
				return nil // Skip files that can't be opened
			}
			defer file.Close()

			// Create a scanner to read the file line by line
			scanner := bufio.NewScanner(file)
			lineNum := 0

			// Scan each line
			for scanner.Scan() {
				lineNum++
				line := scanner.Text()

				// Check if the line contains the substring
				if strings.Contains(line, substring) {
					// Add to results
					results = append(results, SearchResult{
						FilePath:    validPath,
						LineNumber:  lineNum,
						LineContent: line,
						ResourceURI: pathToResourceURI(validPath),
					})
					resultCount++

					// Check if we've reached the maximum results
					if resultCount >= maxResults {
						return filepath.SkipDir
					}
				}
			}

			// Check for scanner errors
			if err := scanner.Err(); err != nil {
				return nil // Skip files with scanning errors
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
	// Use mimetype library for more accurate detection
	mtype, err := mimetype.DetectFile(path)
	if err != nil {
		// Fallback to extension-based detection if file can't be read
		ext := filepath.Ext(path)
		if ext != "" {
			mimeType := mime.TypeByExtension(ext)
			if mimeType != "" {
				return mimeType
			}
		}
		return "application/octet-stream" // Default
	}

	return mtype.String()
}

// isTextFile determines if a file is likely a text file based on MIME type
func isTextFile(mimeType string) bool {
	// Check for common text MIME types
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}

	// Common application types that are text-based
	textApplicationTypes := []string{
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-javascript",
		"application/typescript",
		"application/x-typescript",
		"application/x-yaml",
		"application/yaml",
		"application/toml",
		"application/x-sh",
		"application/x-shellscript",
	}

	if slices.Contains(textApplicationTypes, mimeType) {
		return true
	}

	// Check for +format types
	if strings.Contains(mimeType, "+xml") ||
		strings.Contains(mimeType, "+json") ||
		strings.Contains(mimeType, "+yaml") {
		return true
	}

	// Common code file types that might be misidentified
	if strings.HasPrefix(mimeType, "text/x-") {
		return true
	}

	if strings.HasPrefix(mimeType, "application/x-") &&
		(strings.Contains(mimeType, "script") ||
			strings.Contains(mimeType, "source") ||
			strings.Contains(mimeType, "code")) {
		return true
	}

	return false
}

// isImageFile determines if a file is an image based on MIME type
func isImageFile(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") ||
		(mimeType == "application/xml" && strings.HasSuffix(strings.ToLower(mimeType), ".svg"))
}

// pathToResourceURI converts a file path to a resource URI
func pathToResourceURI(path string) string {
	return "file://" + path
}

// Resource handler
func (fs *FilesystemHandler) handleReadResource(
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
	validPath, err := fs.validatePath(path)
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

func (fs *FilesystemHandler) handleReadFile(
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

	// Check if it's a text file
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
	} else if isImageFile(mimeType) {
		// It's an image file, return as image content
		if info.Size() <= MAX_BASE64_SIZE {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Image file: %s (%s, %d bytes)", validPath, mimeType, info.Size()),
					},
					mcp.ImageContent{
						Type:     "image",
						Data:     base64.StdEncoding.EncodeToString(content),
						MIMEType: mimeType,
					},
				},
			}, nil
		} else {
			// Too large for base64, return a reference
			resourceURI := pathToResourceURI(validPath)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Image file is too large to display inline (%d bytes). Access it via resource URI: %s", info.Size(), resourceURI),
					},
					mcp.EmbeddedResource{
						Type: "resource",
						Resource: mcp.TextResourceContents{
							URI:      resourceURI,
							MIMEType: "text/plain",
							Text:     fmt.Sprintf("Large image: %s (%s, %d bytes)", validPath, mimeType, info.Size()),
						},
					},
				},
			}, nil
		}
	} else {
		// It's another type of binary file
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

func (fs *FilesystemHandler) handleWriteFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return nil, err
	}
	content, err := request.RequireString("content")
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

func (fs *FilesystemHandler) handleListDirectory(
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

func (fs *FilesystemHandler) handleCreateDirectory(
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

func (fs *FilesystemHandler) handleCopyFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	source, err := request.RequireString("source")
	if err != nil {
		return nil, err
	}
	destination, err := request.RequireString("destination")
	if err != nil {
		return nil, err
	}

	// Handle empty or relative paths for source
	if source == "." || source == "./" {
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
		source = cwd
	}
	if destination == "." || destination == "./" {
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
		destination = cwd
	}

	validSource, err := fs.validatePath(source)
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
	srcInfo, err := os.Stat(validSource)
	if os.IsNotExist(err) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error: Source does not exist: %s", source),
				},
			},
			IsError: true,
		}, nil
	} else if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error accessing source: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	validDest, err := fs.validatePath(destination)
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

	// Perform the copy operation based on whether source is a file or directory
	if srcInfo.IsDir() {
		// It's a directory, copy recursively
		if err := copyDir(validSource, validDest); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error copying directory: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
	} else {
		// It's a file, copy directly
		if err := copyFile(validSource, validDest); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error copying file: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
	}

	resourceURI := pathToResourceURI(validDest)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf(
					"Successfully copied %s to %s",
					source,
					destination,
				),
			},
			mcp.EmbeddedResource{
				Type: "resource",
				Resource: mcp.TextResourceContents{
					URI:      resourceURI,
					MIMEType: "text/plain",
					Text:     fmt.Sprintf("Copied file: %s", validDest),
				},
			},
		},
	}, nil
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	// Open the source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create the destination file
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy the contents
	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Get source file mode
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Set the same file mode on destination
	return os.Chmod(dst, sourceInfo.Mode())
}

// copyDir recursively copies a directory tree from src to dst
func copyDir(src, dst string) error {
	// Get properties of source dir
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create the destination directory with the same permissions
	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	// Read directory entries
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Handle symlinks
		if entry.Type()&os.ModeSymlink != 0 {
			// For simplicity, we'll skip symlinks in this implementation
			continue
		}

		// Recursively copy subdirectories or copy files
		if entry.IsDir() {
			if err = copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err = copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (fs *FilesystemHandler) handleMoveFile(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	source, err := request.RequireString("source")
	if err != nil {
		return nil, err
	}
	destination, err := request.RequireString("destination")
	if err != nil {
		return nil, err
	}

	// Handle empty or relative paths for source
	if source == "." || source == "./" {
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
		source = cwd
	}

	// Handle empty or relative paths for destination
	if destination == "." || destination == "./" {
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
		destination = cwd
	}

	validSource, err := fs.validatePath(source)
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

	validDest, err := fs.validatePath(destination)
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

func (fs *FilesystemHandler) handleSearchFiles(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return nil, err
	}
	pattern, err := request.RequireString("pattern")
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

	results, err := fs.searchFiles(validPath, pattern)
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

func (fs *FilesystemHandler) handleTree(
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
	if depthParam, err := request.RequireFloat("depth"); err != nil {
		depth = int(depthParam)
	}

	// Extract follow_symlinks parameter (optional, default: false)
	followSymlinks := false // Default value
	if followParam, err := request.RequireBool("follow_symlinks"); err != nil {
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

func (fs *FilesystemHandler) handleGetFileInfo(
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

	info, err := fs.getFileStats(validPath)
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
					Text: fmt.Sprintf("%s: %s (%s, %d bytes)",
						fileTypeText,
						validPath,
						mimeType,
						info.Size),
				},
			},
		},
	}, nil
}

func (fs *FilesystemHandler) handleReadMultipleFiles(
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

func (fs *FilesystemHandler) handleDeleteFile(
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
	if recursiveParam, err := request.RequireBool("recursive"); err != nil {
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

// handleModifyFile handles the modify_file tool request
func (fs *FilesystemHandler) handleModifyFile(
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

func (fs *FilesystemHandler) handleSearchWithinFiles(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// Extract and validate parameters
	path, err := request.RequireString("path")
	if err != nil {
		return nil, err
	}
	substring, err := request.RequireString("substring")
	if err != nil {
		return nil, err
	}
	if substring == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "Error: substring cannot be empty",
				},
			},
			IsError: true,
		}, nil
	}

	// Extract optional depth parameter
	maxDepth := 0 // 0 means unlimited
	if depthArg, err := request.RequireFloat("depth"); err == nil {
		maxDepth = int(depthArg)
		if maxDepth < 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Error: depth cannot be negative",
					},
				},
				IsError: true,
			}, nil
		}
	}

	// Extract optional max_results parameter
	maxResults := MAX_SEARCH_RESULTS // default limit
	if maxResultsArg, err := request.RequireFloat("max_results"); err == nil {
		maxResults = int(maxResultsArg)
		if maxResults <= 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: "Error: max_results must be positive",
					},
				},
				IsError: true,
			}, nil
		}
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

	// Check if the path is a directory
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
					Text: "Error: search path must be a directory",
				},
			},
			IsError: true,
		}, nil
	}

	// Perform the search
	results, err := fs.searchWithinFiles(validPath, substring, maxDepth, maxResults)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error searching within files: %v", err),
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
					Text: fmt.Sprintf("No occurrences of '%s' found in files under %s", substring, path),
				},
			},
		}, nil
	}

	// Format search results
	var formattedResults strings.Builder
	formattedResults.WriteString(fmt.Sprintf("Found %d occurrences of '%s':\n\n", len(results), substring))

	// Group results by file for easier readability
	fileResultsMap := make(map[string][]SearchResult)
	for _, result := range results {
		fileResultsMap[result.FilePath] = append(fileResultsMap[result.FilePath], result)
	}

	// Display results grouped by file
	for filePath, fileResults := range fileResultsMap {
		resourceURI := pathToResourceURI(filePath)
		formattedResults.WriteString(fmt.Sprintf("File: %s (%s)\n", filePath, resourceURI))

		for _, result := range fileResults {
			// Truncate line content if too long (keeping context around the match)
			lineContent := result.LineContent
			if len(lineContent) > 100 {
				// Find the substring position
				substrPos := strings.Index(strings.ToLower(lineContent), strings.ToLower(substring))

				// Calculate start and end positions for context
				contextStart := max(0, substrPos-30)
				contextEnd := min(len(lineContent), substrPos+len(substring)+30)

				if contextStart > 0 {
					lineContent = "..." + lineContent[contextStart:contextEnd]
				} else {
					lineContent = lineContent[:contextEnd]
				}

				if contextEnd < len(result.LineContent) {
					lineContent += "..."
				}
			}

			formattedResults.WriteString(fmt.Sprintf("  Line %d: %s\n", result.LineNumber, lineContent))
		}
		formattedResults.WriteString("\n")
	}

	// If results were limited, note this in the output
	if len(results) >= maxResults {
		formattedResults.WriteString(fmt.Sprintf("\nNote: Results limited to %d matches. There may be more occurrences.", maxResults))
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

func (fs *FilesystemHandler) handleListAllowedDirectories(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// Remove the trailing separator for display purposes
	displayDirs := make([]string, len(fs.allowedDirs))
	for i, dir := range fs.allowedDirs {
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

// Helper function since Go < 1.21 doesn't have min/max functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Helper function since Go < 1.21 doesn't have min/max functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
