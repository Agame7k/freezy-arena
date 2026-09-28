// Copyright 2026 Team 254. All Rights Reserved.
//
// Arena support for multi-field events (hub and node roles) and multi-conference alliance selection.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"log"
	"time"
)

// NodeLink is implemented by the node client and lets the arena of a field node talk to its hub.
type NodeLink interface {
	// SubmitResult delivers a committed result to the hub (or queues it if the hub can't be reached in time).
	SubmitResult(match *model.Match, matchResult *model.MatchResult, isEdit bool) (*ResultReceipt, error)
	// ClaimNextMatch asks the hub for the next match to play on this field (dynamic field assignment).
	ClaimNextMatch() (*model.Match, error)
	// ReleaseMatch gives a claimed but unplayed match back to the hub.
	ReleaseMatch(matchId int) error
	// CheckTeamsAvailable returns an error if any of the given teams is currently on the other field.
	CheckTeamsAvailable(teamIds []int) error
	// Status returns the current state of the connection to the hub.
	Status() NodeLinkStatus
}

// ResultReceipt describes the outcome of submitting a result to the hub.
type ResultReceipt struct {
	// Rankings returned by the hub after applying the result, if it answered in time.
	Rankings game.Rankings
	// True if the hub didn't answer in time and the result is waiting in the outbox.
	Queued bool
	// True if the hub had already applied this exact result.
	Duplicate bool
}

// NodeLinkStatus summarizes a node's connection to its hub.
type NodeLinkStatus struct {
	Connected bool
	Approved  bool
	// True if the hub can be reached but refused the connection (e.g. wrong secret or field ID).
	Refused        bool
	OutboxSize     int
	RejectedCount  int
	LastError      string
	LastContact    time.Time
	MirrorPending  bool
	FieldTeams     map[int][]int
	SyncedVersions map[string]int64
}

// MultiFieldStatus is published to the match play and event control pages.
type MultiFieldStatus struct {
	Role      string
	FieldId   int
	FieldName string
	IsNode    bool
	IsHub     bool
	Connected bool
	Approved  bool
	// True if the hub can be reached but refused this node's connection, so it isn't simply offline.
	Refused    bool
	OutboxSize int
	// Results that the hub refused; they stay on this node until resolved (see the results bundle export).
	RejectedCount int
	LastError     string
	SimulateMode  bool
}

// ApplyStartupOverrides records the multi-field settings given on the command line (so that several instances can be
// started on one machine for testing) into the event settings before the arena starts.
type StartupOverrides struct {
	Role            string
	FieldId         int
	FieldName       string
	HubAddress      string
	HubSharedSecret string
}

// ApplyStartupOverrides saves any non-empty command-line overrides into the local event settings and reloads them.
func (arena *Arena) ApplyStartupOverrides(overrides StartupOverrides) error {
	settings := arena.EventSettings
	changed := false
	if overrides.Role != "" {
		role, ok := model.MultiFieldRoleFromString(overrides.Role)
		if !ok {
			return fmt.Errorf("invalid role %q; must be standalone, hub or node", overrides.Role)
		}
		settings.MultiFieldRole = role
		changed = true
	}
	if overrides.FieldId > 0 {
		settings.FieldId = overrides.FieldId
		if overrides.FieldName == "" {
			settings.FieldName = fmt.Sprintf("Field %d", overrides.FieldId)
		}
		changed = true
	}
	if overrides.FieldName != "" {
		settings.FieldName = overrides.FieldName
		changed = true
	}
	if overrides.HubAddress != "" {
		hubAddress, err := model.NormalizeHubAddress(overrides.HubAddress)
		if err != nil {
			return err
		}
		settings.HubAddress = hubAddress
		changed = true
	}
	if overrides.HubSharedSecret != "" {
		settings.HubSharedSecret = overrides.HubSharedSecret
		changed = true
	}
	if !changed {
		return nil
	}
	if err := arena.Database.UpdateEventSettings(settings); err != nil {
		return err
	}
	return arena.LoadSettings()
}

// EnsureHubSharedSecret makes up and saves a shared secret for a hub that doesn't have one, so that fields can connect as
// soon as the operator copies it from Event Control.
func (arena *Arena) EnsureHubSharedSecret() error {
	if !arena.EventSettings.IsHub() || arena.EventSettings.HubSharedSecret != "" {
		return nil
	}
	arena.EventSettings.HubSharedSecret = model.GenerateSharedSecret()
	if err := arena.Database.UpdateEventSettings(arena.EventSettings); err != nil {
		return err
	}
	log.Printf("Generated the hub's shared secret; it is shown on the Event Control page")
	return arena.LoadSettings()
}

// UsesFieldHardware returns false if the arena should not talk to any field hardware (because it is a hub, which has
// no field, or because hardware is being simulated for testing).
func (arena *Arena) UsesFieldHardware() bool {
	return !arena.SimulateHardware && !arena.EventSettings.IsHub()
}

// GetMultiFieldStatus returns the current multi-field status for display.
func (arena *Arena) GetMultiFieldStatus() MultiFieldStatus {
	settings := arena.EventSettings
	status := MultiFieldStatus{
		Role:         settings.MultiFieldRole.String(),
		FieldId:      settings.FieldId,
		FieldName:    settings.DisplayFieldName(),
		IsNode:       settings.IsNode(),
		IsHub:        settings.IsHub(),
		SimulateMode: arena.SimulateHardware,
	}
	if settings.IsNode() && arena.NodeLink != nil {
		linkStatus := arena.NodeLink.Status()
		status.Connected = linkStatus.Connected
		status.Approved = linkStatus.Approved
		status.Refused = linkStatus.Refused
		status.OutboxSize = linkStatus.OutboxSize
		status.RejectedCount = linkStatus.RejectedCount
		status.LastError = linkStatus.LastError
	}
	return status
}

// notifyDataChanged tells any interested party (e.g. the hub's mirror service) that event data has changed.
func (arena *Arena) notifyDataChanged() {
	if arena.OnDataChanged != nil {
		arena.OnDataChanged()
	}
}

// NotifyDataChanged is the exported form of notifyDataChanged, for use by web handlers that modify event data.
func (arena *Arena) NotifyDataChanged() {
	arena.notifyDataChanged()
}

// matchIsOnThisField returns true if the given match should be played on this instance's field.
func (arena *Arena) matchIsOnThisField(match *model.Match) bool {
	if !arena.EventSettings.IsNode() {
		return true
	}
	return match.FieldId == arena.EventSettings.FieldId
}

// FilterMatchesForField returns only the matches that are played on this instance's field (all matches unless this is
// a node).
func (arena *Arena) FilterMatchesForField(matches []model.Match) []model.Match {
	if !arena.EventSettings.IsNode() {
		return matches
	}
	var filtered []model.Match
	for _, match := range matches {
		if arena.matchIsOnThisField(&match) {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

// checkTeamsAvailable refuses to configure the field for a team that is currently on another field, to avoid two
// access points broadcasting the same team network.
func (arena *Arena) checkTeamsAvailable(match *model.Match) error {
	if !arena.EventSettings.IsNode() || arena.NodeLink == nil || match.Type == model.Test {
		return nil
	}
	return arena.NodeLink.CheckTeamsAvailable(
		[]int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3},
	)
}

// Alliance selection state that is kept separately for each conference.
type allianceSelectionState struct {
	alliances   []model.Alliance
	rankedTeams []model.AllianceSelectionRankedTeam
}

// SwitchAllianceSelectionConference saves the in-progress alliance selection of the current conference and makes the
// given conference the active one.
func (arena *Arena) SwitchAllianceSelectionConference(conferenceId int) {
	if arena.allianceSelectionStates == nil {
		arena.allianceSelectionStates = make(map[int]allianceSelectionState)
	}
	arena.allianceSelectionStates[arena.AllianceSelectionConferenceId] = allianceSelectionState{
		alliances:   arena.AllianceSelectionAlliances,
		rankedTeams: arena.AllianceSelectionRankedTeams,
	}
	state := arena.allianceSelectionStates[conferenceId]
	arena.AllianceSelectionConferenceId = conferenceId
	arena.AllianceSelectionAlliances = state.alliances
	arena.AllianceSelectionRankedTeams = state.rankedTeams
	if arena.AllianceSelectionAlliances == nil {
		arena.AllianceSelectionAlliances = []model.Alliance{}
	}
	if arena.AllianceSelectionRankedTeams == nil {
		arena.AllianceSelectionRankedTeams = []model.AllianceSelectionRankedTeam{}
	}
}

// ResetAllianceSelectionStates discards the in-progress alliance selection of every conference.
func (arena *Arena) ResetAllianceSelectionStates() {
	arena.allianceSelectionStates = make(map[int]allianceSelectionState)
	arena.AllianceSelectionAlliances = []model.Alliance{}
	arena.AllianceSelectionRankedTeams = []model.AllianceSelectionRankedTeam{}
}

// ConferenceAllianceSelectionAlliances returns the in-progress (or saved) alliances of the given conference.
func (arena *Arena) ConferenceAllianceSelectionAlliances(conferenceId int) []model.Alliance {
	if conferenceId == arena.AllianceSelectionConferenceId {
		return arena.AllianceSelectionAlliances
	}
	return arena.allianceSelectionStates[conferenceId].alliances
}

// ConferenceDisplay holds the attributes of a conference that displays need.
type ConferenceDisplay struct {
	Id         int
	Name       string
	ShortName  string
	Color      string
	LogoSuffix string
}

// EventDisplayInfo is attached to match load and score posted messages so that displays can show the field name and
// conference information. It is empty (apart from the field name) in a single-conference event.
type EventDisplayInfo struct {
	FieldName         string
	MultiConference   bool
	Conferences       map[int]ConferenceDisplay
	TeamConferences   map[int]int
	ConferenceRanks   map[int]int
	PreviousConfRanks map[int]int
	RedAllianceLabel  string
	BlueAllianceLabel string
	MatchConferenceId int
}

// BuildEventDisplayInfo assembles the field and conference information for the given match and rankings.
func (arena *Arena) BuildEventDisplayInfo(match *model.Match, rankings game.Rankings) *EventDisplayInfo {
	info := &EventDisplayInfo{
		FieldName:         arena.EventSettings.DisplayFieldName(),
		MultiConference:   arena.EventSettings.MultiConferenceEnabled,
		Conferences:       map[int]ConferenceDisplay{},
		TeamConferences:   map[int]int{},
		ConferenceRanks:   map[int]int{},
		PreviousConfRanks: map[int]int{},
	}
	if match != nil && match.Type == model.Playoff && arena.PlayoffTournament != nil {
		info.RedAllianceLabel = arena.PlayoffTournament.AllianceLabel(match.PlayoffRedAlliance)
		info.BlueAllianceLabel = arena.PlayoffTournament.AllianceLabel(match.PlayoffBlueAlliance)
	}
	if !info.MultiConference {
		return info
	}
	if match != nil {
		info.MatchConferenceId = match.ConferenceId
	}

	conferences, err := arena.Database.GetAllConferences()
	if err != nil {
		return info
	}
	for _, conference := range conferences {
		info.Conferences[conference.Id] = ConferenceDisplay{
			Id:         conference.Id,
			Name:       conference.Name,
			ShortName:  conference.ShortName,
			Color:      conference.Color,
			LogoSuffix: conference.LogoSuffix,
		}
	}
	teams, err := arena.Database.GetAllTeams()
	if err != nil {
		return info
	}
	for _, team := range teams {
		if team.ConferenceId > 0 {
			info.TeamConferences[team.Id] = team.ConferenceId
		}
	}
	if rankings == nil {
		rankings, _ = arena.Database.GetAllRankings()
	}
	for _, ranking := range tournament.ConferenceRankings(rankings, teams, 0) {
		info.ConferenceRanks[ranking.TeamId] = ranking.ConferenceRank
		info.PreviousConfRanks[ranking.TeamId] = ranking.PreviousConferenceRank
	}
	return info
}

func (arena *Arena) generateMultiFieldStatusMessage() any {
	return arena.GetMultiFieldStatus()
}

// usesDynamicClaims returns true if matches of the given type are claimed from the hub one at a time.
func (arena *Arena) usesDynamicClaims(matchType model.MatchType) bool {
	return arena.EventSettings.IsNode() && arena.NodeLink != nil && matchType == model.Qualification &&
		arena.EventSettings.QualFieldAssignmentMode == model.DynamicFieldAssignment
}

// LoadNextAvailableMatch claims the next available qualification match from the hub and loads it (dynamic field
// assignment).
func (arena *Arena) LoadNextAvailableMatch() error {
	if !arena.EventSettings.IsNode() || arena.NodeLink == nil {
		return fmt.Errorf("claiming matches is only available on a field node")
	}
	claimedMatch, err := arena.NodeLink.ClaimNextMatch()
	if err != nil {
		return err
	}
	if claimedMatch == nil {
		return fmt.Errorf("the hub has no match available for this field right now")
	}
	match, err := arena.Database.GetMatchById(claimedMatch.Id)
	if err != nil {
		return err
	}
	if match == nil {
		// The mirror hasn't caught up yet; use the hub's copy.
		return arena.LoadMatch(claimedMatch)
	}
	match.FieldId = claimedMatch.FieldId
	if err = arena.Database.UpdateMatch(match); err != nil {
		return err
	}
	return arena.LoadMatch(match)
}

// releaseClaimedMatch gives the currently loaded match back to the hub if it was claimed but never played.
func (arena *Arena) releaseClaimedMatch(nextMatch *model.Match) {
	previous := arena.CurrentMatch
	if previous == nil || previous.Id == nextMatch.Id || !arena.usesDynamicClaims(previous.Type) ||
		previous.IsComplete() || !previous.StartedAt.IsZero() {
		return
	}
	nodeLink := arena.NodeLink
	matchId := previous.Id
	go func() {
		if err := nodeLink.ReleaseMatch(matchId); err != nil {
			log.Printf("Failed to release match %d back to the hub: %v", matchId, err)
		}
	}()
}

// RebuildPlayoffTournamentReadOnly reconstructs the in-memory playoff tournament from the current settings and
// database without writing to the database (used by nodes after their mirror is updated).
func (arena *Arena) RebuildPlayoffTournamentReadOnly() error {
	if err := arena.CreatePlayoffTournament(); err != nil {
		return err
	}
	return arena.PlayoffTournament.Refresh(arena.Database)
}
