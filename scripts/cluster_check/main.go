// Copyright 2026 Team 254. All Rights Reserved.
//
// Checks a stopped dev cluster (see dev_cluster.ps1) after a dry run: every node's mirror must match the hub, every
// outbox must be empty, no playable match may be left unplayed, and no team may have been in matches on both fields at
// overlapping times.
//
//   go run ./scripts/cluster_check [-hub dev_cluster/hub.db]

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	hubPath := flag.String("hub", filepath.Join("dev_cluster", "hub.db"), "the hub's database")
	flag.Parse()

	// start_event.ps1 names the field databases after the hub's (hub_field1.db); dev_cluster.ps1 uses field1.db.
	dir := filepath.Dir(*hubPath)
	baseName := strings.TrimSuffix(filepath.Base(*hubPath), filepath.Ext(*hubPath))
	fieldPaths := []string{filepath.Join(dir, baseName+"_field1.db"), filepath.Join(dir, baseName+"_field2.db")}
	if _, err := os.Stat(fieldPaths[0]); err != nil {
		fieldPaths = []string{filepath.Join(dir, "field1.db"), filepath.Join(dir, "field2.db")}
	}

	hub, err := model.OpenDatabase(*hubPath)
	if err != nil {
		fmt.Println("Can't open the hub database (is the cluster still running?):", err)
		os.Exit(2)
	}
	defer hub.Close()

	problems := 0
	report := func(format string, args ...any) {
		fmt.Printf("PROBLEM: "+format+"\n", args...)
		problems++
	}

	for _, name := range fieldPaths {
		node, err := model.OpenDatabase(name)
		if err != nil {
			report("can't open %s: %v", name, err)
			continue
		}
		for _, collection := range model.MirroredCollections {
			hubJson, _ := hub.SnapshotCollection(collection)
			nodeJson, _ := node.SnapshotCollection(collection)
			if collection == model.TeamsCollection {
				// Teams carry field-local state on nodes, so only compare which teams exist.
				var hubTeams, nodeTeams []model.Team
				_ = json.Unmarshal(hubJson, &hubTeams)
				_ = json.Unmarshal(nodeJson, &nodeTeams)
				if len(hubTeams) != len(nodeTeams) {
					report("%s has %d teams but the hub has %d", name, len(nodeTeams), len(hubTeams))
				}
				continue
			}
			if string(hubJson) != string(nodeJson) {
				report("%s: %s differs from the hub", name, collection)
			}
		}
		entries, _ := node.GetAllSyncOutboxEntries()
		if len(entries) > 0 {
			report("%s still has %d results in its outbox", name, len(entries))
		}
		_ = node.Close()
	}

	for _, matchType := range []model.MatchType{model.Qualification, model.Playoff} {
		matches, _ := hub.GetMatchesByType(matchType, true)
		fieldCounts := map[int]int{}
		for i, first := range matches {
			fieldCounts[first.FieldId]++
			if !first.IsComplete() && first.Status != game.MatchHidden && first.Red1 > 0 && first.Blue1 > 0 {
				report("%s is playable but was never played", first.ShortName)
			}
			for _, second := range matches[i+1:] {
				if !overlap(&first, &second) {
					continue
				}
				for _, teamId := range teamIds(&first) {
					for _, otherTeamId := range teamIds(&second) {
						if teamId == otherTeamId {
							report("team %d was in %s and %s at the same time", teamId, first.ShortName, second.ShortName)
						}
					}
				}
			}
		}
		fmt.Printf("%s: %d matches, per field %v\n", matchType, len(matches), fieldCounts)
	}
	receipts, _ := hub.GetAllHubResultReceipts()
	awards, _ := hub.GetAllAwards()
	fmt.Printf("Hub applied %d results and has %d awards.\n", len(receipts), len(awards))

	if problems > 0 {
		fmt.Printf("%d problems found.\n", problems)
		os.Exit(1)
	}
	fmt.Println("OK: the hub and both nodes agree.")
}

// Returns true if the two matches were played on different fields at overlapping times.
func overlap(first, second *model.Match) bool {
	if first.FieldId == second.FieldId || first.StartedAt.IsZero() || second.StartedAt.IsZero() ||
		first.ScoreCommittedAt.IsZero() || second.ScoreCommittedAt.IsZero() {
		return false
	}
	return first.StartedAt.Before(second.ScoreCommittedAt) && second.StartedAt.Before(first.ScoreCommittedAt)
}

func teamIds(match *model.Match) []int {
	var ids []int
	for _, id := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}
