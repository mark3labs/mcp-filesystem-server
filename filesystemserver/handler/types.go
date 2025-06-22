package handler

import "time"

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
