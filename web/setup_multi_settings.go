// Copyright 2026 Team 254. All Rights Reserved.
//
// Parsing and validation of the Multi-Conference and Multi-Field settings tabs.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/playoff"
	"net/http"
	"strconv"
	"strings"
)

// Parses the multi-conference and multi-field settings from the form, validates them and applies them to the given
// settings and to the conference records. If they are invalid, it returns a message explaining why and the conferences
// as submitted, leaves the submitted values in eventSettings so that the form can be shown again for the operator to
// correct, and saves nothing.
func (web *Web) applyMultiSettings(
	r *http.Request, eventSettings *model.EventSettings,
) (string, []model.Conference, error) {
	updated := *eventSettings
	var conferences []model.Conference
	fail := func(message string) (string, []model.Conference, error) {
		*eventSettings = updated
		return message, conferences, nil
	}

	existingConferences, err := web.arena.Database.EnsureConferences()
	if err != nil {
		return "", nil, err
	}

	if r.PostFormValue("multiConferenceForm") == "1" {
		updated.MultiConferenceEnabled = r.PostFormValue("multiConferenceEnabled") == "on"
		updated.PreferMixedConferenceAlliances = r.PostFormValue("preferMixedConferenceAlliances") == "on"
		updated.AllianceFinalizeMode = model.AllianceFinalizeMode(formInt(r, "allianceFinalizeMode", 0))
		updated.ChampionshipFormat = model.ChampionshipFormat(formInt(r, "championshipFormat", 0))
		updated.ChampionshipSeriesLength = formInt(r, "championshipSeriesLength", 3)
		updated.ChampionshipFieldMode = model.ChampionshipFieldMode(formInt(r, "championshipFieldMode", 0))
		updated.ChampionshipBreakSec = formInt(r, "championshipBreakSec", 900)

		conferences = append([]model.Conference(nil), existingConferences...)
		for i := range conferences {
			conference := &conferences[i]
			prefix := fmt.Sprintf("conf%d", conference.Id)
			if _, ok := r.PostForm[prefix+"Name"]; !ok {
				continue
			}
			conference.Name = strings.TrimSpace(r.PostFormValue(prefix + "Name"))
			conference.ShortName = strings.TrimSpace(r.PostFormValue(prefix + "ShortName"))
			conference.Color = r.PostFormValue(prefix + "Color")
			conference.LogoSuffix = r.PostFormValue(prefix + "LogoSuffix")
			conference.PlayoffType = model.PlayoffType(formInt(r, prefix+"PlayoffType", int(conference.PlayoffType)))
			conference.NumAlliances = formInt(r, prefix+"NumAlliances", conference.NumAlliances)
			conference.PlayoffFieldId = formInt(r, prefix+"FieldId", conference.PlayoffFieldId)
			seriesLengths := make(map[string]int)
			for _, round := range []string{"EF", "QF", "SF", "F"} {
				seriesLengths[round] = formInt(r, prefix+"Series"+round, conference.SeriesLength(round))
			}
			conference.SeriesLengths = seriesLengths
		}
	}

	if r.PostFormValue("multiFieldForm") == "1" {
		updated.MultiFieldRole = model.MultiFieldRole(formInt(r, "multiFieldRole", int(updated.MultiFieldRole)))
		updated.FieldId = formInt(r, "fieldId", updated.FieldId)
		updated.FieldName = strings.TrimSpace(r.PostFormValue("fieldName"))
		updated.HubAddress = strings.TrimSpace(r.PostFormValue("hubAddress"))
		updated.HubSharedSecret = strings.TrimSpace(r.PostFormValue("hubSharedSecret"))
		updated.QualFieldAssignmentMode = model.QualFieldAssignmentMode(
			formInt(r, "qualFieldAssignmentMode", int(updated.QualFieldAssignmentMode)),
		)
		updated.MinTurnaroundSec = formInt(r, "minTurnaroundSec", updated.MinTurnaroundSec)
		updated.SinglePlayoffFieldInterleave = r.PostFormValue("singlePlayoffFieldInterleave") == "on"
	}

	if message := validateMultiFieldSettings(&updated); message != "" {
		return fail(message)
	}

	alliances, err := web.arena.Database.GetAllAlliances()
	if err != nil {
		return "", nil, err
	}
	if len(alliances) > 0 {
		// The playoff structure is locked once alliances exist, in the same way as the single-conference playoff type.
		const unlockHint = " To change it, first use Clear Playoff/Alliance Data on the Event tab (the database is " +
			"backed up automatically), then run alliance selection again."
		if updated.MultiConferenceEnabled != eventSettings.MultiConferenceEnabled {
			return fail("Cannot turn multi-conference mode on or off after alliance selection has been finalized." +
				unlockHint)
		}
		if updated.MultiConferenceEnabled && conferences != nil {
			if conferencePlayoffsChanged(existingConferences, conferences) ||
				updated.ChampionshipFormat != eventSettings.ChampionshipFormat ||
				updated.EffectiveChampionshipSeriesLength() != eventSettings.EffectiveChampionshipSeriesLength() {
				return fail("Cannot change conference playoff formats after alliance selection has been finalized." +
					unlockHint)
			}
		}
	}

	if updated.MultiConferenceEnabled {
		checkConferences := conferences
		if checkConferences == nil {
			checkConferences = existingConferences
		}
		_, err = playoff.NewMultiConferencePlayoffTournament(
			checkConferences, playoff.MultiConferenceOptionsFromSettings(&updated),
		)
		if err != nil {
			return fail("Invalid conference playoff configuration: " + err.Error())
		}

		// Catch conferences that are too small for their alliances now, rather than at alliance selection, when the
		// other conference may already have locked the playoff structure.
		enabling := !eventSettings.MultiConferenceEnabled
		if len(alliances) == 0 && (enabling || conferenceSizesChanged(existingConferences, checkConferences)) {
			teams, err := web.arena.Database.GetAllTeams()
			if err != nil {
				return "", nil, err
			}
			if len(teams) > 0 {
				check := checkConferenceTeams(teams, checkConferences, &updated)
				if enabling && !check.ok() {
					return fail("Cannot enable multi-conference mode: " + check.String() + ".")
				}
				if len(check.SizeProblems) > 0 {
					return fail("Conference too small: " + strings.Join(check.SizeProblems, "; ") + ".")
				}
			}
		}
	}

	// Everything is valid; apply the changes.
	for i := range conferences {
		if err = web.arena.Database.UpdateConference(&conferences[i]); err != nil {
			return "", nil, err
		}
	}
	*eventSettings = updated
	return "", nil, nil
}

// Checks the combination of multi-field settings for mistakes, filling in what can be worked out (the full hub address,
// and a shared secret for a hub that doesn't have one yet).
func validateMultiFieldSettings(settings *model.EventSettings) string {
	if settings.MultiFieldRole < model.StandaloneRole || settings.MultiFieldRole > model.NodeRole {
		return "Invalid multi-field role."
	}
	hubAddress, err := model.NormalizeHubAddress(settings.HubAddress)
	if err != nil {
		return "Hub address: " + err.Error() + "."
	}
	settings.HubAddress = hubAddress
	if settings.IsNode() {
		if settings.FieldId < 1 || settings.FieldId > model.MaxFieldId {
			return "A field node must have a field ID of 1 or 2."
		}
		if settings.HubAddress == "" {
			return "A field node must have the hub address set: enter the hub laptop's IP address, which is listed on " +
				"the hub's Event Control page."
		}
		if settings.HubSharedSecret == "" {
			return "A field node needs the hub's shared secret, which is shown on the hub's Event Control page."
		}
	}
	if settings.IsHub() && settings.HubSharedSecret == "" {
		// The hub makes one up; the operator copies it from Event Control onto each field laptop.
		settings.HubSharedSecret = model.GenerateSharedSecret()
	}
	if settings.MinTurnaroundSec < 0 {
		return "The minimum turnaround can't be negative."
	}
	if settings.ChampionshipFormat < model.ChampionsSeriesChampionship ||
		settings.ChampionshipFormat > model.NoChampionship {
		return "Invalid championship format."
	}
	return ""
}

// Returns true if any conference's playoff structure differs between the two lists.
func conferencePlayoffsChanged(before, after []model.Conference) bool {
	beforeById := model.ConferenceMap(before)
	for _, conference := range after {
		previous, ok := beforeById[conference.Id]
		if !ok || previous.PlayoffType != conference.PlayoffType || previous.NumAlliances != conference.NumAlliances ||
			previous.ShortName != conference.ShortName {
			return true
		}
		for _, round := range []string{"EF", "QF", "SF", "F"} {
			if previous.SeriesLength(round) != conference.SeriesLength(round) {
				return true
			}
		}
	}
	return false
}

// Returns true if any conference's number of alliances differs between the two lists.
func conferenceSizesChanged(before, after []model.Conference) bool {
	beforeById := model.ConferenceMap(before)
	for _, conference := range after {
		if previous, ok := beforeById[conference.Id]; !ok || conference.NumAlliances != previous.NumAlliances {
			return true
		}
	}
	return false
}

// conferenceTeamCheck describes what is wrong with the conference assignment of an event's teams.
type conferenceTeamCheck struct {
	// Teams that aren't in any conference.
	Unassigned []int
	// Conferences with too few teams to fill their alliances, with what to do about it.
	SizeProblems []string
}

func (check conferenceTeamCheck) ok() bool {
	return len(check.Unassigned) == 0 && len(check.SizeProblems) == 0
}

// Returns a description of the problems, e.g. "teams without a conference: 254, 1114; South has 19 teams but ...".
func (check conferenceTeamCheck) String() string {
	var problems []string
	if len(check.Unassigned) > 0 {
		var teamIds []string
		for i, teamId := range check.Unassigned {
			if i == 10 {
				teamIds = append(teamIds, "...")
				break
			}
			teamIds = append(teamIds, strconv.Itoa(teamId))
		}
		problems = append(problems, "teams without a conference: "+strings.Join(teamIds, ", "))
	}
	problems = append(problems, check.SizeProblems...)
	return strings.Join(problems, "; ")
}

// Checks that every team is assigned to a conference and that each conference has enough teams to fill its alliances.
func checkConferenceTeams(
	teams []model.Team, conferences []model.Conference, settings *model.EventSettings,
) conferenceTeamCheck {
	var check conferenceTeamCheck
	counts := make(map[int]int)
	for _, team := range teams {
		if team.ConferenceId == 0 {
			check.Unassigned = append(check.Unassigned, team.Id)
		} else {
			counts[team.ConferenceId]++
		}
	}
	teamsPerAlliance := 3
	if settings.SelectionRound3Order != "" {
		teamsPerAlliance = 4
	}
	for _, conference := range conferences {
		needed := conference.NumAlliances * teamsPerAlliance
		count := counts[conference.Id]
		if count >= needed {
			continue
		}
		minAlliances := 4
		if conference.PlayoffType == model.SingleEliminationPlayoff {
			minAlliances = 2
		}
		fix := "add teams to it"
		if fits := count / teamsPerAlliance; fits >= minAlliances {
			fix = fmt.Sprintf("lower its number of alliances to %d or fewer (Settings → Multi-Conference)", fits)
		}
		check.SizeProblems = append(
			check.SizeProblems,
			fmt.Sprintf(
				"%s has %d teams but %d alliances of %d need %d; %s", conference.Name, count, conference.NumAlliances,
				teamsPerAlliance, needed, fix,
			),
		)
	}
	return check
}

// Returns the integer value of the given form field, or the default if it is missing or invalid.
func formInt(r *http.Request, name string, defaultValue int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue(name)))
	if err != nil {
		return defaultValue
	}
	return value
}
