// Copyright 2026 Team 254. All Rights Reserved.
//
// The step-by-step checklist on the hub's Event Control page, which tells the operator what has been done and what to
// do next, from connecting the fields through to the awards.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"net"
	"net/http"
	"sort"
	"strings"
)

const (
	stepDone    = "done"
	stepTodo    = "todo"
	stepProblem = "problem"
)

// checklistStep is one line of the Event Control checklist.
type checklistStep struct {
	Title    string
	Detail   string
	State    string
	Current  bool
	Link     string
	LinkText string
	// Optional inline action: "secret" (set the shared secret) or "approve" (approve the node for FieldId).
	Action  string
	FieldId int
}

// Builds the checklist from the current state of the event.
func (web *Web) eventChecklist() ([]checklistStep, error) {
	settings := web.arena.EventSettings
	database := web.arena.Database
	var steps []checklistStep

	// Connecting the fields.
	if settings.HubSharedSecret == "" {
		steps = append(steps, checklistStep{
			Title:  "Set a shared secret",
			Detail: "Field nodes must know this secret to connect. Enter the same secret on each field laptop.",
			State:  stepProblem,
			Action: "secret",
		})
	} else {
		steps = append(steps, checklistStep{
			Title:  "Shared secret set",
			Detail: "Enter the same secret on each field laptop (Settings → Multi-Field).",
			State:  stepDone,
			Action: "secret",
		})
	}
	statuses := map[int]bool{}
	approved := map[int]bool{}
	registered := map[int]bool{}
	addresses := map[int]string{}
	duplicates := map[int]string{}
	if web.hub != nil {
		for _, status := range web.hub.NodeStatuses() {
			registered[status.FieldId] = true
			statuses[status.FieldId] = status.Connected
			approved[status.FieldId] = status.Approved
			if host, _, err := net.SplitHostPort(status.RemoteAddr); err == nil {
				addresses[status.FieldId] = host
			}
			duplicates[status.FieldId] = status.DuplicateFrom
		}
	}
	for fieldId := 1; fieldId <= model.MaxFieldId; fieldId++ {
		step := checklistStep{Title: fmt.Sprintf("Connect Field %d", fieldId), FieldId: fieldId}
		switch {
		case registered[fieldId] && !approved[fieldId]:
			step.State = stepProblem
			step.Detail = fmt.Sprintf("Field %d is asking to join", fieldId)
			if addresses[fieldId] != "" {
				step.Detail += " from " + addresses[fieldId]
			}
			step.Detail += ". Approve it if that's your field laptop."
			step.Action = "approve"
		case statuses[fieldId] && duplicates[fieldId] != "":
			step.State = stepProblem
			step.Title = fmt.Sprintf("Field %d connected, but another laptop also says it's Field %d", fieldId, fieldId)
			step.Detail = fmt.Sprintf(
				"%s tried to connect as Field %d and was refused. Check the Field ID on each field laptop "+
					"(Settings → Multi-Field): one must be Field 1 and the other Field 2.",
				duplicates[fieldId], fieldId,
			)
		case statuses[fieldId]:
			step.State = stepDone
			step.Title = fmt.Sprintf("Field %d connected", fieldId)
			if addresses[fieldId] != "" {
				step.Detail = "From " + addresses[fieldId]
			}
		case registered[fieldId]:
			step.State = stepProblem
			step.Detail = fmt.Sprintf(
				"Field %d was approved but isn't connected right now. Check that it's running and on the network.",
				fieldId,
			)
		default:
			step.State = stepTodo
			step.Detail = "Not connected yet. See \"How to connect a field\" below."
		}
		steps = append(steps, step)
	}

	// Teams and schedule.
	teams, err := database.GetAllTeams()
	if err != nil {
		return nil, err
	}
	teamStep := checklistStep{Title: "Add teams", Link: "/setup/teams", LinkText: "Team List", State: stepTodo}
	if len(teams) > 0 {
		teamStep.State = stepDone
		teamStep.Title = fmt.Sprintf("%d teams", len(teams))
		if settings.MultiConferenceEnabled {
			counts := map[int]int{}
			for _, team := range teams {
				counts[team.ConferenceId]++
			}
			conferences, err := database.GetAllConferences()
			if err != nil {
				return nil, err
			}
			var parts []string
			for _, conference := range conferences {
				parts = append(parts, fmt.Sprintf("%s %d", conference.Name, counts[conference.Id]))
			}
			teamStep.Detail = strings.Join(parts, ", ")
			if counts[0] > 0 {
				teamStep.State = stepProblem
				teamStep.Detail += fmt.Sprintf("; %d without a conference", counts[0])
			}
			alliances, err := database.GetAllAlliances()
			if err != nil {
				return nil, err
			}
			if check := checkConferenceTeams(teams, conferences, settings); len(check.SizeProblems) > 0 &&
				len(alliances) == 0 {
				// Caught here so that it can be fixed before either conference's alliance selection locks the playoffs.
				teamStep.State = stepProblem
				teamStep.Detail += ". " + strings.Join(check.SizeProblems, ". ")
			}
		}
	}
	steps = append(steps, teamStep)

	quals, err := database.GetMatchesByType(model.Qualification, true)
	if err != nil {
		return nil, err
	}
	scheduleStep := checklistStep{
		Title: "Generate the qualification schedule", Link: "/setup/schedule", LinkText: "Match Scheduling",
		State: stepTodo,
	}
	if len(quals) > 0 {
		scheduleStep.State = stepDone
		scheduleStep.Title = fmt.Sprintf("Qualification schedule: %d matches", len(quals))
		unassigned := 0
		for _, match := range quals {
			if match.FieldId == 0 {
				unassigned++
			}
		}
		if unassigned > 0 && settings.QualFieldAssignmentMode == model.DynamicFieldAssignment {
			scheduleStep.Detail = "Fields press Load Next Available to claim matches (dynamic assignment)."
		} else if unassigned > 0 {
			scheduleStep.State = stepProblem
			scheduleStep.Detail = fmt.Sprintf(
				"%d matches have no field; regenerate the schedule with the field assignment you want.", unassigned,
			)
		}
	}
	steps = append(steps, scheduleStep)

	// Qualifications.
	played, perField := countPlayed(quals)
	qualStep := checklistStep{Title: "Play qualifications", State: stepTodo}
	if len(quals) > 0 {
		qualStep.Title = fmt.Sprintf("Qualifications: %d of %d played", played, len(quals))
		qualStep.Detail = perField
		if played == len(quals) {
			qualStep.State = stepDone
		}
	}
	steps = append(steps, qualStep)

	// Alliance selection.
	alliances, err := database.GetAllAlliances()
	if err != nil {
		return nil, err
	}
	if settings.MultiConferenceEnabled {
		conferences, err := database.GetAllConferences()
		if err != nil {
			return nil, err
		}
		for _, conference := range conferences {
			count := 0
			for _, alliance := range alliances {
				if alliance.ConferenceId == conference.Id {
					count++
				}
			}
			step := checklistStep{
				Title:    conference.Name + " alliance selection",
				Link:     fmt.Sprintf("/alliance_selection?conference=%d", conference.Id),
				LinkText: "Alliance Selection",
				State:    stepTodo,
			}
			if count > 0 {
				step.State = stepDone
				step.Detail = fmt.Sprintf("%d alliances", count)
			}
			steps = append(steps, step)
		}
	} else {
		step := checklistStep{
			Title: "Alliance selection", Link: "/alliance_selection", LinkText: "Alliance Selection", State: stepTodo,
		}
		if len(alliances) > 0 {
			step.State = stepDone
			step.Detail = fmt.Sprintf("%d alliances", len(alliances))
		}
		steps = append(steps, step)
	}

	// Playoffs and awards.
	playoffs, err := database.GetMatchesByType(model.Playoff, false)
	if err != nil {
		return nil, err
	}
	playoffStep := checklistStep{
		Title: "Play the playoffs", State: stepTodo, Link: "/displays/bracket?conference=all", LinkText: "Bracket",
	}
	if len(playoffs) > 0 {
		playoffPlayed, playoffPerField := countPlayed(playoffs)
		playoffStep.Title = fmt.Sprintf("Playoffs: %d matches played", playoffPlayed)
		playoffStep.Detail = playoffPerField
		if web.arena.PlayoffTournament != nil && web.arena.PlayoffTournament.IsComplete() {
			playoffStep.State = stepDone
			playoffStep.Title = "Playoffs complete"
		}
		waiting := 0
		for _, match := range playoffs {
			if !match.IsComplete() && match.Status != game.MatchHidden && match.PlayoffRedAlliance > 0 &&
				match.PlayoffBlueAlliance > 0 && match.FieldId == 0 && settings.IsHub() {
				waiting++
			}
		}
		if waiting > 0 {
			playoffStep.State = stepProblem
			playoffStep.Detail = fmt.Sprintf(
				"%d ready match(es) have no field yet; pick one under \"Assign Match to Field\".", waiting,
			)
		}
	}
	steps = append(steps, playoffStep)

	awards, err := database.GetAllAwards()
	if err != nil {
		return nil, err
	}
	awardStep := checklistStep{Title: "Awards", Link: "/setup/awards", LinkText: "Awards", State: stepTodo}
	winners := 0
	for _, award := range awards {
		if award.Type == model.WinnerAward || award.Type == model.EventChampionAward ||
			award.Type == model.ConferenceWinnerAward {
			winners++
		}
	}
	if winners > 0 {
		awardStep.Detail = fmt.Sprintf("%d winner awards generated from the playoffs", winners)
		if playoffStep.State == stepDone {
			awardStep.State = stepDone
		}
	} else {
		awardStep.Detail = "Winner and finalist awards are created automatically as the playoffs finish."
	}
	steps = append(steps, awardStep)

	// Highlight the first step that still needs attention.
	for i := range steps {
		if steps[i].State != stepDone {
			steps[i].Current = true
			break
		}
	}
	return steps, nil
}

// Returns the number of completed matches and a per-field breakdown such as "Field 1: 10/12, Field 2: 9/12".
func countPlayed(matches []model.Match) (int, string) {
	played := 0
	totals := map[int]int{}
	done := map[int]int{}
	for _, match := range matches {
		if match.Status == game.MatchHidden {
			continue
		}
		totals[match.FieldId]++
		if match.IsComplete() {
			played++
			done[match.FieldId]++
		}
	}
	var fieldIds []int
	for fieldId := range totals {
		fieldIds = append(fieldIds, fieldId)
	}
	sort.Ints(fieldIds)
	var parts []string
	for _, fieldId := range fieldIds {
		name := fmt.Sprintf("Field %d", fieldId)
		if fieldId == 0 {
			name = "No field yet"
		}
		parts = append(parts, fmt.Sprintf("%s: %d/%d", name, done[fieldId], totals[fieldId]))
	}
	return played, strings.Join(parts, ", ")
}

// hubAddress is an address at which field nodes may be able to reach this hub.
type hubAddress struct {
	Address string
	// Name of the network adapter, e.g. "Wi-Fi" or "Ethernet".
	Label string
}

// Returns the addresses at which field nodes can probably reach this hub, most likely first: the address in use by
// this browser, then physical network adapters, then virtual ones (VPNs, virtual machines), then localhost.
func hubAddressesForNodes(r *http.Request) []hubAddress {
	port := model.DefaultHubPort
	if _, requestPort, err := net.SplitHostPort(r.Host); err == nil && requestPort != "" {
		port = requestPort
	}
	type candidate struct {
		hubAddress
		rank int
	}
	var candidates []candidate
	seen := map[string]bool{}
	add := func(host, label string, rank int) {
		address := "http://" + net.JoinHostPort(host, port)
		if !seen[address] {
			seen[address] = true
			candidates = append(candidates, candidate{hubAddress{address, label}, rank})
		}
	}
	if host, _, err := net.SplitHostPort(r.Host); err == nil && host != "localhost" && host != "127.0.0.1" &&
		host != "::1" {
		add(host, "the address this page was opened with", 0)
	}
	if interfaces, err := net.Interfaces(); err == nil {
		for _, networkInterface := range interfaces {
			if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addresses, err := networkInterface.Addrs()
			if err != nil {
				continue
			}
			rank := adapterRank(networkInterface.Name)
			for _, address := range addresses {
				ipNet, ok := address.(*net.IPNet)
				if !ok || ipNet.IP.To4() == nil || ipNet.IP.IsLoopback() || ipNet.IP.IsLinkLocalUnicast() {
					continue
				}
				addressRank := rank
				// Carrier-grade NAT addresses are almost always a VPN such as Tailscale.
				if ip := ipNet.IP.To4(); ip[0] == 100 && ip[1]&0xc0 == 64 && addressRank < 3 {
					addressRank = 3
				}
				add(ipNet.IP.String(), networkInterface.Name, addressRank)
			}
		}
	}
	add("localhost", "only for field instances on this same computer", 5)
	sort.SliceStable(
		candidates,
		func(i, j int) bool {
			return candidates[i].rank < candidates[j].rank
		},
	)
	addresses := make([]hubAddress, len(candidates))
	for i, candidate := range candidates {
		addresses[i] = candidate.hubAddress
	}
	return addresses
}

// Ranks a network adapter by how likely field laptops are to reach the hub through it: 1 for a physical adapter, 2 for
// a virtual switch bridged to one (e.g. Hyper-V "vEthernet (Ethernet)"), 3 for a VPN, and 4 for adapters that only
// reach virtual machines on this computer.
func adapterRank(name string) int {
	lowerName := strings.ToLower(name)
	containsAny := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(lowerName, word) {
				return true
			}
		}
		return false
	}
	switch {
	case containsAny("default switch", "wsl", "docker", "vbox", "virtualbox", "vmware", "vmnet", "host-only"):
		return 4
	case containsAny("tailscale", "zerotier", "vpn", "wireguard", "tun", "tap", "utun"):
		return 3
	case containsAny("vethernet", "virtual", "bridge", "hyper-v"):
		return 2
	}
	return 1
}
