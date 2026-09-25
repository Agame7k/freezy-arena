// Copyright 2026 Team 254. All Rights Reserved.
//
// Fakes a driver station connection for every team in the match, so that the field monitor, FTA console, and match
// logs can be exercised without real robots. Robots boot up link by link after their match is loaded, and during a
// match they drain their batteries and occasionally lose their radio, brown out, or spike their trip times. Enabled
// with the -simulate flag.

package field

import (
	"log"
	"math/rand"
	"time"
)

// How a simulated robot tends to behave. Assigned by team number so that the same teams are consistently troublesome
// across matches, which is what the FTA console's history and watchlist are built to catch.
type simulatedProfile int

const (
	simulatedHealthy simulatedProfile = iota
	simulatedFlakyRadio
	simulatedWeakBattery
	simulatedSlowBoot
)

// Chance per second of each kind of trouble starting while a match is running.
var simulatedOutageRates = map[simulatedProfile]float64{
	simulatedHealthy: 0.0008, simulatedFlakyRadio: 0.012, simulatedWeakBattery: 0.001, simulatedSlowBoot: 0.001,
}
var simulatedSagRates = map[simulatedProfile]float64{
	simulatedHealthy: 0.002, simulatedFlakyRadio: 0.002, simulatedWeakBattery: 0.03, simulatedSlowBoot: 0.002,
}
var simulatedTripSpikeRates = map[simulatedProfile]float64{
	simulatedHealthy: 0.0005, simulatedFlakyRadio: 0.015, simulatedWeakBattery: 0.0005, simulatedSlowBoot: 0.0005,
}

// Connection levels, from nothing up to robot code running, matching the order the chain comes up in.
const (
	simulatedLevelNone = iota
	simulatedLevelDs
	simulatedLevelRadio
	simulatedLevelRio
	simulatedLevelCode
)

type simulatedRobot struct {
	teamId   int
	profile  simulatedProfile
	loadedAt time.Time
	// Seconds after the team is loaded at which each link in the chain comes up.
	linkUpSec [simulatedLevelCode + 1]float64
	// Voltage of the battery at rest when the match starts.
	restingVolts float64
	snr          int

	outageUntil    time.Time
	outageLevel    int
	sagUntil       time.Time
	sagVolts       float64
	tripSpikeUntil time.Time
	tripSpikeMs    int
}

type robotSimulator struct {
	robots     map[string]*simulatedRobot
	rng        *rand.Rand
	lastUpdate time.Time
}

func newRobotSimulator(seed int64) *robotSimulator {
	return &robotSimulator{robots: make(map[string]*simulatedRobot), rng: rand.New(rand.NewSource(seed))}
}

// Turns on robot simulation. Without a PLC the station E-stop inputs start out active, so they are released here so
// that matches can be started.
func (arena *Arena) EnableRobotSimulator() {
	arena.robotSimulator = newRobotSimulator(time.Now().UnixNano())
	if !arena.Plc.IsEnabled() {
		arena.Plc.ResetEstops()
	}
	log.Println("Simulating robot connections for every team in the match.")
}

func simulatedProfileForTeam(teamId int) simulatedProfile {
	switch teamId % 10 {
	case 1, 7:
		return simulatedFlakyRadio
	case 2:
		return simulatedWeakBattery
	case 3:
		return simulatedSlowBoot
	default:
		return simulatedHealthy
	}
}

func (simulator *robotSimulator) newRobot(teamId int, now time.Time) *simulatedRobot {
	robot := &simulatedRobot{
		teamId:       teamId,
		profile:      simulatedProfileForTeam(teamId),
		loadedAt:     now,
		restingVolts: 12.4 + simulator.rng.Float64()*0.6,
		snr:          34 + simulator.rng.Intn(12),
	}
	radioBootSec := 2 + simulator.rng.Float64()*4
	switch robot.profile {
	case simulatedSlowBoot:
		radioBootSec = 15 + simulator.rng.Float64()*20
	case simulatedWeakBattery:
		robot.restingVolts = 11.6 + simulator.rng.Float64()*0.5
	case simulatedFlakyRadio:
		robot.snr = 14 + simulator.rng.Intn(10)
	}
	robot.linkUpSec[simulatedLevelDs] = 0.5 + simulator.rng.Float64()*1.5
	robot.linkUpSec[simulatedLevelRadio] = robot.linkUpSec[simulatedLevelDs] + radioBootSec
	robot.linkUpSec[simulatedLevelRio] = robot.linkUpSec[simulatedLevelRadio] + 1 + simulator.rng.Float64()*3
	robot.linkUpSec[simulatedLevelCode] = robot.linkUpSec[simulatedLevelRio] + 1 + simulator.rng.Float64()*2
	return robot
}

// Returns true with the given chance per second, scaled to the time since the last update.
func (simulator *robotSimulator) happens(ratePerSec, elapsedSec float64) bool {
	return simulator.rng.Float64() < ratePerSec*elapsedSec
}

func (simulator *robotSimulator) randomBetween(low, high float64) float64 {
	return low + simulator.rng.Float64()*(high-low)
}

// Advances every simulated robot to the given time and writes its state into the station's driver station connection.
func (simulator *robotSimulator) update(arena *Arena, now time.Time) {
	elapsedSec := 0.0
	if !simulator.lastUpdate.IsZero() {
		elapsedSec = now.Sub(simulator.lastUpdate).Seconds()
	}
	simulator.lastUpdate = now
	matchRunning := arena.isMatchRunning()
	matchTimeSec := arena.MatchTimeSec()

	for station, allianceStation := range arena.AllianceStations {
		if allianceStation.Team == nil {
			delete(simulator.robots, station)
			continue
		}
		teamId := allianceStation.Team.Id
		robot := simulator.robots[station]
		dsConn := allianceStation.DsConn
		if robot == nil || robot.teamId != teamId {
			robot = simulator.newRobot(teamId, now)
			simulator.robots[station] = robot
		}
		if dsConn == nil || dsConn.TeamId != teamId {
			dsConn = &DriverStationConnection{TeamId: teamId, AllianceStation: station}
			allianceStation.DsConn = dsConn
		}

		level := simulatedLevelNone
		for linkLevel := simulatedLevelDs; linkLevel <= simulatedLevelCode; linkLevel++ {
			if now.Sub(robot.loadedAt).Seconds() >= robot.linkUpSec[linkLevel] {
				level = linkLevel
			}
		}

		if matchRunning && level == simulatedLevelCode {
			if now.After(robot.outageUntil) && simulator.happens(simulatedOutageRates[robot.profile], elapsedSec) {
				// Most outages are the radio dropping, then code crashing, then the roboRIO rebooting.
				roll := simulator.rng.Float64()
				robot.outageLevel = simulatedLevelDs
				if roll > 0.85 {
					robot.outageLevel = simulatedLevelRadio
				} else if roll > 0.6 {
					robot.outageLevel = simulatedLevelRio
				}
				robot.outageUntil = now.Add(time.Duration(simulator.randomBetween(2, 12) * float64(time.Second)))
			}
			if now.After(robot.sagUntil) && simulator.happens(simulatedSagRates[robot.profile], elapsedSec) {
				robot.sagVolts = simulator.randomBetween(7.0, 8.0)
				if robot.profile == simulatedWeakBattery {
					robot.sagVolts = simulator.randomBetween(6.2, 7.3)
				}
				robot.sagUntil = now.Add(time.Duration(simulator.randomBetween(0.3, 1) * float64(time.Second)))
			}
			if now.After(robot.tripSpikeUntil) && simulator.happens(simulatedTripSpikeRates[robot.profile], elapsedSec) {
				robot.tripSpikeMs = 25 + simulator.rng.Intn(35)
				robot.tripSpikeUntil = now.Add(time.Duration(simulator.randomBetween(3, 6) * float64(time.Second)))
			}
		} else if !matchRunning {
			robot.outageUntil = time.Time{}
			robot.sagUntil = time.Time{}
			robot.tripSpikeUntil = time.Time{}
		}
		if now.Before(robot.outageUntil) {
			level = min(level, robot.outageLevel)
		}

		// Pretend a packet was just received, so that the connection doesn't time out between loops.
		dsConn.lastPacketTime = now
		dsConn.DsLinked = level >= simulatedLevelDs
		dsConn.RadioLinked = level >= simulatedLevelRadio
		dsConn.RioLinked = level >= simulatedLevelRio
		dsConn.RobotLinked = level >= simulatedLevelCode
		if dsConn.RobotLinked {
			dsConn.lastRobotLinkedTime = now
		}
		dsConn.DsReportedStatusValid = dsConn.RobotLinked
		dsConn.DsReportedAuto = dsConn.Auto
		dsConn.DsReportedTeleop = !dsConn.Auto
		dsConn.DsReportedEnabled = dsConn.RobotLinked && dsConn.Enabled
		dsConn.DsReportedDisabled = !dsConn.DsReportedEnabled

		dsConn.BatteryVoltage = 0
		dsConn.DsRobotTripTimeMs = 0
		if dsConn.RioLinked {
			voltage := robot.restingVolts
			if matchRunning {
				drainPerSec := 1.0 / 150
				if robot.profile == simulatedWeakBattery {
					drainPerSec = 2.0 / 150
				}
				voltage -= drainPerSec * matchTimeSec
				if dsConn.Enabled {
					voltage -= simulator.randomBetween(0.3, 1.2)
				}
			}
			if now.Before(robot.sagUntil) {
				voltage = min(voltage, robot.sagVolts)
			}
			dsConn.BatteryVoltage = voltage
			dsConn.DsRobotTripTimeMs = 3 + simulator.rng.Intn(5)
			if now.Before(robot.tripSpikeUntil) {
				dsConn.DsRobotTripTimeMs = robot.tripSpikeMs + simulator.rng.Intn(5)
			}
		}
		if matchRunning && dsConn.DsLinked && (!dsConn.RobotLinked || now.Before(robot.tripSpikeUntil)) {
			// Packets go missing while the robot is unreachable or the link is congested.
			if simulator.rng.Intn(2) == 0 {
				dsConn.MissedPacketCount++
			}
		}

		allianceStation.WifiStatus.TeamId = teamId
		allianceStation.WifiStatus.RadioLinked = dsConn.RadioLinked
		allianceStation.WifiStatus.SignalNoiseRatio = 0
		allianceStation.WifiStatus.MBits = 0
		if dsConn.RadioLinked {
			allianceStation.WifiStatus.SignalNoiseRatio = robot.snr + simulator.rng.Intn(3) - 1
			allianceStation.WifiStatus.MBits = simulator.randomBetween(0.1, 0.3)
			if dsConn.Enabled {
				allianceStation.WifiStatus.MBits = simulator.randomBetween(1.5, 3.5)
			}
		}
	}
}

// Runs the robot simulator for one arena loop, if it's enabled.
func (arena *Arena) updateRobotSimulator() {
	if arena.robotSimulator != nil {
		arena.robotSimulator.update(arena, time.Now())
	}
}
