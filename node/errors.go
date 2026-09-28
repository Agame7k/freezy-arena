// Copyright 2026 Team 254. All Rights Reserved.
//
// Turns errors from talking to the hub into advice that a field operator can act on.

package node

import (
	"fmt"
	"strings"
)

// explainHubError returns a plain-English explanation of the given error with what to do about it, followed by the
// technical detail in brackets for whoever is debugging.
func explainHubError(err error, hubAddress string) string {
	detail := err.Error()
	lower := strings.ToLower(detail)
	address := strings.TrimSpace(hubAddress)
	if address == "" {
		address = "the hub"
	}

	var advice string
	switch {
	case strings.Contains(lower, "no hub address"):
		advice = "No hub address is set. Enter the hub's address on Settings → Multi-Field."
	case strings.Contains(lower, "invalid shared secret"):
		advice = "The shared secret on this field doesn't match the hub's. Enter the same secret on both machines " +
			"(Settings → Multi-Field here, Event Control on the hub)."
	case strings.Contains(lower, "no shared secret configured"):
		advice = "The hub doesn't have a shared secret yet. Set one on the hub's Event Control page, then enter the " +
			"same secret here."
	case strings.Contains(lower, "not running as a multi-field hub"):
		advice = fmt.Sprintf(
			"%s is running Cheesy Arena but isn't set up as the hub. Set its role to Hub on its Settings → "+
				"Multi-Field tab.", address,
		)
	case strings.Contains(lower, "already connected to the hub from"):
		advice = "Another laptop is already connected to the hub as this field. Each field laptop needs its own Field " +
			"ID: check Settings → Multi-Field on both (one Field 1, the other Field 2). This laptop keeps trying and " +
			"takes over if the other one disconnects."
	case strings.Contains(lower, "invalid field id"):
		advice = "This laptop's Field ID isn't valid. Set it to 1 or 2 on Settings → Multi-Field."
	case strings.Contains(lower, "not been approved") || strings.Contains(lower, "waiting for the hub admin"):
		advice = "Waiting for approval: on the hub, open Event Control and click Approve next to this field."
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "unreachable") || strings.Contains(lower, "can't reach hub") ||
		strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "forcibly closed"):
		advice = fmt.Sprintf(
			"Can't reach the hub at %s. Check that the hub is running, that the address and port are right, and "+
				"that both machines are on the same network. Results are saved here and will be sent when it's back.",
			address,
		)
	case strings.Contains(lower, "lost connection"):
		advice = "Lost the connection to the hub; reconnecting. Results are saved here and will be sent when it's back."
	case strings.Contains(lower, "hub rejected result"):
		advice = "The hub refused a result from this field (it is kept here). Fix the cause on the hub, then press " +
			"Resync on Settings → Multi-Field."
	default:
		return detail
	}
	if strings.HasPrefix(lower, "waiting for the hub admin") || strings.HasPrefix(lower, "no hub address") {
		// These messages come from this node itself, so the detail adds nothing.
		return advice
	}
	return advice + " [" + detail + "]"
}
