// Copyright 2026 Team 254. All Rights Reserved.
//
// Model and datastore CRUD methods for faults detected by the field while monitoring robot connections, for use by
// the FTA.

package model

import (
	"sort"
	"time"
)

type FtaEventType string

const (
	FtaEventDsLost        FtaEventType = "DsLost"
	FtaEventDsRestored    FtaEventType = "DsRestored"
	FtaEventRadioLost     FtaEventType = "RadioLost"
	FtaEventRadioRestored FtaEventType = "RadioRestored"
	FtaEventRobotLost     FtaEventType = "RobotLost"
	FtaEventRobotRestored FtaEventType = "RobotRestored"
	FtaEventLowBattery    FtaEventType = "LowBattery"
	FtaEventBrownout      FtaEventType = "Brownout"
	FtaEventHighTripTime  FtaEventType = "HighTripTime"
	FtaEventPacketLoss    FtaEventType = "PacketLoss"
	FtaEventEStop         FtaEventType = "EStop"
	FtaEventAStop         FtaEventType = "AStop"
	FtaEventBypass        FtaEventType = "Bypass"
	FtaEventWrongStation  FtaEventType = "WrongStation"
	FtaEventFieldEStop    FtaEventType = "FieldEStop"
)

type FtaSeverity string

const (
	FtaSeverityGood FtaSeverity = "good"
	FtaSeverityWarn FtaSeverity = "warn"
	FtaSeverityBad  FtaSeverity = "bad"
)

type FtaEvent struct {
	Id             int `db:"id"`
	MatchId        int
	MatchType      MatchType
	MatchShortName string
	TeamId         int
	Station        string
	MatchTimeSec   float64
	Time           time.Time
	Type           FtaEventType
	Severity       FtaSeverity
	Message        string
}

func (database *Database) CreateFtaEvent(event *FtaEvent) error {
	return database.ftaEventTable.create(event)
}

// Returns all events logged during the given match, oldest first.
func (database *Database) GetFtaEventsByMatchId(matchId int) ([]FtaEvent, error) {
	return database.getFtaEvents(func(event *FtaEvent) bool { return event.MatchId == matchId })
}

// Returns all events logged for the given team, oldest first.
func (database *Database) GetFtaEventsByTeamId(teamId int) ([]FtaEvent, error) {
	return database.getFtaEvents(func(event *FtaEvent) bool { return event.TeamId == teamId })
}

func (database *Database) GetAllFtaEvents() ([]FtaEvent, error) {
	return database.getFtaEvents(func(event *FtaEvent) bool { return true })
}

func (database *Database) getFtaEvents(include func(event *FtaEvent) bool) ([]FtaEvent, error) {
	events, err := database.ftaEventTable.getAll()
	if err != nil {
		return nil, err
	}

	var matchingEvents []FtaEvent
	for i := range events {
		if include(&events[i]) {
			matchingEvents = append(matchingEvents, events[i])
		}
	}
	sort.SliceStable(matchingEvents, func(i, j int) bool {
		return matchingEvents[i].Id < matchingEvents[j].Id
	})
	return matchingEvents, nil
}

func (database *Database) TruncateFtaEvents() error {
	return database.ftaEventTable.truncate()
}
