// Copyright 2026 Team 254. All Rights Reserved.
//
// Support for the field simulator: what the driver stations, team signs, and PLC outputs are doing, taken from the
// same code that drives the real hardware so that it can be visualized without any of it connected.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/dssim"
	"github.com/Team254/cheesy-arena/game"
	"image/color"
	"strings"
)

var fieldSimStationIds = [6]string{"R1", "R2", "R3", "B1", "B2", "B3"}

// FieldSimSign is what one team number or timer sign is showing. FrontColor is a CSS hex color with the sign's
// intensity applied (black when a blinking sign is in its off phase).
type FieldSimSign struct {
	FrontText  string
	FrontColor string
	RearText   string
}

// FieldSimStation is the state of one driver station.
type FieldSimStation struct {
	Id           string
	TeamId       int
	Bypass       bool
	EStop        bool
	AStop        bool
	EStopPressed bool
	AStopPressed bool
	Ethernet     bool
	DsConnected  bool
	RobotLinked  bool
	Sign         FieldSimSign
}

// FieldSimStatus is everything on the field that the field simulator shows besides the Hubs and the displays. Coils
// holds the PLC coil states as "0"/"1" characters in the order of Plc.GetCoilNames().
type FieldSimStatus struct {
	MatchName                  string
	Stations                   [6]FieldSimStation
	RedTimer                   FieldSimSign
	BlueTimer                  FieldSimSign
	PlcEnabled                 bool
	SimulatedPlc               bool
	FieldEStopPressed          bool
	FtaReady                   bool
	Coils                      string
	RedFuel                    int
	BlueFuel                   int
	RedActiveFuel              int
	BlueActiveFuel             int
	CanStartMatch              bool
	StartConditions            string
	AudienceDisplayMode        string
	AllianceStationDisplayMode string
	Robots                     [6]dssim.Status
	SimTeams                   bool
}

// GetFieldSimStatus returns the current state of the driver stations, signs, and PLC.
func (arena *Arena) GetFieldSimStatus() FieldSimStatus {
	_, simPlcErr := arena.simPlc()
	startConditions := arena.getStartMatchConditions()
	status := FieldSimStatus{
		PlcEnabled:                 arena.Plc.IsEnabled(),
		SimulatedPlc:               simPlcErr == nil,
		FieldEStopPressed:          arena.Plc.GetFieldEStop(),
		FtaReady:                   arena.Plc.IsFtaReady(),
		CanStartMatch:              len(startConditions) == 0,
		StartConditions:            strings.Join(startConditions, "; "),
		AudienceDisplayMode:        arena.AudienceDisplayMode,
		AllianceStationDisplayMode: arena.AllianceStationDisplayMode,
	}
	if arena.CurrentMatch != nil {
		status.MatchName = arena.CurrentMatch.LongName
	}
	if arena.SimRobots != nil {
		status.Robots = arena.SimRobots.Statuses()
	}
	status.SimTeams = arena.simTestTeams != [6]int{}

	var coils strings.Builder
	for _, coil := range arena.Plc.GetAllCoils() {
		if coil {
			coils.WriteByte('1')
		} else {
			coils.WriteByte('0')
		}
	}
	status.Coils = coils.String()

	redHub := &arena.RedRealtimeScore.CurrentScore.Hub
	blueHub := &arena.BlueRealtimeScore.CurrentScore.Hub
	for _, count := range redHub.ShiftCounts {
		status.RedFuel += count
	}
	for _, count := range blueHub.ShiftCounts {
		status.BlueFuel += count
	}
	status.RedActiveFuel = redHub.GetShiftCount(game.ShiftAuto, true) + redHub.GetTeleopActiveFuelCount()
	status.BlueActiveFuel = blueHub.GetShiftCount(game.ShiftAuto, true) + blueHub.GetTeleopActiveFuelCount()

	redEStops, blueEStops := arena.Plc.GetTeamEStops()
	redAStops, blueAStops := arena.Plc.GetTeamAStops()
	eStops := append(redEStops[:], blueEStops[:]...)
	aStops := append(redAStops[:], blueAStops[:]...)
	for i, stationId := range fieldSimStationIds {
		allianceStation := arena.AllianceStations[stationId]
		station := FieldSimStation{
			Id:           stationId,
			Bypass:       allianceStation.Bypass,
			EStop:        allianceStation.EStop,
			AStop:        allianceStation.AStop,
			EStopPressed: eStops[i],
			AStopPressed: aStops[i],
			Ethernet:     allianceStation.Ethernet,
		}
		if allianceStation.Team != nil {
			station.TeamId = allianceStation.Team.Id
		}
		if allianceStation.DsConn != nil {
			station.DsConnected = true
			station.RobotLinked = allianceStation.DsConn.RobotLinked
		}
		status.Stations[i] = station
	}

	// Generate the sign contents with the same code that drives the physical signs.
	arena.TeamSigns.forEachSign(
		arena,
		func(sign *TeamSign, station string, isRed bool, countdown, inMatchRearText string) {
			frontText, frontColor, rearText := sign.generateTexts(arena, station, isRed, countdown, inMatchRearText)
			simSign := FieldSimSign{frontText, fieldSimSignColor(frontColor), rearText}
			switch {
			case station == "" && isRed:
				status.RedTimer = simSign
			case station == "":
				status.BlueTimer = simSign
			default:
				for i := range status.Stations {
					if status.Stations[i].Id == station {
						status.Stations[i].Sign = simSign
					}
				}
			}
		},
	)
	return status
}

// fieldSimSignColor converts a sign color, whose alpha channel is its intensity, to a CSS hex color.
func fieldSimSignColor(signColor color.RGBA) string {
	scale := func(value uint8) uint8 {
		return uint8(int(value) * int(signColor.A) / 255)
	}
	return fmt.Sprintf("#%02x%02x%02x", scale(signColor.R), scale(signColor.G), scale(signColor.B))
}
