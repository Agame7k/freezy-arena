// Copyright 2026 Team 254. All Rights Reserved.
//
// Keeps a simulated Driver Station running at each alliance station for whichever team the arena has assigned there.

package dssim

import (
	"fmt"
	"sync"
	"time"
)

const syncPeriod = 250 * time.Millisecond

// Manager runs one simulated Driver Station per alliance station. The robot state is chosen per station and carries
// over from match to match; the team is whichever one the arena has assigned to the station, so loading a new match
// brings up that match's teams.
type Manager struct {
	address      string
	tcpPort      int
	udpPort      int
	stationTeams func(station string) int
	mutex        sync.Mutex
	states       map[string]RobotState
	stations     map[string]*driverStation
	done         chan struct{}
}

// NewManager creates a manager that connects to the arena's driver station ports at the given address, looking up
// station teams with the given function, and starts keeping the Driver Stations in sync.
func NewManager(address string, tcpPort, udpPort int, stationTeams func(station string) int) *Manager {
	manager := &Manager{
		address:      address,
		tcpPort:      tcpPort,
		udpPort:      udpPort,
		stationTeams: stationTeams,
		states:       make(map[string]RobotState),
		stations:     make(map[string]*driverStation),
		done:         make(chan struct{}),
	}
	go func() {
		ticker := time.NewTicker(syncPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-manager.done:
				return
			case <-ticker.C:
				manager.Sync()
			}
		}
	}()
	return manager
}

// SetState sets how far the robot at the given station ("R1" through "B3") comes up.
func (manager *Manager) SetState(station string, state RobotState) error {
	if !isStation(station) {
		return fmt.Errorf("invalid alliance station '%s'", station)
	}
	if _, ok := RobotStateNames[state]; !ok {
		return fmt.Errorf("invalid robot state %d", state)
	}
	manager.mutex.Lock()
	manager.states[station] = state
	manager.mutex.Unlock()
	manager.Sync()
	return nil
}

// GetState returns the robot state chosen for the given station.
func (manager *Manager) GetState(station string) RobotState {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return manager.states[station]
}

// Statuses returns the status of each station's simulated Driver Station, in R1 through B3 order.
func (manager *Manager) Statuses() [6]Status {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	var statuses [6]Status
	for i, station := range stationNames {
		if ds, ok := manager.stations[station]; ok {
			statuses[i] = ds.getStatus()
		} else {
			statuses[i] = Status{Station: station, State: manager.states[station], Connection: ConnectionOff}
		}
	}
	return statuses
}

// Sync starts, stops, or reconnects Driver Stations to match the chosen states and the arena's station teams.
func (manager *Manager) Sync() {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	for _, station := range stationNames {
		state := manager.states[station]
		teamId := manager.stationTeams(station)
		ds, running := manager.stations[station]
		if running && (state == RobotOff || teamId == 0 || ds.teamId != teamId) {
			ds.close()
			delete(manager.stations, station)
			running = false
		}
		if state == RobotOff || teamId == 0 {
			continue
		}
		if running {
			ds.setState(state)
		} else {
			manager.stations[station] = newDriverStation(
				manager.address, manager.tcpPort, manager.udpPort, station, teamId, state,
			)
		}
	}
}

// Close stops all of the Driver Stations and stops keeping them in sync; the manager can't be used afterward.
func (manager *Manager) Close() {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	select {
	case <-manager.done:
	default:
		close(manager.done)
	}
	for station, ds := range manager.stations {
		ds.close()
		delete(manager.stations, station)
	}
	manager.states = make(map[string]RobotState)
}

func isStation(station string) bool {
	for _, name := range stationNames {
		if name == station {
			return true
		}
	}
	return false
}
