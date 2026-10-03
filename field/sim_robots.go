// Copyright 2026 Team 254. All Rights Reserved.
//
// Simulated robots for the field simulator: simulated Driver Stations (see the dssim package) that connect to the arena
// over the real network protocol, plus the teams to put in simulated test matches.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/dssim"
	"github.com/Team254/cheesy-arena/model"
	"sort"
)

const simRobotArenaAddress = "127.0.0.1"

// startSimRobots creates the simulated Driver Stations, which connect to this arena's driver station ports.
func (arena *Arena) startSimRobots() {
	arena.SimRobots = dssim.NewManager(
		simRobotArenaAddress, driverStationTcpListenPort, driverStationUdpReceivePort, arena.simStationTeamId,
	)
}

// simStationTeamId returns the team assigned to the given station, or zero if there is none.
func (arena *Arena) simStationTeamId(station string) int {
	if allianceStation, ok := arena.AllianceStations[station]; ok && allianceStation.Team != nil {
		return allianceStation.Team.Id
	}
	return 0
}

// SetSimRobotState sets how far the simulated robot at the given station comes up ("all" for every station). Any
// state other than off also plugs the station's Ethernet in on the simulated PLC.
func (arena *Arena) SetSimRobotState(station string, state dssim.RobotState) error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	if arena.SimRobots == nil {
		return fmt.Errorf("simulated robots aren't running")
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	stations := []string{station}
	if station == "all" {
		stations = fieldSimStationIds[:]
	}
	for _, stationId := range stations {
		if err := arena.SimRobots.SetState(stationId, state); err != nil {
			return err
		}
		if simPlc, err := arena.simPlc(); err == nil {
			if err = simPlc.SetEthernetConnected(stationId, state != dssim.RobotOff); err != nil {
				return err
			}
		}
	}
	return nil
}

// UseEventTeamsInSimMatches puts the first six teams at the event into the loaded test match, and into every test
// match the simulator loads from now on, so that simulated robots have teams to connect as.
func (arena *Arena) UseEventTeamsInSimMatches() error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	if arena.MatchState != PreMatch || arena.CurrentMatch.Type != model.Test {
		return fmt.Errorf("teams can only be put into a test match before it starts (press Reset first)")
	}
	teams, err := arena.Database.GetAllTeams()
	if err != nil {
		return err
	}
	if len(teams) == 0 {
		return fmt.Errorf("there are no teams at this event yet; add some on the Teams page")
	}
	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Id < teams[j].Id
	})
	var teamIds [6]int
	for i := 0; i < len(teamIds) && i < len(teams); i++ {
		teamIds[i] = teams[i].Id
	}
	arena.simTestTeams = teamIds
	return arena.applySimTestTeams()
}

// ClearSimMatchTeams stops putting teams into simulated test matches and empties the loaded test match.
func (arena *Arena) ClearSimMatchTeams() error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	arena.simTestTeams = [6]int{}
	if arena.MatchState != PreMatch || arena.CurrentMatch.Type != model.Test {
		return nil
	}
	return arena.applySimTestTeams()
}

// applySimTestTeams puts the simulator's chosen teams into the loaded test match.
func (arena *Arena) applySimTestTeams() error {
	if arena.CurrentMatch.Type != model.Test {
		return nil
	}
	teams := arena.simTestTeams
	return arena.SubstituteTeams(teams[0], teams[1], teams[2], teams[3], teams[4], teams[5])
}

// simRobotLinked returns true if the station's robot is linked, so that a simulated match doesn't bypass it.
func (allianceStation *AllianceStation) simRobotLinked() bool {
	return allianceStation.DsConn != nil && allianceStation.DsConn.RobotLinked
}
