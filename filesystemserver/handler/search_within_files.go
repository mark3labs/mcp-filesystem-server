package handler

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func (fs *FilesystemHandler) HandleSearchWithinFiles(
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
	results, err := searchWithinFiles(validPath, substring, maxDepth, maxResults, fs)
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

// searchWithinFiles searches for a substring within file contents
func searchWithinFiles(
	rootPath, substring string, maxDepth int, maxResults int, fs *FilesystemHandler,
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
