// Copyright 2026 Team 254. All Rights Reserved.
//
// Model for tracking the number of connected referee panels.

package field

import (
	"sync"
)

type RefereePanelRegistry struct {
	numHeadReferee int
	numReferee     int
	mutex          sync.Mutex
}

// Records a newly connected referee panel.
func (registry *RefereePanelRegistry) RegisterPanel(isHeadReferee bool) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	if isHeadReferee {
		registry.numHeadReferee++
	} else {
		registry.numReferee++
	}
}

// Records a disconnected referee panel.
func (registry *RefereePanelRegistry) UnregisterPanel(isHeadReferee bool) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	if isHeadReferee {
		registry.numHeadReferee = max(registry.numHeadReferee-1, 0)
	} else {
		registry.numReferee = max(registry.numReferee-1, 0)
	}
}

// Returns the number of connected head referee and referee panels.
func (registry *RefereePanelRegistry) GetNumPanels() (int, int) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	return registry.numHeadReferee, registry.numReferee
}
