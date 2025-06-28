package handler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchFiles_Pattern(t *testing.T) {

	// setting up test folder
	// tmpDir/
	// - foo/
	//   - bar.h
	//   - test.c
	// - test.h
	// - test.c

	dir := t.TempDir()
	test_h := filepath.Join(dir, "test.h")
	err := os.WriteFile(test_h, []byte("foo"), 0644)
	require.NoError(t, err)

	test_c := filepath.Join(dir, "test.c")
	err = os.WriteFile(test_c, []byte("foo"), 0644)
	require.NoError(t, err)

	fooDir := filepath.Join(dir, "foo")
	err = os.MkdirAll(fooDir, 0755)
	require.NoError(t, err)

	foo_bar_h := filepath.Join(fooDir, "bar.h")
	err = os.WriteFile(foo_bar_h, []byte("foo"), 0644)
	require.NoError(t, err)

	foo_test_c := filepath.Join(fooDir, "test.c")
	err = os.WriteFile(foo_test_c, []byte("foo"), 0644)
	require.NoError(t, err)

	handler, err := NewFilesystemHandler(resolveAllowedDirs(t, dir))
	require.NoError(t, err)

	tests := []struct {
		info    string
		pattern string
		matches []string
	}{
		{info: "use placeholder with extension", pattern: "*.h", matches: []string{test_h, foo_bar_h}},
		{info: "use placeholder with name", pattern: "test.*", matches: []string{test_h, test_c}},
		{info: "same filename", pattern: "test.c", matches: []string{test_c, foo_test_c}},
	}

	for _, test := range tests {
		t.Run(test.info, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Name = "search_files"
			request.Params.Arguments = map[string]any{
				"path":    dir,
				"pattern": test.pattern,
			}

			result, err := handler.HandleSearchFiles(context.Background(), request)
			require.NoError(t, err)
			assert.False(t, result.IsError)
			assert.Len(t, result.Content, 1)

			for _, match := range test.matches {
				assert.Contains(t, result.Content[0].(mcp.TextContent).Text, match)
			}
		})
	}
}
