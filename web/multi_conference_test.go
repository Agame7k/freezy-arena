// Copyright 2026 Team 254. All Rights Reserved.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

// Sets up two small single-elimination conferences and twelve ranked teams split between them.
func setupMultiConferenceWeb(t *testing.T) *Web {
	web := setupTestWeb(t)
	conferences, err := web.arena.Database.EnsureConferences()
	assert.Nil(t, err)
	for _, conference := range conferences {
		conference.PlayoffType = model.SingleEliminationPlayoff
		conference.NumAlliances = 2
		assert.Nil(t, web.arena.Database.UpdateConference(&conference))
	}
	for i := 1; i <= 12; i++ {
		conferenceId := 1
		if i%2 == 0 {
			conferenceId = 2
		}
		assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 100 + i, ConferenceId: conferenceId}))
		assert.Nil(t, web.arena.Database.CreateRanking(&game.Ranking{TeamId: 100 + i, Rank: i}))
	}
	web.arena.EventSettings.MultiConferenceEnabled = true
	assert.Nil(t, web.arena.Database.UpdateEventSettings(web.arena.EventSettings))
	assert.Nil(t, web.arena.LoadSettings())
	return web
}

func TestSettingsMultiConference(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254}))

	form := "name=Chezy&multiConferenceForm=1&multiConferenceEnabled=on&championshipFormat=1" +
		"&championshipSeriesLength=5&conf1Name=East&conf1ShortName=E&conf1Color=%23ff0000&conf1PlayoffType=1" +
		"&conf1NumAlliances=2&conf1SeriesF=1&conf1FieldId=1&conf2Name=West&conf2ShortName=W&conf2PlayoffType=1" +
		"&conf2NumAlliances=2&conf2FieldId=2"

	// A team without a conference blocks turning the mode on.
	recorder := web.postHttpResponse("/setup/settings", form)
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "teams without a conference: 254")
	assert.False(t, web.arena.EventSettings.MultiConferenceEnabled)

	// With no teams yet, it can be turned on during setup.
	assert.Nil(t, web.arena.Database.DeleteTeam(254))
	recorder = web.postHttpResponse("/setup/settings", form)
	assert.Equal(t, 303, recorder.Code, recorder.Body.String())
	assert.True(t, web.arena.EventSettings.MultiConferenceEnabled)
	assert.Equal(t, model.CrossoverSemisChampionship, web.arena.EventSettings.ChampionshipFormat)
	assert.Equal(t, 5, web.arena.EventSettings.ChampionshipSeriesLength)
	conference, _ := web.arena.Database.GetConferenceById(1)
	assert.Equal(t, "East", conference.Name)
	assert.Equal(t, "#ff0000", conference.Color)
	assert.Equal(t, model.SingleEliminationPlayoff, conference.PlayoffType)
	assert.Equal(t, 1, conference.SeriesLength("F"))
	assert.True(t, web.arena.PlayoffTournament.IsMultiConference())

	// Invalid combinations are rejected.
	recorder = web.postHttpResponse("/setup/settings", strings.Replace(form, "conf1PlayoffType=1", "conf1PlayoffType=0", 1))
	assert.Contains(t, recorder.Body.String(), "double elimination requires 4 to 8 alliances")

	// Once alliances exist, the mode and the conference formats are locked.
	assert.Nil(t, web.arena.Database.CreateAlliance(&model.Alliance{Id: 1}))
	recorder = web.postHttpResponse("/setup/settings", strings.Replace(form, "&multiConferenceEnabled=on", "", 1))
	assert.Contains(t, recorder.Body.String(), "Cannot turn multi-conference mode on or off")
	recorder = web.postHttpResponse("/setup/settings", strings.Replace(form, "conf1NumAlliances=2", "conf1NumAlliances=3", 1))
	assert.Contains(t, recorder.Body.String(), "Cannot change conference playoff formats")
	// ...and the message says how to unlock them.
	assert.Contains(t, recorder.Body.String(), "Clear Playoff/Alliance Data")
}

func TestConferenceSizeIsCheckedWhenItChanges(t *testing.T) {
	// Two single-elimination conferences of six teams, each with two alliances of three.
	web := setupMultiConferenceWeb(t)
	form := "name=Chezy&multiConferenceForm=1&multiConferenceEnabled=on&conf1Name=North&conf1ShortName=N" +
		"&conf1PlayoffType=1&conf1NumAlliances=3&conf2Name=South&conf2ShortName=S&conf2PlayoffType=1" +
		"&conf2NumAlliances=2"

	// Growing a conference beyond its teams is caught now rather than at alliance selection, with the fix.
	recorder := web.postHttpResponse("/setup/settings", form)
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(
		t, recorder.Body.String(), "North has 6 teams but 3 alliances of 3 need 9; lower its number of alliances to 2",
	)
	conference, _ := web.arena.Database.GetConferenceById(1)
	assert.Equal(t, 2, conference.NumAlliances)
	// The form is shown again with what was entered, so that only the mistake needs correcting.
	assert.Regexp(t, `name="conf1NumAlliances"[^>]*value="3"`, recorder.Body.String())

	// Shrinking a conference that is still too small afterwards is caught too.
	conference.NumAlliances = 4
	assert.Nil(t, web.arena.Database.UpdateConference(conference))
	recorder = web.postHttpResponse("/setup/settings", form)
	assert.Contains(t, recorder.Body.String(), "North has 6 teams but 3 alliances of 3 need 9")

	// A team that is added later without a conference doesn't stop unrelated settings from being saved.
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 999}))
	recorder = web.postHttpResponse("/setup/settings", strings.Replace(form, "conf1NumAlliances=3", "conf1NumAlliances=2", 1))
	assert.Equal(t, 303, recorder.Code, recorder.Body.String())
}

func TestSettingsErrorKeepsWhatWasEnteredAndChangesNothing(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.SimulateHardware = true

	// A hub doesn't need a secret typed in; it makes up one that is easy to copy onto the field laptops.
	recorder := web.postHttpResponse("/setup/settings", "name=Chezy&multiFieldForm=1&multiFieldRole=1")
	assert.Equal(t, 303, recorder.Code, recorder.Body.String())
	if assert.NotNil(t, web.hub) {
		t.Cleanup(web.hub.Stop)
	}
	assert.True(t, web.arena.EventSettings.IsHub())
	assert.Regexp(t, `^[a-z2-9]{4}-[a-z2-9]{4}$`, web.arena.EventSettings.HubSharedSecret)
	secret := web.arena.EventSettings.HubSharedSecret

	// A mistake leaves the running settings alone, and shows the form again with what was entered.
	recorder = web.postHttpResponse(
		"/setup/settings", "name=Renamed&adminPassword=newpassword&multiFieldForm=1&multiFieldRole=2&fieldId=2",
	)
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "must have the hub address set")
	assert.Contains(t, body, `value="Renamed"`)
	assert.Contains(t, body, `<option value="2" selected>Node`)
	settings := web.arena.EventSettings
	assert.Equal(t, "Chezy", settings.Name)
	assert.Equal(t, "", settings.AdminPassword)
	assert.True(t, settings.IsHub())
	assert.Equal(t, secret, settings.HubSharedSecret)
	// The page header still shows what this machine is actually running as.
	assert.Contains(t, body, "Multi-field hub")
	assert.NotContains(t, body, "field node: teams")

	// A node needs the hub's secret.
	recorder = web.postHttpResponse(
		"/setup/settings", "name=Chezy&multiFieldForm=1&multiFieldRole=2&fieldId=2&hubAddress=10.0.0.5",
	)
	assert.Contains(t, recorder.Body.String(), "needs the hub's shared secret")
	assert.True(t, web.arena.EventSettings.IsHub())
}

func TestSettingsMultiField(t *testing.T) {
	web := setupTestWeb(t)
	recorder := web.postHttpResponse(
		"/setup/settings", "name=Chezy&multiFieldForm=1&multiFieldRole=2&fieldId=2&fieldName=Field+Two",
	)
	assert.Contains(t, recorder.Body.String(), "must have the hub address set")

	recorder = web.postHttpResponse(
		"/setup/settings",
		"name=Chezy&multiFieldForm=1&multiFieldRole=2&fieldId=2&fieldName=Field+Two&hubAddress=http://hub:8080"+
			"&hubSharedSecret=s3cret&qualFieldAssignmentMode=2&minTurnaroundSec=420",
	)
	assert.Equal(t, 303, recorder.Code, recorder.Body.String())
	settings := web.arena.EventSettings
	assert.Equal(t, model.NodeRole, settings.MultiFieldRole)
	assert.Equal(t, 2, settings.FieldId)
	assert.Equal(t, "Field Two", settings.FieldName)
	assert.Equal(t, model.DynamicFieldAssignment, settings.QualFieldAssignmentMode)
	assert.Equal(t, 420, settings.MinTurnaroundSec)

	// Posting another tab leaves the multi-field settings alone.
	recorder = web.postHttpResponse("/setup/settings", "name=Other&activeSettingsTab=event")
	assert.Equal(t, 303, recorder.Code)
	assert.Equal(t, model.NodeRole, web.arena.EventSettings.MultiFieldRole)
	assert.Equal(t, "http://hub:8080", web.arena.EventSettings.HubAddress)

	recorder = web.getHttpResponse("/setup/settings")
	assert.Contains(t, recorder.Body.String(), "Promote node to standalone")
}

func TestTeamsConferences(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.MultiConferenceEnabled = true
	web.arena.EventSettings.TbaDownloadEnabled = false
	web.arena.Database.EnsureConferences()

	recorder := web.postHttpResponse("/setup/teams", "teamNumbers=254,N%0D%0A1114,south%0D%0A2056%0D%0A604,2")
	assert.Equal(t, 303, recorder.Code)
	teams, _ := web.arena.Database.GetAllTeams()
	conferences := map[int]int{}
	for _, team := range teams {
		conferences[team.Id] = team.ConferenceId
	}
	assert.Equal(t, map[int]int{254: 1, 604: 2, 1114: 2, 2056: 0}, conferences)

	// Bulk assignment of the selected teams.
	recorder = web.postHttpResponse(
		"/setup/teams/conferences", "action=bulk&bulkConferenceId=1&selectedTeams=2056&selectedTeams=604",
	)
	assert.Equal(t, 303, recorder.Code)
	team, _ := web.arena.Database.GetTeamById(604)
	assert.Equal(t, 1, team.ConferenceId)
	team, _ = web.arena.Database.GetTeamById(2056)
	assert.Equal(t, 1, team.ConferenceId)

	// Per-team dropdowns.
	recorder = web.postHttpResponse("/setup/teams/conferences", "action=save&conference_254=2&conference_1114=0")
	assert.Equal(t, 303, recorder.Code)
	team, _ = web.arena.Database.GetTeamById(254)
	assert.Equal(t, 2, team.ConferenceId)
	team, _ = web.arena.Database.GetTeamById(1114)
	assert.Equal(t, 0, team.ConferenceId)

	// Dropdown changes are kept when the bulk button is pressed.
	recorder = web.postHttpResponse(
		"/setup/teams/conferences", "action=bulk&bulkConferenceId=2&selectedTeams=2056&conference_1114=1",
	)
	assert.Equal(t, 303, recorder.Code)
	team, _ = web.arena.Database.GetTeamById(2056)
	assert.Equal(t, 2, team.ConferenceId)
	team, _ = web.arena.Database.GetTeamById(1114)
	assert.Equal(t, 1, team.ConferenceId)
	recorder = web.postHttpResponse("/setup/teams/conferences", "action=save&conference_1114=0&conference_2056=1")
	assert.Equal(t, 303, recorder.Code)

	// A conference that isn't recognized on import is pointed out rather than silently left blank.
	recorder = web.postHttpResponse("/setup/teams", "teamNumbers=9999,Nroth%0D%0A9998,-")
	assert.Equal(t, 303, recorder.Code)
	recorder = web.getHttpResponse(recorder.Header().Get("Location"))
	assert.Contains(t, recorder.Body.String(), "recognize the conference for 9999")
	assert.NotContains(t, recorder.Body.String(), "9998 (")
	assert.Nil(t, web.arena.Database.DeleteTeam(9999))
	assert.Nil(t, web.arena.Database.DeleteTeam(9998))

	recorder = web.getHttpResponse("/setup/teams")
	assert.Contains(t, recorder.Body.String(), "North: 2")
	assert.Contains(t, recorder.Body.String(), "Conference assignment is incomplete")

	// Conferences are locked once alliances exist.
	assert.Nil(t, web.arena.Database.CreateAlliance(&model.Alliance{Id: 1}))
	recorder = web.postHttpResponse("/setup/teams/conferences", "action=save&conference_254=1")
	team, _ = web.arena.Database.GetTeamById(254)
	assert.Equal(t, 2, team.ConferenceId)
}

func TestConferenceAllianceSelectionPerConference(t *testing.T) {
	web := setupMultiConferenceWeb(t)

	// Conference 1 (odd-numbered teams) selects first.
	recorder := web.getHttpResponse("/alliance_selection?conference=1")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "North Conference")
	recorder = web.postHttpResponse("/alliance_selection/start", "")
	assert.Equal(t, 303, recorder.Code)
	if assert.Equal(t, 2, len(web.arena.AllianceSelectionAlliances)) {
		assert.Equal(t, 1, web.arena.AllianceSelectionAlliances[0].Id)
		assert.Equal(t, 1, web.arena.AllianceSelectionAlliances[0].Seed)
		assert.Equal(t, 1, web.arena.AllianceSelectionAlliances[0].ConferenceId)
	}
	if assert.Equal(t, 6, len(web.arena.AllianceSelectionRankedTeams)) {
		assert.Equal(t, 101, web.arena.AllianceSelectionRankedTeams[0].TeamId)
		assert.Equal(t, 1, web.arena.AllianceSelectionRankedTeams[0].Rank)
		assert.Equal(t, 103, web.arena.AllianceSelectionRankedTeams[1].TeamId)
		assert.Equal(t, 2, web.arena.AllianceSelectionRankedTeams[1].Rank)
	}

	// A team from the other conference can't be picked.
	recorder = web.postHttpResponse("/alliance_selection", "selection0_0=102")
	assert.Contains(t, recorder.Body.String(), "not in the conference that is currently selecting")

	recorder = web.postHttpResponse(
		"/alliance_selection",
		"selection0_0=101&selection0_1=103&selection0_2=105&selection1_0=107&selection1_1=109&selection1_2=111",
	)
	assert.Equal(t, 303, recorder.Code)
	recorder = web.postHttpResponse("/alliance_selection/finalize", "startTime=2026-01-01 01:00:00 PM")
	assert.Equal(t, 303, recorder.Code)
	assert.Equal(t, "/alliance_selection?conference=2", recorder.Header().Get("Location"))

	// The whole tournament exists, with the second conference's matches as placeholders.
	alliances, _ := web.arena.Database.GetAllAlliances()
	assert.Equal(t, 2, len(alliances))
	matches, _ := web.arena.Database.GetMatchesByType(model.Playoff, true)
	assert.NotEmpty(t, matches)
	secondConferenceFilled := false
	for _, match := range matches {
		if match.ConferenceId == 2 && match.Red1 > 0 {
			secondConferenceFilled = true
		}
	}
	assert.False(t, secondConferenceFilled)

	// Conference 2 then selects and finalizes, filling in its matches.
	assert.Equal(t, 2, web.arena.AllianceSelectionConferenceId)
	recorder = web.postHttpResponse("/alliance_selection/start", "")
	assert.Equal(t, 303, recorder.Code)
	assert.Equal(t, 3, web.arena.AllianceSelectionAlliances[0].Id)
	recorder = web.postHttpResponse(
		"/alliance_selection",
		"selection0_0=102&selection0_1=104&selection0_2=106&selection1_0=108&selection1_1=110&selection1_2=112",
	)
	assert.Equal(t, 303, recorder.Code)
	recorder = web.postHttpResponse("/alliance_selection/finalize", "startTime=2026-01-01 02:00:00 PM")
	assert.Equal(t, 303, recorder.Code)
	alliances, _ = web.arena.Database.GetAllAlliances()
	assert.Equal(t, 4, len(alliances))
	assert.Equal(t, 2, alliances[3].Seed)
	assert.Equal(t, 2, alliances[3].ConferenceId)
	matches, _ = web.arena.Database.GetMatchesByType(model.Playoff, false)
	for _, match := range matches {
		if match.ConferenceId == 2 {
			assert.Equal(t, 104, match.Red1, match.ShortName)
			assert.Equal(t, 110, match.Blue1, match.ShortName)
			// On a single field both conferences share one playoff timeline, starting at the first start time.
			assert.Equal(t, 13, match.Time.Local().Hour(), match.ShortName)
			break
		}
	}

	// Finalizing again is refused.
	recorder = web.postHttpResponse("/alliance_selection/finalize", "startTime=2026-01-01 02:00:00 PM")
	assert.Contains(t, recorder.Body.String(), "already been finalized")
}

func TestMatchPlayListFilteredByField(t *testing.T) {
	web := setupTestWeb(t)
	for i, fieldId := range []int{1, 2, 1} {
		match := model.Match{
			Type: model.Qualification, TypeOrder: i + 1, ShortName: fmt.Sprintf("Q%d", i+1), FieldId: fieldId,
		}
		assert.Nil(t, web.arena.Database.CreateMatch(&match))
	}
	web.arena.EventSettings.MultiFieldRole = model.NodeRole
	web.arena.EventSettings.FieldId = 2
	web.arena.EventSettings.FieldName = "Field 2"

	recorder := web.getHttpResponse("/match_play/match_load")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "Q2")
	assert.NotContains(t, body, "Q1")
	assert.NotContains(t, body, "Q3")
	assert.Contains(t, body, "Showing only matches on Field 2")
}

func TestEventControlPage(t *testing.T) {
	web := setupTestWeb(t)
	recorder := web.getHttpResponse("/event_control")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "this machine isn't the hub")
	assert.Contains(t, recorder.Body.String(), "no restart is needed")
	recorder = web.getHttpResponse("/api/hub/versions")
	assert.Equal(t, 404, recorder.Code)

	web.arena.EventSettings.MultiFieldRole = model.HubRole
	web.arena.EventSettings.HubSharedSecret = "secret"
	eventHub := hub.New(web.arena)
	t.Cleanup(eventHub.Stop)
	web.SetHub(eventHub)
	match := model.Match{Type: model.Qualification, TypeOrder: 1, ShortName: "Q1", LongName: "Qualification 1",
		FieldId: 1}
	assert.Nil(t, web.arena.Database.CreateMatch(&match))
	recorder = web.getHttpResponse("/event_control")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "Import results bundle")
	// The secret is on the page (masked until shown) so that it can be copied onto the field laptops.
	assert.Contains(t, body, `data-secret="secret"`)
	assert.NotContains(t, body, "&lt;your secret&gt;")
	// Any unplayed match can be moved, not only ones without a field.
	assert.Contains(t, body, `<optgroup label="On Field 1">`)
	recorder = web.getHttpResponse("/api/hub/versions")
	assert.Equal(t, 401, recorder.Code)
	recorder = web.getHttpResponse("/displays/dual_field?displayId=100&background=%23000")
	assert.Equal(t, 200, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "only available on the multi-field hub")

	// Moving a match says what happened.
	recorder = web.postHttpResponse("/event_control/assign_field", fmt.Sprintf("matchId=%d&fieldId=2", match.Id))
	assert.Equal(t, 303, recorder.Code)
	recorder = web.getHttpResponse(recorder.Header().Get("Location"))
	assert.Contains(t, recorder.Body.String(), "Qualification 1 is now on Field 2.")
	movedMatch, _ := web.arena.Database.GetMatchById(match.Id)
	assert.Equal(t, 2, movedMatch.FieldId)

	// A file that isn't a results bundle is refused with an explanation.
	for _, test := range []struct {
		contents string
		expected string
	}{
		{"not json", "isn't a results bundle"},
		{`{"FieldId": 1}`, "has no results in it"},
		{`{"FieldId": 7, "Submissions": [{"MatchId": 1, "PlayNumber": 1}]}`, "isn't a results bundle from a field"},
	} {
		recorder = web.postMultipartFile("/event_control/import_bundle", "bundleFile", "bundle.json", test.contents)
		assert.Contains(t, recorder.Body.String(), test.expected, test.contents)
	}
}

func TestMultiConferenceReportsAndBracket(t *testing.T) {
	web := setupMultiConferenceWeb(t)
	web.arena.EventSettings.MultiFieldRole = model.HubRole
	assert.Nil(t, web.arena.Database.CreateMatch(
		&model.Match{Type: model.Qualification, TypeOrder: 1, ShortName: "Q1", FieldId: 2, Red1: 101},
	))

	recorder := web.getHttpResponse("/reports/csv/rankings?conference=2")
	assert.Equal(t, 200, recorder.Code)
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	assert.Equal(t, "ConferenceRank,Conference,OverallRank,TeamId,RankingPoints,MatchPoints,AutoFuelPoints,"+
		"TowerPoints,Wins,Losses,Ties,Disqualifications,Played", lines[0])
	assert.Equal(t, 7, len(lines))
	assert.True(t, strings.HasPrefix(lines[1], "1,South,2,102,"), lines[1])

	recorder = web.getHttpResponse("/reports/csv/schedule/qualification")
	assert.Contains(t, recorder.Body.String(), "Match,Type,Time,Field,Conference,")
	assert.Contains(t, recorder.Body.String(), "Q1,Qualification,")
	recorder = web.getHttpResponse("/reports/csv/schedule/qualification?field=1")
	assert.NotContains(t, recorder.Body.String(), "Q1,")

	recorder = web.getHttpResponse("/reports/csv/teams")
	assert.Contains(t, recorder.Body.String(), "101,North,")

	recorder = web.getHttpResponse("/reports/pdf/rankings?conference=1")
	assert.Equal(t, 200, recorder.Code)

	recorder = web.getHttpResponse("/api/bracket/svg?conference=all")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `id="matchup_N-F"`)
	assert.Contains(t, body, `id="matchup_S-F"`)
	assert.Contains(t, body, `id="matchup_F"`)
	assert.Contains(t, body, ">N1<")
	assert.Contains(t, body, "Winner NF1")
	recorder = web.getHttpResponse("/api/bracket/svg?conference=2")
	assert.NotContains(t, recorder.Body.String(), `id="matchup_N-F"`)
	assert.Contains(t, recorder.Body.String(), "South Conference")
}

func TestDevToolsRequireSimulation(t *testing.T) {
	web := setupTestWeb(t)
	recorder := web.postHttpResponse("/api/dev/seed_event", "")
	assert.Equal(t, 409, recorder.Code)
	recorder = web.postHttpResponse("/api/dev/simulate_match", "")
	assert.Equal(t, 409, recorder.Code)

	web.arena.SimulateHardware = true
	recorder = web.postHttpResponse("/api/dev/seed_event", "teams=24&matchesPerTeam=6")
	assert.Equal(t, 200, recorder.Code, recorder.Body.String())
	teams, _ := web.arena.Database.GetAllTeams()
	assert.Equal(t, 24, len(teams))
	matches, _ := web.arena.Database.GetMatchesByType(model.Qualification, true)
	assert.Equal(t, 24, len(matches))

	// Simulate a match and check that its result was committed.
	recorder = web.postHttpResponse("/api/dev/simulate_match", "type=qualification")
	assert.Equal(t, 200, recorder.Code, recorder.Body.String())
	matches, _ = web.arena.Database.GetMatchesByType(model.Qualification, true)
	assert.True(t, matches[0].IsComplete())
	rankings, _ := web.arena.Database.GetAllRankings()
	assert.Equal(t, 6, len(rankings))
}

func TestMultiConferenceBracketReport(t *testing.T) {
	web := setupMultiConferenceWeb(t)
	recorder := web.getHttpResponse("/reports/pdf/bracket")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "matchup_S-F")
	recorder = web.getHttpResponse("/reports/pdf/bracket?conference=1")
	assert.Equal(t, 200, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "matchup_S-F")
}

func TestDevSeedFromRoster(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.SimulateHardware = true
	var roster strings.Builder
	for i := 1; i <= 24; i++ {
		conference := "NM"
		if i > 16 {
			conference = "CM"
		}
		roster.WriteString(fmt.Sprintf("%d,%s,Team %d's Bot\n", 100+i, conference, i))
	}
	recorder := web.postHttpResponse(
		"/api/dev/seed_event",
		"matchesPerTeam=6&conf1Name=NMRC&conf1Short=NM&conf2Name=CMRC&conf2Short=CM&roster="+
			strings.ReplaceAll(strings.ReplaceAll(roster.String(), "\n", "%0A"), "'", "%27"),
	)
	assert.Equal(t, 200, recorder.Code, recorder.Body.String())
	team, _ := web.arena.Database.GetTeamById(117)
	assert.Equal(t, 2, team.ConferenceId)
	assert.Equal(t, "Team 17's Bot", team.Nickname)
	conferences, _ := web.arena.Database.GetAllConferences()
	assert.Equal(t, "CMRC", conferences[1].Name)
	assert.Equal(t, "CM", conferences[1].ShortName)
	// Each conference's bracket is sized to its own team count (16 and 8 teams).
	assert.Equal(t, 5, conferences[0].NumAlliances)
	assert.Equal(t, 2, conferences[1].NumAlliances)
	assert.True(t, web.arena.PlayoffTournament.IsMultiConference())
}

func TestDevSeedRejectsBadRostersWithoutSavingAnything(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.SimulateHardware = true
	names := "&conf1Name=NMRC&conf1Short=NM&conf2Name=CMRC&conf2Short=CM"
	buildRoster := func(lines ...string) string {
		var roster strings.Builder
		for i := 1; i <= 12; i++ {
			roster.WriteString(fmt.Sprintf("%d,NM,Team %d%%0A%d,CM,Team %d%%0A", 100+i, i, 200+i, i))
		}
		for _, line := range lines {
			roster.WriteString(line + "%0A")
		}
		return roster.String()
	}

	for _, test := range []struct {
		roster   string
		expected string
	}{
		{buildRoster("101,CM,Duplicate"), "listed more than once"},
		{buildRoster("300,SMRC,Wrong conference"), "neither"},
		{"number,conference,nickname%0A101,NM,A%0A102,NM,B%0A103,CM,C", "at least 6"},
		{"no teams here", "doesn't contain any teams"},
	} {
		recorder := web.postHttpResponse("/api/dev/seed_event", "matchesPerTeam=6"+names+"&roster="+test.roster)
		assert.Equal(t, 400, recorder.Code, test.expected)
		assert.Contains(t, recorder.Body.String(), test.expected)
		teams, _ := web.arena.Database.GetAllTeams()
		assert.Empty(t, teams, test.expected)
		matches, _ := web.arena.Database.GetMatchesByType(model.Qualification, true)
		assert.Empty(t, matches, test.expected)
	}

	// Conference names are matched case-insensitively, by name or short name, and a header line is skipped.
	roster := "number,conference,nickname%0A" + buildRoster("300,cmrc,Lowercase name")
	recorder := web.postHttpResponse("/api/dev/seed_event", "matchesPerTeam=6"+names+"&roster="+roster)
	assert.Equal(t, 200, recorder.Code, recorder.Body.String())
	team, _ := web.arena.Database.GetTeamById(300)
	assert.Equal(t, 2, team.ConferenceId)
}

func TestNodeRefusesChangesToHubOwnedData(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.MultiFieldRole = model.NodeRole
	web.arena.EventSettings.FieldId = 1

	web.arena.EventSettings.HubAddress = "http://10.0.0.5:8080"
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 1114}))

	for _, path := range []string{
		"/setup/teams", "/setup/teams/clear", "/setup/teams/conferences", "/setup/schedule/generate",
		"/setup/schedule/save", "/setup/awards", "/setup/breaks", "/setup/db/clear/qualification",
		"/setup/db/clear/playoff",
	} {
		recorder := web.postHttpResponse(path, "teamNumbers=254")
		assert.Equal(t, 409, recorder.Code, path)
		assert.Contains(t, recorder.Body.String(), "Make this change on the hub", path)
	}
	teams, _ := web.arena.Database.GetAllTeams()
	assert.Equal(t, 1, len(teams))
	recorder := web.getHttpResponse("/setup/teams/generate_wpa_keys?all=true")
	assert.Equal(t, 409, recorder.Code)

	// The pages themselves say that they are view-only, with a link to the same page on the hub.
	for _, path := range []string{
		"/setup/teams", "/setup/schedule", "/setup/awards", "/setup/breaks", "/alliance_selection",
	} {
		recorder = web.getHttpResponse(path + "?matchType=qualification")
		assert.Equal(t, 200, recorder.Code, path)
		assert.Contains(t, recorder.Body.String(), "<b>View only.</b>", path)
		assert.Contains(t, recorder.Body.String(), `href="http://10.0.0.5:8080`+path+`"`, path)
	}
	// The settings page doesn't offer to clear data that comes from the hub.
	recorder = web.getHttpResponse("/setup/settings")
	assert.NotContains(t, recorder.Body.String(), "Clear Qualification Data")
	assert.Contains(t, recorder.Body.String(), "clear them on the hub instead")

	// Alliance selection can't be started on a node either.
	recorder = web.postHttpResponse("/alliance_selection/start", "")
	assert.Contains(t, recorder.Body.String(), "Alliance selection is run on the hub")
	assert.Empty(t, web.arena.AllianceSelectionAlliances)

	// Once the node is promoted to standalone, the same changes are allowed.
	web.arena.EventSettings.MultiFieldRole = model.StandaloneRole
	web.arena.EventSettings.TbaDownloadEnabled = false
	recorder = web.postHttpResponse("/setup/teams", "teamNumbers=254")
	assert.NotEqual(t, 409, recorder.Code)
}
