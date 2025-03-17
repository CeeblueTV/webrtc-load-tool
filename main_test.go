package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHelp(t *testing.T) {
	fl := initFlags()

	assert.False(t, isHelp([]string{"webrtc-load-tool", "-c", "30"}, fl))
	assert.True(t, isHelp([]string{}, fl))
	assert.True(t, isHelp([]string{"webrtc-load-tool"}, fl))
	assert.True(t, isHelp([]string{"webrtc-load-tool", "-h"}, fl))
	assert.True(t, isHelp([]string{"webrtc-load-tool", "--help"}, fl))
}
