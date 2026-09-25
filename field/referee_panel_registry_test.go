// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRefereePanelRegistry(t *testing.T) {
	var registry RefereePanelRegistry
	numHead, numRef := registry.GetNumPanels()
	assert.Equal(t, 0, numHead)
	assert.Equal(t, 0, numRef)

	registry.RegisterPanel(true)
	registry.RegisterPanel(false)
	registry.RegisterPanel(false)
	numHead, numRef = registry.GetNumPanels()
	assert.Equal(t, 1, numHead)
	assert.Equal(t, 2, numRef)

	registry.UnregisterPanel(false)
	registry.UnregisterPanel(true)
	registry.UnregisterPanel(true)
	numHead, numRef = registry.GetNumPanels()
	assert.Equal(t, 0, numHead)
	assert.Equal(t, 1, numRef)
}
