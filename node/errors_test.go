// Copyright 2026 Team 254. All Rights Reserved.

package node

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestExplainHubError(t *testing.T) {
	address := "http://192.168.50.10:8080"
	for _, test := range []struct {
		err      string
		expected string
	}{
		{"hub rejected connection (401): invalid shared secret", "doesn't match the hub's"},
		{"hub rejected connection (401): the hub has no shared secret configured", "doesn't have a shared secret"},
		{"hub rejected connection (404): this instance is not running as a multi-field hub", "isn't set up as the hub"},
		{"hub returned 403: this node has not been approved on the hub yet", "click Approve"},
		{"can't reach hub: dial tcp 192.168.50.10:8080: connectex: No connection could be made because the target " +
			"machine actively refused it.", "Can't reach the hub at " + address},
		{"Post \"http://192.168.50.10:8080/api/hub/results\": context deadline exceeded (Client.Timeout exceeded)",
			"Can't reach the hub"},
		{"lost connection to hub: EOF", "reconnecting"},
		{"hub rejected result for Q7: match Q7 is assigned to field 2, not field 1", "press Resync"},
		{"hub rejected connection (423): field 1 is already connected to the hub from 10.0.0.7; each field laptop " +
			"needs its own field ID", "needs its own Field ID"},
		{"hub rejected connection (400): invalid field ID \"3\"; it must be 1 or 2", "Set it to 1 or 2"},
	} {
		explanation := explainHubError(fmt.Errorf("%s", test.err), address)
		assert.Contains(t, explanation, test.expected, test.err)
		// The technical detail is kept for whoever is debugging.
		assert.Contains(t, explanation, test.err)
	}

	// Messages the node generates itself aren't repeated, and unknown errors pass through unchanged.
	assert.Equal(
		t,
		"Waiting for approval: on the hub, open Event Control and click Approve next to this field.",
		explainHubError(fmt.Errorf("waiting for the hub admin to approve this field"), address),
	)
	assert.Equal(t, "something odd", explainHubError(fmt.Errorf("something odd"), address))
}
