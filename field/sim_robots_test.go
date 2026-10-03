// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/dssim"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/plc"
	"github.com/stretchr/testify/assert"
	"net"
	"strings"
	"testing"
	"time"
)

// setupSimRobotArena starts the arena's driver station listeners on free local ports and points simulated robots at
// them.
func setupSimRobotArena(t *testing.T) *Arena {
	arena := setupTestArena(t)
	arena.SimControlsEnabled = true
	arena.Plc = plc.NewSimPlc()

	tcpListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if !assert.Nil(t, err) {
		t.FailNow()
	}
	t.Cleanup(func() {
		tcpListener.Close()
	})
	go arena.serveDriverStations(tcpListener)

	// The UDP listener loop doesn't stop when its socket closes, so leave the socket open for the rest of the run.
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if !assert.Nil(t, err) {
		t.FailNow()
	}
	arena.DriverStationUdpSocket = udpConn
	go arena.listenForDsUdpPackets()

	arena.SimRobots = dssim.NewManager(
		"127.0.0.1",
		tcpListener.Addr().(*net.TCPAddr).Port,
		udpConn.LocalAddr().(*net.UDPAddr).Port,
		arena.simStationTeamId,
	)
	t.Cleanup(arena.SimRobots.Close)
	return arena
}

func waitFor(t *testing.T, description string, condition func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.Fail(t, "timed out waiting for "+description)
}

func TestSimRobotsConnectThroughDriverStationProtocol(t *testing.T) {
	arena := setupSimRobotArena(t)
	allianceStation := arena.AllianceStations["R1"]
	assert.Nil(t, arena.assignTeam(254, "R1"))

	// A robot running code links through the arena's real driver station handling.
	assert.Nil(t, arena.SetSimRobotState("R1", dssim.CodeRunning))
	assert.NotNil(t, arena.SetSimRobotState("R7", dssim.CodeRunning))
	assert.NotNil(t, arena.SetSimRobotState("R2", dssim.RobotState(42)))
	waitFor(t, "R1 robot to link", func() bool {
		return allianceStation.DsConn != nil && allianceStation.DsConn.RobotLinked
	})
	assert.Equal(t, 254, allianceStation.DsConn.TeamId)
	assert.True(t, allianceStation.DsConn.RadioLinked)
	assert.True(t, allianceStation.DsConn.RioLinked)
	assert.InDelta(t, 12.6, allianceStation.DsConn.BatteryVoltage, 0.01)

	// The simulated PLC sees the station's Ethernet plugged in.
	redEthernets, _ := arena.Plc.GetEthernetConnected()
	assert.True(t, redEthernets[0])

	// What the arena tells the robot to do comes back through the simulated Driver Station.
	allianceStation.GameData = "R"
	waitFor(t, "R1 robot to be enabled in auto", func() bool {
		arena.sendDsPacket(true, true)
		status := arena.SimRobots.Statuses()[0]
		return status.Enabled && status.Auto && status.GameData == "R"
	})
	status := arena.SimRobots.Statuses()[0]
	assert.Equal(t, "R1", status.Station)
	assert.Equal(t, 254, status.TeamId)
	assert.Equal(t, dssim.ConnectionConnected, status.Connection)
	waitFor(t, "R1 Driver Station to report auto", func() bool {
		return allianceStation.DsConn.DsReportedStatusValid && allianceStation.DsConn.DsReportedAuto
	})

	allianceStation.EStop = true
	waitFor(t, "R1 robot to be E-stopped", func() bool {
		arena.sendDsPacket(false, true)
		status := arena.SimRobots.Statuses()[0]
		return status.EStop && !status.Enabled
	})
	allianceStation.EStop = false

	// Dropping back to just the roboRIO unlinks the robot.
	assert.Nil(t, arena.SetSimRobotState("R1", dssim.RioLinked))
	waitFor(t, "R1 robot code to stop", func() bool {
		return allianceStation.DsConn != nil && allianceStation.DsConn.RioLinked && !allianceStation.DsConn.RobotLinked
	})

	// Loading a different team brings that team's Driver Station up instead.
	assert.Nil(t, arena.assignTeam(1503, "R1"))
	waitFor(t, "team 1503 to connect", func() bool {
		return allianceStation.DsConn != nil && allianceStation.DsConn.TeamId == 1503
	})

	// Turning the robot off disconnects the Driver Station and unplugs the Ethernet.
	assert.Nil(t, arena.SetSimRobotState("R1", dssim.RobotOff))
	waitFor(t, "R1 Driver Station to disconnect", func() bool {
		return allianceStation.DsConn == nil
	})
	redEthernets, _ = arena.Plc.GetEthernetConnected()
	assert.False(t, redEthernets[0])
	assert.Equal(t, dssim.ConnectionOff, arena.SimRobots.Statuses()[0].Connection)
}

func TestSimRobotsInSimulatedMatch(t *testing.T) {
	arena := setupSimRobotArena(t)
	arena.Database.CreateTeam(&model.Team{Id: 1503})
	arena.Database.CreateTeam(&model.Team{Id: 254})
	arena.Database.CreateTeam(&model.Team{Id: 9999})

	// The simulator's test matches use the event's teams.
	assert.Nil(t, arena.UseEventTeamsInSimMatches())
	assert.Equal(t, 254, arena.simStationTeamId("R1"))
	assert.Equal(t, 1503, arena.simStationTeamId("R2"))
	assert.Equal(t, 9999, arena.simStationTeamId("R3"))
	assert.Equal(t, 0, arena.simStationTeamId("B1"))
	assert.True(t, arena.GetFieldSimStatus().SimTeams)

	assert.Nil(t, arena.SetSimRobotState("all", dssim.CodeRunning))
	waitFor(t, "robots to link", func() bool {
		return arena.AllianceStations["R1"].simRobotLinked() && arena.AllianceStations["R2"].simRobotLinked() &&
			arena.AllianceStations["R3"].simRobotLinked()
	})
	arena.Update()
	status := arena.GetFieldSimStatus()
	assert.True(t, strings.HasSuffix(status.Stations[0].Sign.RearText, "Ready"), status.Stations[0].Sign.RearText)
	assert.Equal(t, dssim.ConnectionConnected, status.Robots[0].Connection)
	assert.Equal(t, dssim.ConnectionOff, status.Robots[3].Connection)

	// Starting a simulated match only bypasses the stations without a linked robot.
	assert.Nil(t, arena.StartHubSimMatch())
	assert.False(t, arena.AllianceStations["R1"].Bypass)
	assert.True(t, arena.AllianceStations["B1"].Bypass)
	arena.Update()
	assert.Equal(t, AutoPeriod, arena.MatchState)
	waitFor(t, "R1 robot to be enabled", func() bool {
		arena.sendDsPacket(true, true)
		return arena.SimRobots.Statuses()[0].Enabled
	})

	// The teams carry over to the next simulated test match.
	assert.Nil(t, arena.AbortHubSimMatch())
	arena.Update()
	assert.Nil(t, arena.ResetHubSimMatch())
	assert.Equal(t, 254, arena.simStationTeamId("R1"))
	assert.Nil(t, arena.ClearSimMatchTeams())
	assert.Equal(t, 0, arena.simStationTeamId("R1"))
	assert.False(t, arena.GetFieldSimStatus().SimTeams)
}

func TestUseEventTeamsInSimMatchesErrors(t *testing.T) {
	arena := setupTestArena(t)
	assert.NotNil(t, arena.UseEventTeamsInSimMatches())
	arena.SimControlsEnabled = true
	err := arena.UseEventTeamsInSimMatches()
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "no teams")
	}
	arena.Database.CreateTeam(&model.Team{Id: 254})
	arena.CurrentMatch = &model.Match{Type: model.Qualification}
	err = arena.UseEventTeamsInSimMatches()
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "test match")
	}
	assert.NotNil(t, arena.SetSimRobotState("R1", dssim.CodeRunning))
}
