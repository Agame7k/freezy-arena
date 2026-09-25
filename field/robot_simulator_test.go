// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

// Steps the simulator forward in 100ms increments, returning the time it stopped at.
func runRobotSimulator(arena *Arena, simulator *robotSimulator, start time.Time, durationSec float64) time.Time {
	now := start
	for now.Sub(start).Seconds() < durationSec {
		now = now.Add(100 * time.Millisecond)
		simulator.update(arena, now)
	}
	return now
}

func TestRobotSimulatorConnectsLinkByLink(t *testing.T) {
	arena := setupTestArena(t)
	simulator := newRobotSimulator(1)
	arena.AllianceStations["R1"].Team = &model.Team{Id: 254}
	start := time.Unix(1000, 0)

	simulator.update(arena, start)
	dsConn := arena.AllianceStations["R1"].DsConn
	if assert.NotNil(t, dsConn) {
		assert.Equal(t, 254, dsConn.TeamId)
		assert.Equal(t, "R1", dsConn.AllianceStation)
		assert.False(t, dsConn.DsLinked)
	}
	assert.Nil(t, arena.AllianceStations["R2"].DsConn, "empty stations get no connection")

	// Each link comes up in order, never ahead of the one before it.
	robot := simulator.robots["R1"]
	now := start
	for now.Sub(start).Seconds() < robot.linkUpSec[simulatedLevelCode]+1 {
		now = runRobotSimulator(arena, simulator, now, 0.1)
		assert.True(t, !dsConn.RadioLinked || dsConn.DsLinked)
		assert.True(t, !dsConn.RioLinked || dsConn.RadioLinked)
		assert.True(t, !dsConn.RobotLinked || dsConn.RioLinked)
	}
	assert.True(t, dsConn.RobotLinked)
	assert.GreaterOrEqual(t, dsConn.BatteryVoltage, 12.4)
	assert.True(t, arena.AllianceStations["R1"].WifiStatus.RadioLinked)
	assert.Greater(t, arena.AllianceStations["R1"].WifiStatus.SignalNoiseRatio, 0)

	// The connection keeps going as long as the simulator does, rather than timing out (which uses the real clock).
	simulator.update(arena, time.Now())
	assert.Nil(t, dsConn.update(arena, ""))
	assert.True(t, dsConn.RobotLinked)
}

func TestRobotSimulatorNewTeamReboots(t *testing.T) {
	arena := setupTestArena(t)
	simulator := newRobotSimulator(1)
	arena.AllianceStations["B2"].Team = &model.Team{Id: 254}
	now := runRobotSimulator(arena, simulator, time.Unix(1000, 0), 60)
	assert.True(t, arena.AllianceStations["B2"].DsConn.RobotLinked)

	arena.AllianceStations["B2"].Team = &model.Team{Id: 1114}
	runRobotSimulator(arena, simulator, now, 0.1)
	assert.Equal(t, 1114, arena.AllianceStations["B2"].DsConn.TeamId)
	assert.False(t, arena.AllianceStations["B2"].DsConn.RobotLinked)

	arena.AllianceStations["B2"].Team = nil
	runRobotSimulator(arena, simulator, now, 0.1)
	assert.NotContains(t, simulator.robots, "B2")
}

func TestRobotSimulatorOutage(t *testing.T) {
	arena := setupTestArena(t)
	simulator := newRobotSimulator(1)
	arena.AllianceStations["R3"].Team = &model.Team{Id: 254}
	now := runRobotSimulator(arena, simulator, time.Unix(1000, 0), 60)
	arena.MatchState = TeleopPeriod

	// Force the radio to drop; only the driver station should stay linked.
	robot := simulator.robots["R3"]
	robot.outageLevel = simulatedLevelDs
	robot.outageUntil = now.Add(5 * time.Second)
	now = runRobotSimulator(arena, simulator, now, 1)
	dsConn := arena.AllianceStations["R3"].DsConn
	assert.True(t, dsConn.DsLinked)
	assert.False(t, dsConn.RadioLinked)
	assert.False(t, dsConn.RobotLinked)
	assert.Equal(t, 0.0, dsConn.BatteryVoltage)
	assert.Greater(t, dsConn.MissedPacketCount, 0)

	// Once it's over, the robot comes straight back.
	robot.outageUntil = now
	runRobotSimulator(arena, simulator, now, 0.1)
	assert.True(t, dsConn.RobotLinked)
}

func TestRobotSimulatorAllowsMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	arena.EnableRobotSimulator()
	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.False(t, arena.AllianceStations[station].EStop)
	}

	// Even the slowest-booting teams are ready well within a minute.
	teamIds := []int{254, 1114, 3, 148, 971, 2056}
	for i, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		arena.AllianceStations[station].Team = &model.Team{Id: teamIds[i]}
	}
	runRobotSimulator(arena, arena.robotSimulator, time.Now(), 60)
	arena.handlePlcInputOutput()
	assert.Nil(t, arena.checkCanStartMatch())
}

func TestSimulatedProfileForTeam(t *testing.T) {
	assert.Equal(t, simulatedHealthy, simulatedProfileForTeam(254))
	assert.Equal(t, simulatedFlakyRadio, simulatedProfileForTeam(971))
	assert.Equal(t, simulatedWeakBattery, simulatedProfileForTeam(1712))
	assert.Equal(t, simulatedSlowBoot, simulatedProfileForTeam(33))
}
