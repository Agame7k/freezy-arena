// Copyright 2026 Team 254. All Rights Reserved.
//
// Point-in-time health of each alliance station for the FTA: a pre-match readiness checklist, flags for problems that
// are happening right now, and structured reasons why the match can't be started.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"sort"
	"strings"
)

const (
	// A robot should be on a fresh battery before the match; this is advisory and doesn't block the match start.
	ftaPreMatchBatteryVolts = 12.0
	// During a match, a battery below this is sagging hard and worth watching.
	FtaLowBatteryVolts = 7.5
	// Below this the roboRIO starts browning out and disabling outputs.
	FtaBrownoutVolts = 6.8
	// Battery must recover this far above a threshold before another dip is logged, to avoid log spam.
	ftaBatteryHysteresisVolts = 0.5
	// Round-trip times at or above this are flagged, and must drop below the clear threshold before being re-flagged.
	FtaHighTripTimeMs      = 20
	ftaHighTripTimeClearMs = 15
)

// A single item on a station's pre-match readiness checklist.
type FtaCheck struct {
	Name string
	Ok   bool
	// Advisory checks are shown to the FTA but don't prevent the match from starting.
	Advisory bool
	Detail   string
}

// A problem with a station that is happening right now.
type FtaFlag struct {
	Type     model.FtaEventType
	Severity model.FtaSeverity
	Message  string
}

type FtaStationStatus struct {
	Checks []FtaCheck
	// True if every non-advisory check passes (or the station is empty or bypassed).
	Ready bool
	Flags []FtaFlag
}

// A reason the match can't be started, along with which stations are responsible and how to fix it.
type StartMatchBlocker struct {
	Code     string
	Message  string
	Stations []string
	Hint     string
}

// Returns the readiness checklist and live flags for the given station. The Ethernet check is only included when the PLC
// is enabled, since that is the only way to know whether the driver station cable is plugged in.
func getFtaStationStatus(allianceStation *AllianceStation, matchRunning, plcEnabled bool) FtaStationStatus {
	status := FtaStationStatus{Checks: []FtaCheck{}, Flags: []FtaFlag{}, Ready: true}
	if allianceStation.Team == nil {
		return status
	}
	dsConn := allianceStation.DsConn

	if allianceStation.EStop {
		status.Flags = append(
			status.Flags, FtaFlag{model.FtaEventEStop, model.FtaSeverityBad, "E-stopped"},
		)
	}
	if allianceStation.AStop {
		status.Flags = append(
			status.Flags, FtaFlag{model.FtaEventAStop, model.FtaSeverityWarn, "A-stopped"},
		)
	}
	if allianceStation.Bypass {
		status.Flags = append(
			status.Flags, FtaFlag{model.FtaEventBypass, model.FtaSeverityWarn, "Bypassed"},
		)
	}
	if dsConn != nil && dsConn.WrongStation != "" {
		status.Flags = append(
			status.Flags,
			FtaFlag{
				model.FtaEventWrongStation,
				model.FtaSeverityBad,
				fmt.Sprintf("Wrong station (belongs in %s)", dsConn.WrongStation),
			},
		)
	}

	if matchRunning && !allianceStation.Bypass {
		if dsConn == nil || !dsConn.DsLinked {
			status.Flags = append(
				status.Flags, FtaFlag{model.FtaEventDsLost, model.FtaSeverityBad, "No driver station"},
			)
		} else if !dsConn.RobotLinked {
			status.Flags = append(
				status.Flags, FtaFlag{model.FtaEventRobotLost, model.FtaSeverityBad, robotLostMessage(dsConn)},
			)
		}
	}
	if dsConn != nil && dsConn.RobotLinked && dsConn.BatteryVoltage > 0 {
		voltage := dsConn.BatteryVoltage
		if matchRunning {
			if voltage < FtaBrownoutVolts {
				status.Flags = append(
					status.Flags,
					FtaFlag{model.FtaEventBrownout, model.FtaSeverityBad, fmt.Sprintf("Brownout %.1fV", voltage)},
				)
			} else if voltage < FtaLowBatteryVolts {
				status.Flags = append(
					status.Flags,
					FtaFlag{model.FtaEventLowBattery, model.FtaSeverityWarn, fmt.Sprintf("Low battery %.1fV", voltage)},
				)
			}
		} else if voltage < ftaPreMatchBatteryVolts {
			status.Flags = append(
				status.Flags,
				FtaFlag{
					model.FtaEventLowBattery, model.FtaSeverityWarn, fmt.Sprintf("Battery only %.1fV", voltage),
				},
			)
		}
	}
	if dsConn != nil && dsConn.RobotLinked && dsConn.DsRobotTripTimeMs >= FtaHighTripTimeMs {
		status.Flags = append(
			status.Flags,
			FtaFlag{
				model.FtaEventHighTripTime,
				model.FtaSeverityWarn,
				fmt.Sprintf("Trip time %dms", dsConn.DsRobotTripTimeMs),
			},
		)
	}

	if allianceStation.Bypass {
		return status
	}

	// Build the pre-match checklist, in the order the connection is normally established.
	dsLinked := dsConn != nil && dsConn.DsLinked
	radioLinked := dsConn != nil && dsConn.RadioLinked
	rioLinked := dsConn != nil && dsConn.RioLinked
	robotLinked := dsConn != nil && dsConn.RobotLinked
	correctStation := dsConn == nil || dsConn.WrongStation == ""
	batteryDetail := "Not reporting"
	batteryOk := false
	if robotLinked && dsConn.BatteryVoltage > 0 {
		batteryDetail = fmt.Sprintf("%.1fV", dsConn.BatteryVoltage)
		batteryOk = dsConn.BatteryVoltage >= ftaPreMatchBatteryVolts
	}
	stationDetail := ""
	if !correctStation {
		stationDetail = "Move to " + dsConn.WrongStation
	}
	if plcEnabled {
		status.Checks = append(
			status.Checks,
			FtaCheck{Name: "Ethernet", Ok: allianceStation.Ethernet, Advisory: true, Detail: "DS cable plugged in"},
		)
	}
	status.Checks = append(
		status.Checks,
		FtaCheck{Name: "Station", Ok: correctStation, Detail: stationDetail},
		FtaCheck{Name: "Driver station", Ok: dsLinked},
		FtaCheck{Name: "Radio", Ok: radioLinked},
		FtaCheck{Name: "roboRIO", Ok: rioLinked},
		FtaCheck{Name: "Robot code", Ok: robotLinked},
		FtaCheck{Name: "E-stop clear", Ok: !allianceStation.EStop},
		FtaCheck{Name: "A-stop reset", Ok: allianceStation.aStopReset},
		FtaCheck{Name: "Battery", Ok: batteryOk, Advisory: true, Detail: batteryDetail},
	)
	for _, check := range status.Checks {
		if !check.Ok && !check.Advisory {
			status.Ready = false
		}
	}
	return status
}

// Describes a lost robot link along with the most likely cause, based on how far down the chain the link survives.
func robotLostMessage(dsConn *DriverStationConnection) string {
	switch {
	case dsConn.RioLinked:
		return "No robot code (roboRIO up)"
	case dsConn.RadioLinked:
		return "No roboRIO (radio up)"
	default:
		return "No radio"
	}
}

// Returns the FTA status of every station, keyed by station ID.
func (arena *Arena) getFtaStationStatuses() map[string]FtaStationStatus {
	statuses := make(map[string]FtaStationStatus, len(arena.AllianceStations))
	matchRunning := arena.isMatchRunning()
	plcEnabled := arena.Plc.IsEnabled()
	for station, allianceStation := range arena.AllianceStations {
		statuses[station] = getFtaStationStatus(allianceStation, matchRunning, plcEnabled)
	}
	return statuses
}

// Returns true if robots are in the part of the match where they are expected to be connected and running.
func (arena *Arena) isMatchRunning() bool {
	return arena.MatchState == AutoPeriod || arena.MatchState == PausePeriod || arena.MatchState == TeleopPeriod
}

// Returns all conditions preventing the match from being started, with the stations at fault and how to fix them.
func (arena *Arena) getStartMatchBlockers() []StartMatchBlocker {
	var blockers []StartMatchBlocker
	if arena.MatchState != PreMatch {
		blockers = append(
			blockers,
			StartMatchBlocker{
				Code:    "matchInProgress",
				Message: "a match is still in progress or has results pending",
				Hint:    "Commit or discard the previous match in Match Play, then load the next match.",
			},
		)
	}

	blockers = append(blockers, arena.getAllianceStationStartBlockers("R1", "R2", "R3", "B1", "B2", "B3")...)

	if arena.Plc.IsEnabled() {
		if !arena.Plc.IsHealthy() {
			blockers = append(
				blockers,
				StartMatchBlocker{
					Code:    "plcUnhealthy",
					Message: "PLC is not healthy",
					Hint:    "Check the PLC's power and network connection, and its address in Settings.",
				},
			)
		}
		if arena.Plc.GetFieldEStop() {
			blockers = append(
				blockers,
				StartMatchBlocker{
					Code:    "fieldEStop",
					Message: "field emergency stop is active",
					Hint:    "Release the field E-stop at the scoring table once the field is safe.",
				},
			)
		}
		if !arena.Plc.IsFtaReady() {
			blockers = append(
				blockers,
				StartMatchBlocker{
					Code:    "ftaNotReady",
					Message: "FTA ready switch is not active",
					Hint:    "Turn the FTA ready switch on once the field is clear and robots are set.",
				},
			)
		}
		var disconnectedArmorBlocks []string
		for name, status := range arena.Plc.GetArmorBlockStatuses() {
			if !status {
				disconnectedArmorBlocks = append(disconnectedArmorBlocks, name)
			}
		}
		sort.Strings(disconnectedArmorBlocks)
		for _, name := range disconnectedArmorBlocks {
			blockers = append(
				blockers,
				StartMatchBlocker{
					Code:    "armorBlockDisconnected",
					Message: fmt.Sprintf("PLC ArmorBlock %q is not connected", name),
					Hint:    "Check the ArmorBlock's network cable and power.",
				},
			)
		}
	}

	return blockers
}

func (arena *Arena) getAllianceStationStartBlockers(stations ...string) []StartMatchBlocker {
	var eStoppedStations, aStopNotResetStations, disconnectedStations []string
	for _, station := range stations {
		allianceStation := arena.AllianceStations[station]
		if allianceStation.EStop {
			eStoppedStations = append(eStoppedStations, station)
		}
		if !allianceStation.aStopReset {
			aStopNotResetStations = append(aStopNotResetStations, station)
		}
		if !allianceStation.Bypass {
			if allianceStation.DsConn == nil || !allianceStation.DsConn.RobotLinked {
				disconnectedStations = append(disconnectedStations, station)
			}
		}
	}

	var blockers []StartMatchBlocker
	if len(eStoppedStations) > 0 {
		blockers = append(
			blockers,
			StartMatchBlocker{
				Code:     "eStop",
				Message:  fmt.Sprintf("an emergency stop is active (%s)", strings.Join(eStoppedStations, ", ")),
				Stations: eStoppedStations,
				Hint:     "Release the station E-stop button; it stays latched until the match is over.",
			},
		)
	}
	if len(aStopNotResetStations) > 0 {
		blockers = append(
			blockers,
			StartMatchBlocker{
				Code: "aStopNotReset",
				Message: fmt.Sprintf(
					"an autonomous stop has not been reset since the previous match (%s)",
					strings.Join(aStopNotResetStations, ", "),
				),
				Stations: aStopNotResetStations,
				Hint:     "Have the team press and release the A-stop button to show it works.",
			},
		)
	}
	if len(disconnectedStations) > 0 {
		blockers = append(
			blockers,
			StartMatchBlocker{
				Code: "robotNotConnected",
				Message: fmt.Sprintf(
					"not all robots are connected or bypassed (%s)", strings.Join(disconnectedStations, ", "),
				),
				Stations: disconnectedStations,
				Hint:     "Tap the station to see which link is missing, or bypass it if the team is a no-show.",
			},
		)
	}
	return blockers
}

// Returns descriptions of all conditions preventing the match from being started.
func (arena *Arena) getStartMatchConditions() []string {
	return blockerMessages(arena.getStartMatchBlockers())
}

func (arena *Arena) getAllianceStationStartConditions(stations ...string) []string {
	return blockerMessages(arena.getAllianceStationStartBlockers(stations...))
}

func blockerMessages(blockers []StartMatchBlocker) []string {
	var messages []string
	for _, blocker := range blockers {
		messages = append(messages, blocker.Message)
	}
	return messages
}
