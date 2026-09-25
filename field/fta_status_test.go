// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

func checksByName(status FtaStationStatus) map[string]bool {
	checks := make(map[string]bool)
	for _, check := range status.Checks {
		checks[check.Name] = check.Ok
	}
	return checks
}

func flagTypes(status FtaStationStatus) []model.FtaEventType {
	types := []model.FtaEventType{}
	for _, flag := range status.Flags {
		types = append(types, flag.Type)
	}
	return types
}

func TestFtaStationStatusEmptyStation(t *testing.T) {
	status := getFtaStationStatus(&AllianceStation{}, false, true)
	assert.True(t, status.Ready)
	assert.Empty(t, status.Checks)
	assert.Empty(t, status.Flags)
}

func TestFtaStationStatusChecklist(t *testing.T) {
	testCases := []struct {
		name          string
		station       AllianceStation
		expectedReady bool
		failingChecks []string
	}{
		{
			"no driver station",
			AllianceStation{Team: &model.Team{Id: 254}, aStopReset: true},
			false,
			[]string{"Ethernet", "Driver station", "Radio", "roboRIO", "Robot code", "Battery"},
		},
		{
			"radio up but no roboRIO",
			AllianceStation{
				Team:       &model.Team{Id: 254},
				Ethernet:   true,
				aStopReset: true,
				DsConn:     &DriverStationConnection{DsLinked: true, RadioLinked: true},
			},
			false,
			[]string{"roboRIO", "Robot code", "Battery"},
		},
		{
			"fully connected on a tired battery is still ready",
			AllianceStation{
				Team:       &model.Team{Id: 254},
				Ethernet:   true,
				aStopReset: true,
				DsConn: &DriverStationConnection{
					DsLinked: true, RadioLinked: true, RioLinked: true, RobotLinked: true, BatteryVoltage: 11.8,
				},
			},
			true,
			[]string{"Battery"},
		},
		{
			"wrong station, E-stopped, A-stop not reset",
			AllianceStation{
				Team:     &model.Team{Id: 254},
				Ethernet: true,
				EStop:    true,
				DsConn: &DriverStationConnection{
					DsLinked:       true,
					RadioLinked:    true,
					RioLinked:      true,
					RobotLinked:    true,
					BatteryVoltage: 12.6,
					WrongStation:   "B1",
				},
			},
			false,
			[]string{"Station", "E-stop clear", "A-stop reset"},
		},
		{
			"bypassed station has no checklist",
			AllianceStation{Team: &model.Team{Id: 254}, Bypass: true},
			true,
			[]string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			status := getFtaStationStatus(&testCase.station, false, true)
			assert.Equal(t, testCase.expectedReady, status.Ready)
			failing := []string{}
			for _, check := range status.Checks {
				if !check.Ok {
					failing = append(failing, check.Name)
				}
			}
			assert.Equal(t, testCase.failingChecks, failing)
		})
	}
}

func TestFtaStationStatusOmitsEthernetWithoutPlc(t *testing.T) {
	station := AllianceStation{Team: &model.Team{Id: 254}, aStopReset: true}
	assert.Contains(t, checksByName(getFtaStationStatus(&station, false, true)), "Ethernet")
	assert.NotContains(t, checksByName(getFtaStationStatus(&station, false, false)), "Ethernet")
}

func TestFtaStationStatusFlags(t *testing.T) {
	connected := func(voltage float64, tripTimeMs int) *DriverStationConnection {
		return &DriverStationConnection{
			DsLinked:          true,
			RadioLinked:       true,
			RioLinked:         true,
			RobotLinked:       true,
			BatteryVoltage:    voltage,
			DsRobotTripTimeMs: tripTimeMs,
		}
	}
	testCases := []struct {
		name         string
		station      AllianceStation
		matchRunning bool
		expected     []model.FtaEventType
	}{
		{"healthy", AllianceStation{DsConn: connected(12.5, 5)}, true, []model.FtaEventType{}},
		{
			"low battery in match",
			AllianceStation{DsConn: connected(7.2, 5)},
			true,
			[]model.FtaEventType{model.FtaEventLowBattery},
		},
		{
			"brownout in match",
			AllianceStation{DsConn: connected(6.5, 5)},
			true,
			[]model.FtaEventType{model.FtaEventBrownout},
		},
		{
			"tired battery pre-match",
			AllianceStation{DsConn: connected(11.9, 5)},
			false,
			[]model.FtaEventType{model.FtaEventLowBattery},
		},
		{
			"high trip time",
			AllianceStation{DsConn: connected(12.5, 25)},
			true,
			[]model.FtaEventType{model.FtaEventHighTripTime},
		},
		{"no DS in match", AllianceStation{}, true, []model.FtaEventType{model.FtaEventDsLost}},
		{"no DS pre-match", AllianceStation{}, false, []model.FtaEventType{}},
		{
			"robot lost in match",
			AllianceStation{DsConn: &DriverStationConnection{DsLinked: true, RadioLinked: true}},
			true,
			[]model.FtaEventType{model.FtaEventRobotLost},
		},
		{
			"stops and bypass",
			AllianceStation{EStop: true, AStop: true, Bypass: true},
			true,
			[]model.FtaEventType{model.FtaEventEStop, model.FtaEventAStop, model.FtaEventBypass},
		},
		{
			"wrong station",
			AllianceStation{DsConn: &DriverStationConnection{WrongStation: "R3"}},
			false,
			[]model.FtaEventType{model.FtaEventWrongStation},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.station.Team = &model.Team{Id: 254}
			status := getFtaStationStatus(&testCase.station, testCase.matchRunning, true)
			assert.Equal(t, testCase.expected, flagTypes(status))
		})
	}
}

func TestStartMatchBlockers(t *testing.T) {
	arena := setupTestArena(t)
	arena.AllianceStations["R1"].Bypass = true
	arena.AllianceStations["R2"].Bypass = true
	arena.AllianceStations["R3"].Bypass = true
	arena.AllianceStations["B1"].Bypass = true
	arena.AllianceStations["B2"].Bypass = true
	arena.AllianceStations["B3"].Bypass = true
	assert.Empty(t, arena.getStartMatchBlockers())

	arena.AllianceStations["B2"].Bypass = false
	arena.AllianceStations["R3"].EStop = true
	blockers := arena.getStartMatchBlockers()
	if assert.Len(t, blockers, 2) {
		assert.Equal(t, "eStop", blockers[0].Code)
		assert.Equal(t, []string{"R3"}, blockers[0].Stations)
		assert.NotEmpty(t, blockers[0].Hint)
		assert.Equal(t, "robotNotConnected", blockers[1].Code)
		assert.Equal(t, []string{"B2"}, blockers[1].Stations)
	}
	assert.Equal(
		t,
		[]string{"an emergency stop is active (R3)", "not all robots are connected or bypassed (B2)"},
		arena.getStartMatchConditions(),
	)

	plc := &FakePlc{isEnabled: true}
	arena.Plc = plc
	codes := []string{}
	for _, blocker := range arena.getStartMatchBlockers() {
		codes = append(codes, blocker.Code)
	}
	assert.Equal(t, []string{"eStop", "robotNotConnected", "ftaNotReady"}, codes)
}
