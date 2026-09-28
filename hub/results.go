// Copyright 2026 Team 254. All Rights Reserved.
//
// Applying match results received from field nodes, and handing out matches in dynamic field assignment mode.

package hub

import (
	"errors"
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"net/http"
	"sort"
	"time"
)

// Rough length of a match plus the time needed to clear the field, used to decide when a team was last on a field.
const matchOccupancySec = 180

// SubmissionError is returned when a result submission is rejected outright.
type SubmissionError struct {
	StatusCode int
	Message    string
}

func (err *SubmissionError) Error() string {
	return err.Message
}

// ApplySubmission applies a result from a node exactly once. Duplicate submissions are acknowledged without being
// applied again, results for matches on another field are rejected, and results older than what the hub already has are
// logged as conflicts and ignored (the hub wins).
func (hub *Hub) ApplySubmission(submission *ResultSubmission) (*ResultResponse, error) {
	hub.resultMutex.Lock()
	defer hub.resultMutex.Unlock()
	database := hub.arena.Database

	if submission.PlayNumber <= 0 || submission.MatchId != submission.Match.Id {
		return nil, &SubmissionError{400, "malformed result submission"}
	}

	key := submission.IdempotencyKey()
	receipt, err := database.GetHubResultReceiptByKey(key)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		rankings, _ := database.GetAllRankings()
		return &ResultResponse{Duplicate: true, Message: "result was already applied", Rankings: rankings}, nil
	}

	match, err := database.GetMatchById(submission.MatchId)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, &SubmissionError{404, fmt.Sprintf("match %d does not exist on the hub", submission.MatchId)}
	}
	if match.FieldId != submission.FieldId {
		return nil, &SubmissionError{
			409,
			fmt.Sprintf(
				"match %s is assigned to field %d, not field %d", match.ShortName, match.FieldId, submission.FieldId,
			),
		}
	}

	existingResult, err := database.GetMatchResultForMatch(match.Id)
	if err != nil {
		return nil, err
	}
	if existingResult != nil && existingResult.PlayNumber >= submission.PlayNumber {
		hub.logConflict(
			fmt.Sprintf(
				"field %d sent play %d of %s but the hub already has play %d; keeping the hub's result",
				submission.FieldId, submission.PlayNumber, match.ShortName, existingResult.PlayNumber,
			),
		)
		if err = hub.recordReceipt(submission); err != nil {
			return nil, err
		}
		rankings, _ := database.GetAllRankings()
		return &ResultResponse{Conflict: true, Message: "the hub already has a newer result", Rankings: rankings}, nil
	}

	if match.Type == model.Playoff && (match.PlayoffRedAlliance == 0 || match.PlayoffBlueAlliance == 0) {
		// Possibly a result that overtook the one deciding the alliances, so the node should try again later.
		return nil, &SubmissionError{
			http.StatusTooEarly, fmt.Sprintf("playoff match %s doesn't have both alliances yet", match.ShortName),
		}
	}

	// Take the lineup from the node, since playoff substitutions happen on the field, but never let an empty slot
	// (e.g. from a node whose copy of the match was out of date) overwrite a team the hub knows about.
	submittedLineup := [6]int{
		submission.Match.Red1, submission.Match.Red2, submission.Match.Red3,
		submission.Match.Blue1, submission.Match.Blue2, submission.Match.Blue3,
	}
	lineup := [6]*int{&match.Red1, &match.Red2, &match.Red3, &match.Blue1, &match.Blue2, &match.Blue3}
	for i, teamId := range submittedLineup {
		if teamId == 0 && *lineup[i] != 0 {
			hub.logConflict(
				fmt.Sprintf(
					"field %d sent %s with an empty team slot; keeping team %d", submission.FieldId, match.ShortName,
					*lineup[i],
				),
			)
			continue
		}
		*lineup[i] = teamId
	}
	if !submission.Match.StartedAt.IsZero() {
		match.StartedAt = submission.Match.StartedAt
	}
	if !submission.Match.FieldReadyAt.IsZero() {
		match.FieldReadyAt = submission.Match.FieldReadyAt
	}

	matchResult := submission.MatchResult
	matchResult.Id = 0
	matchResult.MatchId = match.Id
	matchResult.MatchType = match.Type
	matchResult.PlayNumber = submission.PlayNumber
	if matchResult.RedScore == nil {
		matchResult.RedScore = new(game.Score)
	}
	if matchResult.BlueScore == nil {
		matchResult.BlueScore = new(game.Score)
	}
	if matchResult.RedCards == nil {
		matchResult.RedCards = map[string]string{}
	}
	if matchResult.BlueCards == nil {
		matchResult.BlueCards = map[string]string{}
	}

	rankings, err := hub.arena.ApplyCommittedResultAt(
		match, &matchResult, submission.IsEdit, submission.Match.ScoreCommittedAt,
	)
	if err != nil {
		return nil, err
	}
	if err = hub.recordReceipt(submission); err != nil {
		return nil, err
	}

	response := &ResultResponse{Applied: true, Rankings: rankings}
	if rankings == nil {
		response.Rankings, _ = database.GetAllRankings()
	}
	if match.Type == model.Playoff {
		response.PlayoffMatches, _ = database.GetMatchesByType(model.Playoff, true)
	}

	if !submission.IsEdit {
		// Show the latest result on the hub's own displays.
		hub.arena.SavedMatch = match
		hub.arena.SavedMatchResult = &matchResult
		hub.arena.SavedRankings = rankings
		hub.arena.SavedRankingsPending = false
		hub.arena.ScorePostedNotifier.Notify()
	}
	hub.Poke()
	return response, nil
}

func (hub *Hub) recordReceipt(submission *ResultSubmission) error {
	return hub.arena.Database.CreateHubResultReceipt(
		&model.HubResultReceipt{
			Key:        submission.IdempotencyKey(),
			FieldId:    submission.FieldId,
			MatchId:    submission.MatchId,
			PlayNumber: submission.PlayNumber,
			ReceivedAt: time.Now(),
		},
	)
}

// ImportResult summarizes what happened to the results in an imported bundle.
type ImportResult struct {
	// One line per result, in the order in which they appear in the bundle.
	Lines          []string
	Applied        int
	AlreadyApplied int
	NotApplied     int
}

// ImportBundle applies every submission in a results bundle, returning a summary of what happened to each. A playoff
// result that comes before the one that decides its alliances is tried again after the rest of the bundle.
func (hub *Hub) ImportBundle(bundle *ResultsBundle) (*ImportResult, error) {
	if bundle.FieldId < 1 || bundle.FieldId > model.MaxFieldId {
		return nil, fmt.Errorf(
			"this isn't a results bundle from a field laptop (field %d); export one from the field's Settings → "+
				"Multi-Field tab", bundle.FieldId,
		)
	}
	result := &ImportResult{Lines: make([]string, len(bundle.Submissions))}
	applied := make([]bool, len(bundle.Submissions))
	alreadyApplied := make([]bool, len(bundle.Submissions))
	pending := make([]int, len(bundle.Submissions))
	for i := range bundle.Submissions {
		pending[i] = i
	}
	for len(pending) > 0 {
		var tooEarly []int
		for _, i := range pending {
			submission := &bundle.Submissions[i]
			if submission.FieldId == 0 {
				submission.FieldId = bundle.FieldId
			}
			response, err := hub.ApplySubmission(submission)
			var submissionError *SubmissionError
			if errors.As(err, &submissionError) && submissionError.StatusCode == http.StatusTooEarly {
				tooEarly = append(tooEarly, i)
			}
			name := submission.Match.ShortName
			switch {
			case err != nil:
				result.Lines[i] = fmt.Sprintf("%s (play %d): not applied: %v", name, submission.PlayNumber, err)
			case response.Duplicate:
				result.Lines[i] = fmt.Sprintf("%s (play %d): already applied", name, submission.PlayNumber)
				alreadyApplied[i] = true
			case response.Conflict:
				result.Lines[i] = fmt.Sprintf("%s (play %d): hub has a newer result", name, submission.PlayNumber)
				alreadyApplied[i] = true
			default:
				result.Lines[i] = fmt.Sprintf("%s (play %d): applied", name, submission.PlayNumber)
				applied[i] = true
			}
		}
		if len(tooEarly) == len(pending) {
			// No progress; the remaining results can't be applied yet.
			break
		}
		pending = tooEarly
	}
	for i := range bundle.Submissions {
		switch {
		case applied[i]:
			result.Applied++
		case alreadyApplied[i]:
			result.AlreadyApplied++
		default:
			result.NotApplied++
		}
	}
	return result, nil
}

// ClaimNextMatch assigns the next suitable unplayed qualification match to the given field (dynamic mode). A match is
// suitable if none of its teams is loaded on or assigned to another field and every team has rested for at least the
// minimum turnaround time. If the field already holds an unplayed claim, that match is returned again.
func (hub *Hub) ClaimNextMatch(fieldId int) (*model.Match, string, error) {
	hub.resultMutex.Lock()
	defer hub.resultMutex.Unlock()
	return hub.claimNextMatch(fieldId, time.Now())
}

func (hub *Hub) claimNextMatch(fieldId int, now time.Time) (*model.Match, string, error) {
	database := hub.arena.Database
	matches, err := database.GetMatchesByType(model.Qualification, false)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(
		matches,
		func(i, j int) bool {
			return matches[i].TypeOrder < matches[j].TypeOrder
		},
	)

	// Return an existing unplayed claim held by this field.
	for i, match := range matches {
		if match.FieldId == fieldId && !match.IsComplete() {
			return &matches[i], "", nil
		}
	}

	// Determine which teams are busy on other fields, and when each team was last on a field.
	busyTeams := make(map[int]int)
	lastOnField := make(map[int]time.Time)
	for _, fieldTeams := range hub.FieldTeams() {
		if fieldTeams.FieldId == fieldId {
			continue
		}
		for _, teamId := range fieldTeams.TeamIds {
			busyTeams[teamId] = fieldTeams.FieldId
		}
	}
	for _, match := range matches {
		teamIds := matchTeamIds(&match)
		if match.FieldId != 0 && match.FieldId != fieldId && !match.IsComplete() {
			for _, teamId := range teamIds {
				busyTeams[teamId] = match.FieldId
			}
		}
		if match.IsComplete() || !match.StartedAt.IsZero() {
			// Use when the score was committed if known; otherwise estimate from when the match started.
			end := match.ScoreCommittedAt
			if end.IsZero() {
				start := match.StartedAt
				if start.IsZero() {
					start = match.Time
				}
				end = start.Add(matchOccupancySec * time.Second)
			}
			for _, teamId := range teamIds {
				if end.After(lastOnField[teamId]) {
					lastOnField[teamId] = end
				}
			}
		}
	}

	minTurnaround := time.Duration(hub.arena.EventSettings.MinTurnaroundSec) * time.Second
	reason := "no unplayed, unclaimed qualification matches remain"
	for i, match := range matches {
		if match.IsComplete() || match.FieldId != 0 {
			continue
		}
		blocked := ""
		for _, teamId := range matchTeamIds(&match) {
			if otherField, ok := busyTeams[teamId]; ok {
				blocked = fmt.Sprintf("team %d is on field %d", teamId, otherField)
				break
			}
			if last, ok := lastOnField[teamId]; ok && now.Before(last) {
				blocked = fmt.Sprintf("team %d is still finishing a match", teamId)
				break
			} else if ok && now.Sub(last) < minTurnaround {
				blocked = fmt.Sprintf("team %d needs more rest", teamId)
				break
			}
		}
		if blocked != "" {
			if reason == "no unplayed, unclaimed qualification matches remain" {
				reason = fmt.Sprintf("next match %s is blocked: %s", match.ShortName, blocked)
			}
			continue
		}

		claimed := matches[i]
		claimed.FieldId = fieldId
		if err = database.UpdateMatch(&claimed); err != nil {
			return nil, "", err
		}
		hub.Poke()
		return &claimed, "", nil
	}
	return nil, reason, nil
}

// ReleaseMatch gives a claimed but unplayed match back to the pool.
func (hub *Hub) ReleaseMatch(fieldId, matchId int) error {
	hub.resultMutex.Lock()
	defer hub.resultMutex.Unlock()
	match, err := hub.arena.Database.GetMatchById(matchId)
	if err != nil {
		return err
	}
	if match == nil {
		return fmt.Errorf("match %d does not exist", matchId)
	}
	if match.FieldId != fieldId {
		return fmt.Errorf("match %s is not claimed by field %d", match.ShortName, fieldId)
	}
	if match.IsComplete() || match.Type != model.Qualification ||
		hub.arena.EventSettings.QualFieldAssignmentMode != model.DynamicFieldAssignment {
		return nil
	}
	match.FieldId = 0
	if err = hub.arena.Database.UpdateMatch(match); err != nil {
		return err
	}
	hub.Poke()
	return nil
}

// AssignMatchField sets the field of an unplayed match (e.g. a championship match whose field is chosen at load time).
func (hub *Hub) AssignMatchField(matchId, fieldId int) error {
	hub.resultMutex.Lock()
	defer hub.resultMutex.Unlock()
	match, err := hub.arena.Database.GetMatchById(matchId)
	if err != nil {
		return err
	}
	if match == nil {
		return fmt.Errorf("match %d does not exist", matchId)
	}
	if fieldId < 0 || fieldId > 2 {
		return fmt.Errorf("invalid field %d", fieldId)
	}
	if match.IsComplete() {
		return fmt.Errorf("match %s has already been played", match.ShortName)
	}
	for _, status := range hub.NodeStatuses() {
		if status.Connected && status.FieldId != fieldId && status.Report.CurrentMatchId == match.Id &&
			status.Report.MatchInProgress {
			return fmt.Errorf("match %s is being played on field %d right now", match.ShortName, status.FieldId)
		}
	}
	match.FieldId = fieldId
	if err = hub.arena.Database.UpdateMatch(match); err != nil {
		return err
	}
	hub.Poke()
	return nil
}

func matchTeamIds(match *model.Match) []int {
	var teamIds []int
	for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
		if teamId > 0 {
			teamIds = append(teamIds, teamId)
		}
	}
	return teamIds
}
