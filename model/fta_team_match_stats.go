// Copyright 2026 Team 254. All Rights Reserved.
//
// Model and datastore CRUD methods for a per-match summary of a team's robot connection health, for use by the FTA.

package model

import (
	"sort"
	"time"
)

type FtaTeamMatchStats struct {
	Id             int `db:"id"`
	MatchId        int
	MatchType      MatchType
	MatchShortName string
	TeamId         int
	Station        string
	Time           time.Time
	// Lowest battery voltage seen while the robot was linked, or zero if it never linked.
	MinBatteryVoltage float64
	MaxTripTimeMs     int
	MissedPackets     int
	RobotDownSec      float64
	DsDownSec         float64
	FaultCount        int
	BrownoutCount     int
	RobotDropCount    int
	DsDropCount       int
	RobotEverLinked   bool
}

func (database *Database) CreateFtaTeamMatchStats(stats *FtaTeamMatchStats) error {
	return database.ftaTeamMatchStatsTable.create(stats)
}

// Returns the stats for every match the given team has played, oldest first.
func (database *Database) GetFtaTeamMatchStatsByTeamId(teamId int) ([]FtaTeamMatchStats, error) {
	return database.getFtaTeamMatchStats(func(stats *FtaTeamMatchStats) bool { return stats.TeamId == teamId })
}

// Returns the stats for every team in the given match.
func (database *Database) GetFtaTeamMatchStatsByMatchId(matchId int) ([]FtaTeamMatchStats, error) {
	return database.getFtaTeamMatchStats(func(stats *FtaTeamMatchStats) bool { return stats.MatchId == matchId })
}

func (database *Database) GetAllFtaTeamMatchStats() ([]FtaTeamMatchStats, error) {
	return database.getFtaTeamMatchStats(func(stats *FtaTeamMatchStats) bool { return true })
}

func (database *Database) getFtaTeamMatchStats(
	include func(stats *FtaTeamMatchStats) bool,
) ([]FtaTeamMatchStats, error) {
	allStats, err := database.ftaTeamMatchStatsTable.getAll()
	if err != nil {
		return nil, err
	}

	var matchingStats []FtaTeamMatchStats
	for i := range allStats {
		if include(&allStats[i]) {
			matchingStats = append(matchingStats, allStats[i])
		}
	}
	sort.SliceStable(matchingStats, func(i, j int) bool {
		return matchingStats[i].Id < matchingStats[j].Id
	})
	return matchingStats, nil
}

func (database *Database) TruncateFtaTeamMatchStats() error {
	return database.ftaTeamMatchStatsTable.truncate()
}
