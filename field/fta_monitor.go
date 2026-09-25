// Copyright 2026 Team 254. All Rights Reserved.
//
// Watches every alliance station on each arena loop and records faults (disconnects, E-stops, brownouts, etc.) as
// they happen, so that the FTA has a persistent log regardless of whether anyone was watching a screen at the time.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"log"
	"time"
)

const (
	// Trip time must stay high for this long before it is logged, so that single spikes are ignored.
	ftaHighTripTimeSustainSec = 2
	// Missed packets are counted over a sliding window; more than this many in the window is logged as packet loss.
	ftaPacketLossWindowSec   = 1
	ftaPacketLossThreshold   = 10
	ftaPacketLossCooldownSec = 5
	// The arena zeroes the missed packet count at match start, but the driver station's next report restores its running
	// total; ignore the jump by only re-baselining for this long after the match starts.
	ftaPacketLossSettleSec = 2
	ftaRecordQueueSize     = 64
)

// How many times each kind of recurring fault is logged per station per match. A robot that keeps browning out would
// otherwise flood the log; later repeats are still counted in the match stats, just not logged or pushed to the FTA.
var ftaEventLimitsPerMatch = map[model.FtaEventType]int{
	model.FtaEventBrownout:     1,
	model.FtaEventLowBattery:   1,
	model.FtaEventHighTripTime: 1,
	model.FtaEventPacketLoss:   1,
	model.FtaEventRobotLost:    3,
	model.FtaEventDsLost:       3,
}

// Restored events are only logged if the matching lost event was, so the log never has an unpaired "restored".
var ftaRestoredEventCauses = map[model.FtaEventType]model.FtaEventType{
	model.FtaEventRobotRestored: model.FtaEventRobotLost,
	model.FtaEventDsRestored:    model.FtaEventDsLost,
}

type ftaStationState struct {
	teamId         int
	dsLinked       bool
	robotLinked    bool
	eStop          bool
	aStop          bool
	bypass         bool
	wrongStation   string
	lowBattery     bool
	brownout       bool
	highTripSince  time.Time
	highTripLogged bool
	robotLostAt    time.Time
	dsLostAt       time.Time

	packetWindowStart     time.Time
	packetSettleUntil     time.Time
	packetWindowBaseCount int
	lastPacketLossEvent   time.Time

	// Number of times each kind of fault has happened this match, including repeats that weren't logged.
	occurrences map[model.FtaEventType]int

	stats *model.FtaTeamMatchStats
}

type ftaMonitor struct {
	stations        map[string]*ftaStationState
	fieldEStop      bool
	fieldEStopKnown bool
	matchRunning    bool
	lastUpdateTime  time.Time
}

func newFtaMonitor() *ftaMonitor {
	return &ftaMonitor{stations: make(map[string]*ftaStationState)}
}

// Compares the current state of the arena with the previous call and returns the events that occurred in between,
// as well as per-team stats for the match if it just ended.
func (monitor *ftaMonitor) update(
	arena *Arena, now time.Time,
) ([]model.FtaEvent, []model.FtaTeamMatchStats) {
	var events []model.FtaEvent
	var finishedStats []model.FtaTeamMatchStats
	matchRunning := arena.isMatchRunning()
	matchStarted := matchRunning && !monitor.matchRunning
	matchEnded := !matchRunning && monitor.matchRunning
	elapsedSec := 0.0
	if !monitor.lastUpdateTime.IsZero() {
		elapsedSec = now.Sub(monitor.lastUpdateTime).Seconds()
	}
	monitor.lastUpdateTime = now
	monitor.matchRunning = matchRunning

	newEvent := func(station string, teamId int, eventType model.FtaEventType, severity model.FtaSeverity,
		message string) {
		event := model.FtaEvent{
			TeamId:       teamId,
			Station:      station,
			MatchTimeSec: arena.MatchTimeSec(),
			Time:         now,
			Type:         eventType,
			Severity:     severity,
			Message:      message,
		}
		if arena.CurrentMatch != nil {
			event.MatchId = arena.CurrentMatch.Id
			event.MatchType = arena.CurrentMatch.Type
			event.MatchShortName = arena.CurrentMatch.ShortName
		}
		if state, ok := monitor.stations[station]; ok && monitor.matchRunning {
			cause, isRestored := ftaRestoredEventCauses[eventType]
			if isRestored && state.occurrences[cause] > ftaEventLimitsPerMatch[cause] {
				return
			}
			if severity != model.FtaSeverityGood {
				state.occurrences[eventType]++
				if state.stats != nil {
					state.stats.FaultCount++
					switch eventType {
					case model.FtaEventBrownout:
						state.stats.BrownoutCount++
					case model.FtaEventRobotLost:
						state.stats.RobotDropCount++
					case model.FtaEventDsLost:
						state.stats.DsDropCount++
					}
				}
				if limit, ok := ftaEventLimitsPerMatch[eventType]; ok {
					if state.occurrences[eventType] > limit {
						return
					}
					if state.occurrences[eventType] == limit {
						event.Message += " (repeats this match won't be logged)"
					}
				}
			}
		}
		events = append(events, event)
	}

	if arena.Plc.IsEnabled() {
		fieldEStop := arena.Plc.GetFieldEStop()
		if monitor.fieldEStopKnown && fieldEStop && !monitor.fieldEStop {
			newEvent("", 0, model.FtaEventFieldEStop, model.FtaSeverityBad, "Field E-stop activated")
		}
		monitor.fieldEStop = fieldEStop
		monitor.fieldEStopKnown = true
	}

	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		allianceStation, ok := arena.AllianceStations[station]
		if !ok {
			continue
		}
		teamId := 0
		if allianceStation.Team != nil {
			teamId = allianceStation.Team.Id
		}
		dsConn := allianceStation.DsConn
		current := ftaStationState{
			teamId:       teamId,
			dsLinked:     dsConn != nil && dsConn.DsLinked,
			robotLinked:  dsConn != nil && dsConn.RobotLinked,
			eStop:        allianceStation.EStop,
			aStop:        allianceStation.AStop,
			bypass:       allianceStation.Bypass,
			wrongStation: "",
			occurrences:  make(map[model.FtaEventType]int),
		}
		if dsConn != nil {
			current.wrongStation = dsConn.WrongStation
		}

		previous, known := monitor.stations[station]
		if !known || previous.teamId != teamId || teamId == 0 {
			// The first observation of a team in a station establishes a baseline; it isn't a change.
			monitor.stations[station] = &current
			if matchRunning && teamId != 0 {
				current.stats = newFtaTeamMatchStats(arena, station, teamId)
			}
			continue
		}
		state := previous

		if matchStarted {
			state.stats = newFtaTeamMatchStats(arena, station, teamId)
			state.lowBattery = false
			state.occurrences = make(map[model.FtaEventType]int)
			state.brownout = false
			state.highTripSince = time.Time{}
			state.highTripLogged = false
			state.robotLostAt = time.Time{}
			state.dsLostAt = time.Time{}
			state.packetWindowStart = time.Time{}
			state.packetSettleUntil = now.Add(ftaPacketLossSettleSec * time.Second)
		}

		// E-stops, A-stops, bypasses, and wrong stations matter any time.
		if current.eStop && !state.eStop {
			newEvent(station, teamId, model.FtaEventEStop, model.FtaSeverityBad, "E-stop pressed")
		}
		if current.aStop && !state.aStop {
			newEvent(station, teamId, model.FtaEventAStop, model.FtaSeverityWarn, "A-stop pressed")
		}
		if current.bypass && !state.bypass {
			newEvent(station, teamId, model.FtaEventBypass, model.FtaSeverityWarn, "Station bypassed")
		}
		if current.wrongStation != "" && current.wrongStation != state.wrongStation {
			newEvent(
				station,
				teamId,
				model.FtaEventWrongStation,
				model.FtaSeverityBad,
				fmt.Sprintf("Driver station in wrong station (belongs in %s)", current.wrongStation),
			)
		}

		// Connection churn only matters once robots are expected to be running.
		if matchRunning && !current.bypass {
			if state.dsLinked && !current.dsLinked {
				state.dsLostAt = now
				newEvent(station, teamId, model.FtaEventDsLost, model.FtaSeverityBad, "Driver station disconnected")
			} else if !state.dsLinked && current.dsLinked && !state.dsLostAt.IsZero() {
				newEvent(
					station,
					teamId,
					model.FtaEventDsRestored,
					model.FtaSeverityGood,
					fmt.Sprintf("Driver station reconnected (down %.0fs)", now.Sub(state.dsLostAt).Seconds()),
				)
				state.dsLostAt = time.Time{}
			}

			if state.robotLinked && !current.robotLinked {
				state.robotLostAt = now
				message := "Robot link lost"
				if dsConn != nil && current.dsLinked {
					message = "Robot link lost: " + robotLostMessage(dsConn)
				}
				newEvent(station, teamId, model.FtaEventRobotLost, model.FtaSeverityBad, message)
			} else if !state.robotLinked && current.robotLinked && !state.robotLostAt.IsZero() {
				newEvent(
					station,
					teamId,
					model.FtaEventRobotRestored,
					model.FtaSeverityGood,
					fmt.Sprintf("Robot link restored (down %.0fs)", now.Sub(state.robotLostAt).Seconds()),
				)
				state.robotLostAt = time.Time{}
			}

			if current.robotLinked && dsConn.BatteryVoltage > 0 {
				voltage := dsConn.BatteryVoltage
				if !state.brownout && voltage < FtaBrownoutVolts {
					state.brownout = true
					state.lowBattery = true
					newEvent(
						station,
						teamId,
						model.FtaEventBrownout,
						model.FtaSeverityBad,
						fmt.Sprintf("Brownout: battery at %.1fV", voltage),
					)
				} else if !state.lowBattery && voltage < FtaLowBatteryVolts {
					state.lowBattery = true
					newEvent(
						station,
						teamId,
						model.FtaEventLowBattery,
						model.FtaSeverityWarn,
						fmt.Sprintf("Low battery %.1fV", voltage),
					)
				}
				if voltage >= FtaBrownoutVolts+ftaBatteryHysteresisVolts {
					state.brownout = false
				}
				if voltage >= FtaLowBatteryVolts+ftaBatteryHysteresisVolts {
					state.lowBattery = false
				}
			}

			if current.robotLinked && dsConn.DsRobotTripTimeMs >= FtaHighTripTimeMs {
				if state.highTripSince.IsZero() {
					state.highTripSince = now
				}
				if !state.highTripLogged && now.Sub(state.highTripSince).Seconds() >= ftaHighTripTimeSustainSec {
					state.highTripLogged = true
					newEvent(
						station,
						teamId,
						model.FtaEventHighTripTime,
						model.FtaSeverityWarn,
						fmt.Sprintf("High trip time %dms", dsConn.DsRobotTripTimeMs),
					)
				}
			} else if dsConn == nil || dsConn.DsRobotTripTimeMs < ftaHighTripTimeClearMs {
				state.highTripSince = time.Time{}
				state.highTripLogged = false
			}

			if dsConn != nil {
				missedPackets := dsConn.MissedPacketCount
				if state.packetWindowStart.IsZero() || now.Before(state.packetSettleUntil) ||
					missedPackets < state.packetWindowBaseCount ||
					now.Sub(state.packetWindowStart).Seconds() >= ftaPacketLossWindowSec {
					state.packetWindowStart = now
					state.packetWindowBaseCount = missedPackets
				} else if missedPackets-state.packetWindowBaseCount >= ftaPacketLossThreshold &&
					now.Sub(state.lastPacketLossEvent).Seconds() >= ftaPacketLossCooldownSec {
					state.lastPacketLossEvent = now
					newEvent(
						station,
						teamId,
						model.FtaEventPacketLoss,
						model.FtaSeverityWarn,
						fmt.Sprintf("Packet loss (%d missed in %ds)", missedPackets-state.packetWindowBaseCount,
							ftaPacketLossWindowSec),
					)
					state.packetWindowStart = now
					state.packetWindowBaseCount = missedPackets
				}
			}
		} else if !matchRunning {
			state.robotLostAt = time.Time{}
			state.dsLostAt = time.Time{}
		}

		if matchRunning && state.stats != nil && !current.bypass {
			accumulateFtaStats(state.stats, dsConn, current, elapsedSec)
		}

		state.dsLinked = current.dsLinked
		state.robotLinked = current.robotLinked
		state.eStop = current.eStop
		state.aStop = current.aStop
		state.bypass = current.bypass
		state.wrongStation = current.wrongStation
	}

	if matchEnded {
		for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
			if state, ok := monitor.stations[station]; ok && state.stats != nil {
				state.stats.Time = now
				finishedStats = append(finishedStats, *state.stats)
				state.stats = nil
			}
		}
	}

	return events, finishedStats
}

func newFtaTeamMatchStats(arena *Arena, station string, teamId int) *model.FtaTeamMatchStats {
	stats := model.FtaTeamMatchStats{TeamId: teamId, Station: station}
	if arena.CurrentMatch != nil {
		stats.MatchId = arena.CurrentMatch.Id
		stats.MatchType = arena.CurrentMatch.Type
		stats.MatchShortName = arena.CurrentMatch.ShortName
	}
	return &stats
}

// Folds one loop iteration's worth of station state into the running stats for the match.
func accumulateFtaStats(
	stats *model.FtaTeamMatchStats, dsConn *DriverStationConnection, current ftaStationState, elapsedSec float64,
) {
	if !current.dsLinked {
		stats.DsDownSec += elapsedSec
	}
	if !current.robotLinked {
		stats.RobotDownSec += elapsedSec
		return
	}
	stats.RobotEverLinked = true
	if dsConn.BatteryVoltage > 0 && (stats.MinBatteryVoltage == 0 || dsConn.BatteryVoltage < stats.MinBatteryVoltage) {
		stats.MinBatteryVoltage = dsConn.BatteryVoltage
	}
	if dsConn.DsRobotTripTimeMs > stats.MaxTripTimeMs {
		stats.MaxTripTimeMs = dsConn.DsRobotTripTimeMs
	}
	if dsConn.MissedPacketCount > stats.MissedPackets {
		stats.MissedPackets = dsConn.MissedPacketCount
	}
}

// Runs the FTA monitor for one arena loop and records anything it found.
func (arena *Arena) updateFtaMonitor() {
	events, stats := arena.ftaMonitor.update(arena, time.Now())
	if len(events) == 0 && len(stats) == 0 {
		return
	}
	if arena.ftaRecordQueue == nil {
		arena.recordFtaResults(events, stats)
		return
	}
	// Database writes can take a while, so hand them off rather than stalling the arena loop.
	select {
	case arena.ftaRecordQueue <- ftaRecord{events, stats}:
	default:
		log.Printf("FTA record queue is full; dropping %d events and %d stats.", len(events), len(stats))
	}
}

type ftaRecord struct {
	events []model.FtaEvent
	stats  []model.FtaTeamMatchStats
}

// Persists events and stats in the order they were produced, for as long as the arena runs.
func (arena *Arena) runFtaRecorder() {
	for record := range arena.ftaRecordQueue {
		arena.recordFtaResults(record.events, record.stats)
	}
}

func (arena *Arena) recordFtaResults(events []model.FtaEvent, stats []model.FtaTeamMatchStats) {
	for i := range events {
		if err := arena.Database.CreateFtaEvent(&events[i]); err != nil {
			log.Printf("Failed to save FTA event: %v", err)
		}
		arena.FtaEventNotifier.NotifyWithMessage(events[i])
	}
	for i := range stats {
		if err := arena.Database.CreateFtaTeamMatchStats(&stats[i]); err != nil {
			log.Printf("Failed to save FTA team match stats: %v", err)
		}
	}
}
