// Copyright 2026 Team 254. All Rights Reserved.

package web

import (
	"encoding/json"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSummarizeFtaTeam(t *testing.T) {
	qual := func(name string, stats model.FtaTeamMatchStats) model.FtaTeamMatchStats {
		stats.MatchType = model.Qualification
		stats.MatchShortName = name
		return stats
	}
	healthy := model.FtaTeamMatchStats{MinBatteryVoltage: 11.2, MaxTripTimeMs: 4}

	testCases := []struct {
		name            string
		team            model.Team
		stats           []model.FtaTeamMatchStats
		expectedRisk    model.FtaSeverity
		expectedReasons []string
	}{
		{
			name:            "never connected",
			team:            model.Team{Id: 254},
			expectedRisk:    model.FtaSeverityBad,
			expectedReasons: []string{"Hasn't connected to the field yet"},
		},
		{
			name:            "healthy",
			team:            model.Team{Id: 254, HasConnected: true},
			stats:           []model.FtaTeamMatchStats{qual("Q1", healthy), qual("Q2", healthy)},
			expectedRisk:    model.FtaSeverityGood,
			expectedReasons: []string{},
		},
		{
			name: "robot down once, with a short blip ignored",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				qual("Q1", model.FtaTeamMatchStats{RobotDownSec: 14.4, MinBatteryVoltage: 11}),
				qual("Q2", model.FtaTeamMatchStats{RobotDownSec: 2, MinBatteryVoltage: 11}),
			},
			expectedRisk:    model.FtaSeverityBad,
			expectedReasons: []string{"Robot down 14s in Q1"},
		},
		{
			name: "robot down repeatedly reports the newest",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				qual("Q1", model.FtaTeamMatchStats{RobotDownSec: 30}),
				qual("Q2", model.FtaTeamMatchStats{RobotDownSec: 8}),
			},
			expectedRisk:    model.FtaSeverityBad,
			expectedReasons: []string{"Robot down in 2 of last 2 matches (8s in Q2)"},
		},
		{
			name: "old problems age out",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				qual("Q1", model.FtaTeamMatchStats{RobotDownSec: 30, BrownoutCount: 2, MinBatteryVoltage: 6}),
				qual("Q2", healthy),
				qual("Q3", healthy),
				qual("Q4", healthy),
			},
			expectedRisk:    model.FtaSeverityGood,
			expectedReasons: []string{},
		},
		{
			name: "single brownout is a warning and hides low battery",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				qual("Q1", model.FtaTeamMatchStats{MinBatteryVoltage: 7.0}),
				qual("Q2", model.FtaTeamMatchStats{BrownoutCount: 1, MinBatteryVoltage: 6.5}),
			},
			expectedRisk:    model.FtaSeverityWarn,
			expectedReasons: []string{"Browned out in Q2"},
		},
		{
			name: "repeated brownouts",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				qual("Q1", model.FtaTeamMatchStats{BrownoutCount: 1}),
				qual("Q2", model.FtaTeamMatchStats{BrownoutCount: 3}),
			},
			expectedRisk:    model.FtaSeverityBad,
			expectedReasons: []string{"Browned out in 2 of last 2 matches"},
		},
		{
			name: "low battery and high trip time",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				qual("Q1", model.FtaTeamMatchStats{MinBatteryVoltage: 7.2, MaxTripTimeMs: 12}),
				qual("Q2", model.FtaTeamMatchStats{MinBatteryVoltage: 9.0, MaxTripTimeMs: 34}),
			},
			expectedRisk:    model.FtaSeverityWarn,
			expectedReasons: []string{"Battery sagged to 7.2V in Q1", "Trip time hit 34ms in Q2"},
		},
		{
			name: "test matches don't count",
			team: model.Team{Id: 254, HasConnected: true},
			stats: []model.FtaTeamMatchStats{
				{MatchType: model.Test, RobotDownSec: 120, BrownoutCount: 4},
			},
			expectedRisk:    model.FtaSeverityGood,
			expectedReasons: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			summary := summarizeFtaTeam(testCase.team, testCase.stats, nil)
			assert.Equal(t, testCase.expectedRisk, summary.Risk)
			var reasons []string
			for _, reason := range summary.Reasons {
				reasons = append(reasons, reason.Message)
			}
			if len(testCase.expectedReasons) == 0 {
				assert.Empty(t, reasons)
			} else {
				assert.Equal(t, testCase.expectedReasons, reasons)
			}
		})
	}
}

func TestSummarizeFtaTeamTotals(t *testing.T) {
	notes := []model.FtaNote{{Id: 2, Text: "Newest"}, {Id: 1, Text: "Oldest"}}
	stats := []model.FtaTeamMatchStats{
		{MatchType: model.Qualification, FaultCount: 2, BrownoutCount: 1, RobotDownSec: 3, MinBatteryVoltage: 8.1},
		{MatchType: model.Qualification, FaultCount: 1, RobotDownSec: 1.5, MinBatteryVoltage: 9.4},
		{MatchType: model.Test, FaultCount: 9},
	}
	summary := summarizeFtaTeam(model.Team{Id: 254, Nickname: "Poofs", HasConnected: true}, stats, notes)
	assert.Equal(t, "Poofs", summary.Nickname)
	assert.Equal(t, 2, summary.MatchesPlayed)
	assert.Equal(t, 3, summary.FaultCount)
	assert.Equal(t, 1, summary.BrownoutCount)
	assert.Equal(t, 4.5, summary.RobotDownSec)
	assert.Equal(t, 8.1, summary.MinBatteryVoltage)
	if assert.NotNil(t, summary.LastNote) {
		assert.Equal(t, "Newest", summary.LastNote.Text)
	}
}

func TestFtaWatchlistApi(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, HasConnected: true}))
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 1114}))
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 148, HasConnected: true}))
	assert.Nil(t, web.arena.Database.CreateFtaTeamMatchStats(&model.FtaTeamMatchStats{
		MatchType: model.Qualification, MatchShortName: "Q1", TeamId: 148, BrownoutCount: 1,
	}))
	for i := 1; i <= 5; i++ {
		match := model.Match{
			Type: model.Qualification, TypeOrder: i, ShortName: "Q" + string(rune('0'+i)), Red1: 254, Blue3: 1114,
		}
		if i == 1 {
			match.Status = game.RedWonMatch
		}
		assert.Nil(t, web.arena.Database.CreateMatch(&match))
	}

	getWatchlist := func() ftaWatchlist {
		recorder := web.getHttpResponse("/api/fta/watchlist")
		assert.Equal(t, 200, recorder.Code)
		var watchlist ftaWatchlist
		assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &watchlist))
		return watchlist
	}

	// From the test match, the next unplayed qualifications are shown.
	watchlist := getWatchlist()
	if assert.Len(t, watchlist.Upcoming, 3) {
		assert.Equal(t, "Q2", watchlist.Upcoming[0].ShortName)
		assert.Equal(t, "Q4", watchlist.Upcoming[2].ShortName)
		assert.Equal(t, ftaUpcomingStation{"R1", 254}, watchlist.Upcoming[0].Stations[0])
		assert.Equal(t, ftaUpcomingStation{"B3", 1114}, watchlist.Upcoming[0].Stations[5])
	}
	// Teams are sorted with the most worrying first.
	if assert.Len(t, watchlist.Teams, 3) {
		assert.Equal(t, 1114, watchlist.Teams[0].TeamId)
		assert.Equal(t, model.FtaSeverityBad, watchlist.Teams[0].Risk)
		assert.Equal(t, 148, watchlist.Teams[1].TeamId)
		assert.Equal(t, model.FtaSeverityWarn, watchlist.Teams[1].Risk)
		assert.Equal(t, 254, watchlist.Teams[2].TeamId)
	}

	// Once a qualification is loaded, only the matches after it are upcoming.
	match, _ := web.arena.Database.GetMatchByTypeOrder(model.Qualification, 3)
	assert.Nil(t, web.arena.LoadMatch(match))
	watchlist = getWatchlist()
	if assert.Len(t, watchlist.Upcoming, 2) {
		assert.Equal(t, "Q4", watchlist.Upcoming[0].ShortName)
		assert.Equal(t, "Q5", watchlist.Upcoming[1].ShortName)
	}
}
