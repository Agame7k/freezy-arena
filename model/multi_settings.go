// Copyright 2026 Team 254. All Rights Reserved.
//
// Enumerations and helpers for the multi-conference and multi-field event settings.

package model

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MaxFieldId is the highest field ID in a multi-field event; fields are numbered from 1.
const MaxFieldId = 2

// ChampionshipFormat determines how the two conference brackets are joined into an event championship.
type ChampionshipFormat int

const (
	ChampionsSeriesChampionship ChampionshipFormat = iota
	CrossoverSemisChampionship
	DoubleDeckerChampionship
	NoChampionship
)

// ChampionshipFieldMode determines which field plays each championship match.
type ChampionshipFieldMode int

const (
	ChampionshipFieldFixed1 ChampionshipFieldMode = iota
	ChampionshipFieldFixed2
	ChampionshipFieldAlternate
	ChampionshipFieldChooseAtLoad
)

// AllianceFinalizeMode determines whether each conference's alliance selection is finalized separately.
type AllianceFinalizeMode int

const (
	FinalizePerConference AllianceFinalizeMode = iota
	FinalizeBothAtOnce
)

// MultiFieldRole is the role this instance plays in a multi-field event.
type MultiFieldRole int

const (
	StandaloneRole MultiFieldRole = iota
	HubRole
	NodeRole
)

func (role MultiFieldRole) String() string {
	switch role {
	case HubRole:
		return "Hub"
	case NodeRole:
		return "Node"
	default:
		return "Standalone"
	}
}

// MultiFieldRoleFromString parses a role name as used by the command-line flag and settings page.
func MultiFieldRoleFromString(role string) (MultiFieldRole, bool) {
	switch role {
	case "standalone", "Standalone", "":
		return StandaloneRole, true
	case "hub", "Hub":
		return HubRole, true
	case "node", "Node":
		return NodeRole, true
	}
	return StandaloneRole, false
}

// QualFieldAssignmentMode determines how qualification matches are assigned to fields.
type QualFieldAssignmentMode int

const (
	AlternateFieldAssignment QualFieldAssignmentMode = iota
	BlocksFieldAssignment
	DynamicFieldAssignment
)

// IsHub returns true if this instance is the hub of a multi-field event.
func (settings *EventSettings) IsHub() bool {
	return settings.MultiFieldRole == HubRole
}

// IsNode returns true if this instance is a field node of a multi-field event.
func (settings *EventSettings) IsNode() bool {
	return settings.MultiFieldRole == NodeRole
}

// IsMultiField returns true if this instance is part of a multi-field event.
func (settings *EventSettings) IsMultiField() bool {
	return settings.MultiFieldRole != StandaloneRole
}

// EffectiveChampionshipSeriesLength returns the championship series length, defaulting to three.
func (settings *EventSettings) EffectiveChampionshipSeriesLength() int {
	if settings.ChampionshipSeriesLength == 1 || settings.ChampionshipSeriesLength == 5 {
		return settings.ChampionshipSeriesLength
	}
	return 3
}

// DisplayFieldName returns the name of this instance's field, for showing in display headers.
func (settings *EventSettings) DisplayFieldName() string {
	if !settings.IsNode() {
		return ""
	}
	if settings.FieldName != "" {
		return settings.FieldName
	}
	return "Field " + strconv.Itoa(settings.FieldId)
}

// CopySharedEventSettings copies the event-wide settings that a hub distributes to its nodes from the source into the
// destination, leaving everything that is local to a machine (role, field identity, hub connection, passwords and all
// hardware addresses) untouched.
func CopySharedEventSettings(destination, source *EventSettings) {
	destination.Name = source.Name
	destination.LogoSuffix = source.LogoSuffix
	destination.PlayoffType = source.PlayoffType
	destination.NumPlayoffAlliances = source.NumPlayoffAlliances
	destination.SelectionRound2Order = source.SelectionRound2Order
	destination.SelectionRound3Order = source.SelectionRound3Order
	destination.SelectionShowUnpickedTeams = source.SelectionShowUnpickedTeams
	destination.TbaEventCode = source.TbaEventCode
	destination.AutoDurationSec = source.AutoDurationSec
	destination.PauseDurationSec = source.PauseDurationSec
	destination.TransitionShiftDurationSec = source.TransitionShiftDurationSec
	destination.ShiftDurationSec = source.ShiftDurationSec
	destination.EndgameDurationSec = source.EndgameDurationSec
	destination.EnergizedBonusThreshold = source.EnergizedBonusThreshold
	destination.SuperchargedBonusThreshold = source.SuperchargedBonusThreshold
	destination.TraversalBonusThreshold = source.TraversalBonusThreshold
	destination.MultiConferenceEnabled = source.MultiConferenceEnabled
	destination.PreferMixedConferenceAlliances = source.PreferMixedConferenceAlliances
	destination.ChampionshipFormat = source.ChampionshipFormat
	destination.ChampionshipSeriesLength = source.ChampionshipSeriesLength
	destination.ChampionshipFieldMode = source.ChampionshipFieldMode
	destination.ChampionshipBreakSec = source.ChampionshipBreakSec
	destination.AllianceFinalizeMode = source.AllianceFinalizeMode
	destination.QualFieldAssignmentMode = source.QualFieldAssignmentMode
	destination.MinTurnaroundSec = source.MinTurnaroundSec
	destination.SinglePlayoffFieldInterleave = source.SinglePlayoffFieldInterleave
}

// Letters and digits for generated shared secrets, leaving out ones that are easily confused when read aloud or copied
// by hand (0/o, 1/l/i).
const sharedSecretAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GenerateSharedSecret returns a random shared secret that is easy to read out and type on a field laptop, e.g.
// "k7pm-4xqr".
func GenerateSharedSecret() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	secret := make([]byte, 0, 9)
	for i, value := range random {
		if i == 4 {
			secret = append(secret, '-')
		}
		secret = append(secret, sharedSecretAlphabet[int(value)%len(sharedSecretAlphabet)])
	}
	return string(secret)
}

// DefaultHubPort is the port assumed for a hub address that doesn't give one, since Cheesy Arena serves on 8080.
const DefaultHubPort = "8080"

// NormalizeHubAddress turns what an operator types for the hub address (e.g. "192.168.50.10") into a full URL (e.g.
// "http://192.168.50.10:8080"), or returns an error explaining what is wrong with it. An empty address stays empty.
func NormalizeHubAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", nil
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("%q isn't a valid hub address; use the hub's IP address, e.g. 192.168.50.10", address)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("the hub address must start with http:// (got %q)", parsed.Scheme+"://")
	}
	if parsed.Port() == "" && parsed.Scheme == "http" {
		parsed.Host = parsed.Host + ":" + DefaultHubPort
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
