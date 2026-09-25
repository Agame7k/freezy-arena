// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

// Puts a fully-connected team into R1 and establishes the monitor's baseline for it.
func setupFtaMonitorTest(t *testing.T, matchState MatchState) (*Arena, *DriverStationConnection, time.Time) {
	arena := setupTestArena(t)
	arena.MatchState = matchState
	dsConn := &DriverStationConnection{
		TeamId:         254,
		DsLinked:       true,
		RadioLinked:    true,
		RioLinked:      true,
		RobotLinked:    true,
		BatteryVoltage: 12.5,
	}
	arena.AllianceStations["R1"].Team = &model.Team{Id: 254}
	arena.AllianceStations["R1"].DsConn = dsConn
	now := time.Unix(1000, 0)
	events, _ := arena.ftaMonitor.update(arena, now)
	assert.Empty(t, events)
	return arena, dsConn, now
}

func eventTypes(events []model.FtaEvent) []model.FtaEventType {
	types := []model.FtaEventType{}
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func TestFtaMonitorTransitions(t *testing.T) {
	testCases := []struct {
		name       string
		matchState MatchState
		change     func(station *AllianceStation, dsConn *DriverStationConnection)
		expected   []model.FtaEventType
	}{
		{"no change", TeleopPeriod, func(*AllianceStation, *DriverStationConnection) {}, []model.FtaEventType{}},
		{
			"E-stop pre-match",
			PreMatch,
			func(s *AllianceStation, _ *DriverStationConnection) { s.EStop = true },
			[]model.FtaEventType{model.FtaEventEStop},
		},
		{
			"A-stop in auto",
			AutoPeriod,
			func(s *AllianceStation, _ *DriverStationConnection) { s.AStop = true },
			[]model.FtaEventType{model.FtaEventAStop},
		},
		{
			"bypass pre-match",
			PreMatch,
			func(s *AllianceStation, _ *DriverStationConnection) { s.Bypass = true },
			[]model.FtaEventType{model.FtaEventBypass},
		},
		{
			"wrong station pre-match",
			PreMatch,
			func(_ *AllianceStation, d *DriverStationConnection) { d.WrongStation = "B2" },
			[]model.FtaEventType{model.FtaEventWrongStation},
		},
		{
			"robot lost in match",
			TeleopPeriod,
			func(_ *AllianceStation, d *DriverStationConnection) { d.RobotLinked = false },
			[]model.FtaEventType{model.FtaEventRobotLost},
		},
		{
			"robot lost pre-match is ignored",
			PreMatch,
			func(_ *AllianceStation, d *DriverStationConnection) { d.RobotLinked = false },
			[]model.FtaEventType{},
		},
		{
			"DS disconnected in match",
			AutoPeriod,
			func(s *AllianceStation, _ *DriverStationConnection) { s.DsConn = nil },
			[]model.FtaEventType{model.FtaEventDsLost, model.FtaEventRobotLost},
		},
		{
			"low battery in match",
			TeleopPeriod,
			func(_ *AllianceStation, d *DriverStationConnection) { d.BatteryVoltage = 7.2 },
			[]model.FtaEventType{model.FtaEventLowBattery},
		},
		{
			"brownout in match",
			TeleopPeriod,
			func(_ *AllianceStation, d *DriverStationConnection) { d.BatteryVoltage = 6.5 },
			[]model.FtaEventType{model.FtaEventBrownout},
		},
		{
			"low battery pre-match is ignored",
			PreMatch,
			func(_ *AllianceStation, d *DriverStationConnection) { d.BatteryVoltage = 7.2 },
			[]model.FtaEventType{},
		},
		{
			"bypassed robot lost in match is ignored",
			TeleopPeriod,
			func(s *AllianceStation, d *DriverStationConnection) {
				s.Bypass = true
				d.RobotLinked = false
			},
			[]model.FtaEventType{model.FtaEventBypass},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			arena, dsConn, now := setupFtaMonitorTest(t, testCase.matchState)
			testCase.change(arena.AllianceStations["R1"], dsConn)
			events, _ := arena.ftaMonitor.update(arena, now.Add(time.Millisecond*10))
			assert.Equal(t, testCase.expected, eventTypes(events))
			for _, event := range events {
				assert.Equal(t, "R1", event.Station)
				assert.Equal(t, 254, event.TeamId)
			}
		})
	}
}

func TestFtaMonitorRobotRestoredReportsDowntime(t *testing.T) {
	arena, dsConn, now := setupFtaMonitorTest(t, TeleopPeriod)
	dsConn.RioLinked = false
	dsConn.RobotLinked = false
	events, _ := arena.ftaMonitor.update(arena, now.Add(time.Second))
	if assert.Len(t, events, 1) {
		assert.Equal(t, "Robot link lost: No roboRIO (radio up)", events[0].Message)
	}

	dsConn.RioLinked = true
	dsConn.RobotLinked = true
	events, _ = arena.ftaMonitor.update(arena, now.Add(4*time.Second))
	if assert.Len(t, events, 1) {
		assert.Equal(t, model.FtaEventRobotRestored, events[0].Type)
		assert.Equal(t, "Robot link restored (down 3s)", events[0].Message)
	}
}

func TestFtaMonitorLowBatteryLoggedOncePerMatch(t *testing.T) {
	arena, dsConn, now := setupFtaMonitorTest(t, TeleopPeriod)
	for i, voltage := range []float64{7.3, 7.6, 7.2, 8.1, 7.4} {
		dsConn.BatteryVoltage = voltage
		events, _ := arena.ftaMonitor.update(arena, now.Add(time.Duration(i+1)*time.Second))
		if i == 0 {
			assert.Equal(t, []model.FtaEventType{model.FtaEventLowBattery}, eventTypes(events))
		} else {
			// Sagging again later in the match, whether or not the battery recovered in between, isn't re-logged.
			assert.Empty(t, events, "%.1fV", voltage)
		}
	}
}

func TestFtaMonitorHighTripTimeMustBeSustained(t *testing.T) {
	arena, dsConn, now := setupFtaMonitorTest(t, TeleopPeriod)
	dsConn.DsRobotTripTimeMs = 30
	events, _ := arena.ftaMonitor.update(arena, now.Add(time.Second))
	assert.Empty(t, events)
	events, _ = arena.ftaMonitor.update(arena, now.Add(2*time.Second))
	assert.Empty(t, events)
	events, _ = arena.ftaMonitor.update(arena, now.Add(3*time.Second))
	assert.Equal(t, []model.FtaEventType{model.FtaEventHighTripTime}, eventTypes(events))
	events, _ = arena.ftaMonitor.update(arena, now.Add(10*time.Second))
	assert.Empty(t, events)
}

func TestFtaMonitorPacketLoss(t *testing.T) {
	arena, dsConn, now := setupFtaMonitorTest(t, TeleopPeriod)
	dsConn.MissedPacketCount = 0
	events, _ := arena.ftaMonitor.update(arena, now.Add(100*time.Millisecond))
	assert.Empty(t, events)
	dsConn.MissedPacketCount = 12
	events, _ = arena.ftaMonitor.update(arena, now.Add(500*time.Millisecond))
	assert.Equal(t, []model.FtaEventType{model.FtaEventPacketLoss}, eventTypes(events))

	// A second burst inside the cooldown isn't logged again.
	dsConn.MissedPacketCount = 30
	events, _ = arena.ftaMonitor.update(arena, now.Add(900*time.Millisecond))
	assert.Empty(t, events)
}

func TestFtaMonitorTeamChangeIsNotAnEvent(t *testing.T) {
	arena, _, now := setupFtaMonitorTest(t, PreMatch)
	arena.AllianceStations["R1"].Team = &model.Team{Id: 1114}
	arena.AllianceStations["R1"].DsConn = &DriverStationConnection{TeamId: 1114, WrongStation: "B1"}
	events, _ := arena.ftaMonitor.update(arena, now.Add(time.Second))
	assert.Empty(t, events)
}

func TestFtaMonitorFieldEStop(t *testing.T) {
	arena, _, now := setupFtaMonitorTest(t, TeleopPeriod)
	plc := &FakePlc{isEnabled: true}
	arena.Plc = plc
	events, _ := arena.ftaMonitor.update(arena, now.Add(time.Second))
	assert.Empty(t, events)
	plc.fieldEStop = true
	events, _ = arena.ftaMonitor.update(arena, now.Add(2*time.Second))
	if assert.Len(t, events, 1) {
		assert.Equal(t, model.FtaEventFieldEStop, events[0].Type)
		assert.Equal(t, "", events[0].Station)
	}
}

func TestFtaMonitorMatchStats(t *testing.T) {
	arena, dsConn, now := setupFtaMonitorTest(t, PreMatch)
	arena.CurrentMatch = &model.Match{Id: 7, Type: model.Qualification, ShortName: "Q7"}

	arena.MatchState = AutoPeriod
	_, stats := arena.ftaMonitor.update(arena, now.Add(time.Second))
	assert.Empty(t, stats)

	dsConn.BatteryVoltage = 9.1
	dsConn.DsRobotTripTimeMs = 8
	dsConn.MissedPacketCount = 3
	arena.ftaMonitor.update(arena, now.Add(2*time.Second))
	dsConn.RobotLinked = false
	arena.ftaMonitor.update(arena, now.Add(3*time.Second))
	arena.ftaMonitor.update(arena, now.Add(5*time.Second))
	dsConn.RobotLinked = true
	dsConn.BatteryVoltage = 10.2
	arena.ftaMonitor.update(arena, now.Add(6*time.Second))

	arena.MatchState = PostMatch
	_, stats = arena.ftaMonitor.update(arena, now.Add(7*time.Second))
	if assert.Len(t, stats, 1) {
		assert.Equal(t, 7, stats[0].MatchId)
		assert.Equal(t, "Q7", stats[0].MatchShortName)
		assert.Equal(t, 254, stats[0].TeamId)
		assert.Equal(t, "R1", stats[0].Station)
		assert.Equal(t, 9.1, stats[0].MinBatteryVoltage)
		assert.Equal(t, 8, stats[0].MaxTripTimeMs)
		assert.Equal(t, 3, stats[0].MissedPackets)
		assert.InDelta(t, 3.0, stats[0].RobotDownSec, 0.001)
		assert.Equal(t, 1, stats[0].FaultCount)
		assert.True(t, stats[0].RobotEverLinked)
	}

	// Stats are only reported once.
	_, stats = arena.ftaMonitor.update(arena, now.Add(8*time.Second))
	assert.Empty(t, stats)
}

func TestFtaMonitorRecordsToDatabase(t *testing.T) {
	arena, _, _ := setupFtaMonitorTest(t, PreMatch)
	arena.CurrentMatch = &model.Match{Id: 3, ShortName: "P3"}
	arena.AllianceStations["R1"].EStop = true
	arena.updateFtaMonitor()

	events, err := arena.Database.GetFtaEventsByMatchId(3)
	assert.Nil(t, err)
	if assert.Len(t, events, 1) {
		assert.Equal(t, model.FtaEventEStop, events[0].Type)
		assert.Equal(t, "P3", events[0].MatchShortName)
	}
	events, err = arena.Database.GetFtaEventsByTeamId(254)
	assert.Nil(t, err)
	assert.Len(t, events, 1)
}

func TestFtaMonitorRepeatedFaultsAreOnlyLoggedOncePerMatch(t *testing.T) {
	arena, dsConn, now := setupFtaMonitorTest(t, PreMatch)
	arena.MatchState = TeleopPeriod
	arena.ftaMonitor.update(arena, now.Add(time.Second))

	// Brown out five times, recovering fully in between each time.
	var logged []model.FtaEvent
	for i := 0; i < 5; i++ {
		dsConn.BatteryVoltage = 6.2
		events, _ := arena.ftaMonitor.update(arena, now.Add(time.Duration(2*i+2)*time.Second))
		logged = append(logged, events...)
		dsConn.BatteryVoltage = 12.0
		events, _ = arena.ftaMonitor.update(arena, now.Add(time.Duration(2*i+3)*time.Second))
		logged = append(logged, events...)
	}
	if assert.Len(t, logged, 1) {
		assert.Equal(t, model.FtaEventBrownout, logged[0].Type)
		assert.Equal(t, "Brownout: battery at 6.2V (repeats this match won't be logged)", logged[0].Message)
	}

	// Robot drops are logged a few times (with their restores), then go quiet.
	logged = nil
	for i := 0; i < 5; i++ {
		dsConn.RobotLinked = false
		events, _ := arena.ftaMonitor.update(arena, now.Add(time.Duration(20+2*i)*time.Second))
		logged = append(logged, events...)
		dsConn.RobotLinked = true
		events, _ = arena.ftaMonitor.update(arena, now.Add(time.Duration(21+2*i)*time.Second))
		logged = append(logged, events...)
	}
	assert.Equal(
		t,
		[]model.FtaEventType{
			model.FtaEventRobotLost,
			model.FtaEventRobotRestored,
			model.FtaEventRobotLost,
			model.FtaEventRobotRestored,
			model.FtaEventRobotLost,
			model.FtaEventRobotRestored,
		},
		eventTypes(logged),
	)

	// Repeats are still counted in the match stats.
	arena.MatchState = PostMatch
	_, stats := arena.ftaMonitor.update(arena, now.Add(time.Minute))
	if assert.Len(t, stats, 1) {
		assert.Equal(t, 5, stats[0].BrownoutCount)
		assert.Equal(t, 5, stats[0].RobotDropCount)
		assert.Equal(t, 10, stats[0].FaultCount)
	}

	// The next match starts with a clean slate.
	arena.MatchState = AutoPeriod
	arena.ftaMonitor.update(arena, now.Add(2*time.Minute))
	dsConn.BatteryVoltage = 6.2
	events, _ := arena.ftaMonitor.update(arena, now.Add(2*time.Minute+time.Second))
	assert.Equal(t, []model.FtaEventType{model.FtaEventBrownout}, eventTypes(events))
}
