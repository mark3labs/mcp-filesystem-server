package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoot(t *testing.T) {
	handler, err := NewFilesystemHandler([]string{"/"})
	assert.NoError(t, err)
	assert.True(t, handler.isPathInAllowedDirs("/etc/hostname"))
}
