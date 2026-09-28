// Copyright 2026 Team 254. All Rights Reserved.

package tournament

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func multiConferenceTestTeams(numTeams int) []model.Team {
	teams := make([]model.Team, numTeams)
	for i := range teams {
		teams[i].Id = 101 + i
		// Put the first half of the teams in one conference, as would happen when importing them by region.
		if i < numTeams/2 {
			teams[i].ConferenceId = 1
		} else {
			teams[i].ConferenceId = 2
		}
	}
	return teams
}

func TestScheduleSearchIsReproducibleFromSeed(t *testing.T) {
	setupTestDb(t)
	teams := multiConferenceTestTeams(36)
	blocks := []model.ScheduleBlock{{0, model.Qualification, time.Unix(0, 0).UTC(), 60, 360, 0}}
	options := ScheduleOptions{Seed: 12345, PreferMixedConferences: true, SearchIterations: 300, MultiField: true}

	first, firstReport, err := BuildScheduleWithOptions(teams, blocks, model.Qualification, options)
	assert.Nil(t, err)
	second, secondReport, err := BuildScheduleWithOptions(teams, blocks, model.Qualification, options)
	assert.Nil(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, int64(12345), firstReport.Seed)
	assert.Equal(t, firstReport.MixedAlliancePercent, secondReport.MixedAlliancePercent)

	options.Seed = 999
	third, _, err := BuildScheduleWithOptions(teams, blocks, model.Qualification, options)
	assert.Nil(t, err)
	assert.NotEqual(t, first, third)
}

func TestScheduleSearchImprovesConferenceMixing(t *testing.T) {
	setupTestDb(t)
	teams := multiConferenceTestTeams(36)
	blocks := []model.ScheduleBlock{{0, model.Qualification, time.Unix(0, 0).UTC(), 60, 360, 0}}

	totalRandom := 0
	totalSearched := 0
	for seed := int64(1); seed <= 5; seed++ {
		_, randomReport, err := BuildScheduleWithOptions(
			teams, blocks, model.Qualification, ScheduleOptions{Seed: seed},
		)
		assert.Nil(t, err)
		_, searchedReport, err := BuildScheduleWithOptions(
			teams,
			blocks,
			model.Qualification,
			ScheduleOptions{Seed: seed, PreferMixedConferences: true, SearchIterations: 500},
		)
		assert.Nil(t, err)
		assert.GreaterOrEqual(t, searchedReport.MixedAlliancePercent, randomReport.MixedAlliancePercent)
		totalRandom += randomReport.SameConferenceAlliance
		totalSearched += searchedReport.SameConferenceAlliance
	}
	assert.Less(t, totalSearched, totalRandom)
}

func TestScheduleMultiFieldHasNoBackToBackMatches(t *testing.T) {
	setupTestDb(t)
	for _, numTeams := range []int{24, 36, 60} {
		teams := multiConferenceTestTeams(numTeams)
		numMatches := numTeams * 10 / 6
		blocks := []model.ScheduleBlock{{0, model.Qualification, time.Unix(0, 0).UTC(), numMatches, 210, 0}}
		matches, report, err := BuildScheduleWithOptions(
			teams, blocks, model.Qualification, ScheduleOptions{Seed: 7, MultiField: true},
		)
		if !assert.Nil(t, err, "%d teams", numTeams) {
			continue
		}
		assert.Equal(t, 0, report.BackToBackCount, "%d teams", numTeams)

		// Reordering must not change who plays with whom: every team still plays the same number of matches.
		appearances := make(map[int]int)
		for i, match := range matches {
			assert.Equal(t, i+1, match.TypeOrder)
			for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
				appearances[teamId]++
			}
			if i > 0 {
				previous := matches[i-1]
				for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
					for _, previousTeamId := range []int{
						previous.Red1, previous.Red2, previous.Red3, previous.Blue1, previous.Blue2, previous.Blue3,
					} {
						assert.NotEqual(t, teamId, previousTeamId, "team %d plays back to back", teamId)
					}
				}
			}
		}
		for _, team := range teams {
			assert.Equal(t, 10, appearances[team.Id])
		}
	}
}

func TestAssignFields(t *testing.T) {
	blocks := []model.ScheduleBlock{
		{0, model.Qualification, time.Unix(0, 0).UTC(), 4, 210, 0},
		{0, model.Qualification, time.Unix(10000, 0).UTC(), 3, 420, 2},
	}
	matches := make([]model.Match, 7)
	for i := range matches {
		matches[i].TypeOrder = i + 1
	}

	AssignFields(matches, blocks, model.AlternateFieldAssignment)
	assert.Equal(t, []int{1, 2, 1, 2, 1, 2, 1}, fieldIds(matches))

	AssignFields(matches, blocks, model.BlocksFieldAssignment)
	assert.Equal(t, []int{1, 2, 1, 2, 2, 2, 2}, fieldIds(matches))

	AssignFields(matches, blocks, model.DynamicFieldAssignment)
	assert.Equal(t, []int{0, 0, 0, 0, 0, 0, 0}, fieldIds(matches))
}

func TestScheduleReport(t *testing.T) {
	matches := []model.Match{
		{TypeOrder: 1, Time: time.Unix(0, 0), Red1: 1, Red2: 2, Red3: 3, Blue1: 4, Blue2: 5, Blue3: 6, FieldId: 1},
		{TypeOrder: 2, Time: time.Unix(300, 0), Red1: 1, Red2: 4, Red3: 5, Blue1: 2, Blue2: 3, Blue3: 6, FieldId: 2},
	}
	conferences := map[int]int{1: 1, 2: 1, 3: 1, 4: 2, 5: 2, 6: 2}
	report := BuildScheduleReport(matches, conferences, nil)
	assert.Equal(t, 50.0, report.MixedAlliancePercent)
	assert.Equal(t, 2, report.SameConferenceAlliance)
	assert.Equal(t, 300, report.MinTurnaroundSec)
	assert.Equal(t, 6, report.BackToBackCount)
	assert.Equal(t, [2]int{1, 1}, report.TeamFieldCounts[1])
	assert.Equal(t, 2, report.CrossPartnerHistogram[2])
	assert.Equal(t, 4, report.CrossPartnerHistogram[1])
}

func fieldIds(matches []model.Match) []int {
	var ids []int
	for _, match := range matches {
		ids = append(ids, match.FieldId)
	}
	return ids
}
