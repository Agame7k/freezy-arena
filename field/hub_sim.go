// Copyright 2026 Team 254. All Rights Reserved.
//
// Support for the hub lighting simulator: snapshots of the Hub LEDs, the expected LED timeline over a full match, and
// controls for running a robot-free test match to watch the lights.

package field

import (
	"encoding/hex"
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/led"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/plc"
	"math"
	"time"
)

const (
	hubSimTimelineStepMs    = 100
	hubSimTimelineTailSec   = 2
	hubSimMaxFuelPerCommand = 100
	hubSimStartRetryTime    = 500 * time.Millisecond
	hubSimStartRetryPeriod  = 20 * time.Millisecond
)

var hubSimShiftNames = map[game.Shift]string{
	game.ShiftAuto:       "Auto",
	game.ShiftTransition: "Transition",
	game.Shift1:          "Shift 1",
	game.Shift2:          "Shift 2",
	game.Shift3:          "Shift 3",
	game.Shift4:          "Shift 4",
	game.ShiftEndgame:    "Endgame",
}

// HubLedSnapshot is the current state of both Hubs' LEDs along with the match context that drives them. Pixels are
// hex-encoded RGB triplets in controller order.
type HubLedSnapshot struct {
	Red               string
	Blue              string
	RedMode           led.Mode
	BlueMode          led.Mode
	MatchState        MatchState
	MatchTimeSec      float64
	Shift             string
	ShiftRemainingSec float64
	RedActive         bool
	BlueActive        bool
	AutoWinner        string
	SimAutoWinner     string
	TestMatch         bool
	Aborted           bool
}

// HubLedSegment is a span of match time during which both Hubs' LED modes stay the same.
type HubLedSegment struct {
	StartSec float64
	EndSec   float64
	RedMode  led.Mode
	BlueMode led.Mode
}

// HubShiftSpan is a span of match time belonging to one Hub shift.
type HubShiftSpan struct {
	Name       string
	StartSec   float64
	EndSec     float64
	RedActive  bool
	BlueActive bool
}

// HubLedTimeline describes the Hub LED modes over a whole match.
type HubLedTimeline struct {
	Segments []HubLedSegment
	Shifts   []HubShiftSpan
	EndSec   float64
}

// GetHubLedSnapshot returns the current Hub LED pixels and modes and the match state that is driving them.
func (arena *Arena) GetHubLedSnapshot() HubLedSnapshot {
	redPixels, bluePixels := arena.Leds.GetPixels()
	redMode, blueMode := arena.Leds.GetModes()
	snapshot := HubLedSnapshot{
		Red:           encodeHubPixels(redPixels[:]),
		Blue:          encodeHubPixels(bluePixels[:]),
		RedMode:       redMode,
		BlueMode:      blueMode,
		MatchState:    arena.MatchState,
		MatchTimeSec:  arena.MatchTimeSec(),
		SimAutoWinner: arena.hubSimAutoWinner,
		TestMatch:     arena.CurrentMatch != nil && arena.CurrentMatch.Type == model.Test,
		Aborted:       arena.matchAborted,
	}

	redHub := arena.RedRealtimeScore.CurrentScore.Hub
	blueHub := arena.BlueRealtimeScore.CurrentScore.Hub
	if redHub.WonAuto {
		snapshot.AutoWinner = "red"
	} else if blueHub.WonAuto {
		snapshot.AutoWinner = "blue"
	}

	switch snapshot.MatchState {
	case AutoPeriod, TeleopPeriod:
		currentTime := time.Now()
		if shift, remaining, _, ok := redHub.GetCurrentShiftTiming(arena.MatchStartTime, currentTime); ok {
			snapshot.Shift = hubSimShiftNames[shift]
			snapshot.ShiftRemainingSec = remaining.Seconds()
		}
		redRemaining, _ := redHub.GetActiveShiftTiming(arena.MatchStartTime, currentTime)
		blueRemaining, _ := blueHub.GetActiveShiftTiming(arena.MatchStartTime, currentTime)
		snapshot.RedActive = redRemaining > 0
		snapshot.BlueActive = blueRemaining > 0
	case PausePeriod:
		snapshot.Shift = "Pause"
		snapshot.ShiftRemainingSec = game.GetDurationToTeleopStart().Seconds() - snapshot.MatchTimeSec
	}
	return snapshot
}

// GetHubLedTimeline returns the Hub LED modes over a full match for the given auto winner, using the same logic that
// drives the lights during a real match.
func GetHubLedTimeline(redWonAuto bool) HubLedTimeline {
	redHub := game.Hub{WonAuto: redWonAuto}
	blueHub := game.Hub{WonAuto: !redWonAuto}
	matchStartTime := time.Unix(0, 0)
	matchTime := func(sec float64) time.Time {
		return matchStartTime.Add(time.Duration(sec * float64(time.Second)))
	}
	autoEndSec := game.GetDurationToAutoEnd().Seconds()
	teleopStartSec := game.GetDurationToTeleopStart().Seconds()
	teleopEndSec := game.GetDurationToTeleopEnd().Seconds()
	timeline := HubLedTimeline{EndSec: teleopEndSec + hubLightScoringAssessmentSec + hubSimTimelineTailSec}

	endMs := int(timeline.EndSec * 1000)
	for ms := 0; ms < endMs; ms += hubSimTimelineStepMs {
		sec := float64(ms) / 1000
		var redMode, blueMode led.Mode
		switch {
		case sec < autoEndSec:
			redMode, blueMode = led.RedStartupMode, led.BlueStartupMode
		case sec < teleopStartSec:
			redMode, blueMode = led.RedMode, led.BlueMode
		case sec < teleopEndSec:
			currentTime := matchTime(sec)
			shift, remaining, _, _ := redHub.GetCurrentShiftTiming(matchStartTime, currentTime)
			redRemaining, _ := redHub.GetActiveShiftTiming(matchStartTime, currentTime)
			blueRemaining, _ := blueHub.GetActiveShiftTiming(matchStartTime, currentTime)
			redMode, blueMode = teleopHubLedModes(shift, remaining, redRemaining > 0, blueRemaining > 0, redWonAuto)
		case sec < teleopEndSec+hubLightScoringAssessmentSec:
			redMode, blueMode = led.WhiteMode, led.WhiteMode
		default:
			redMode, blueMode = led.OffMode, led.OffMode
		}

		segmentEndSec := float64(ms+hubSimTimelineStepMs) / 1000
		last := len(timeline.Segments) - 1
		if last >= 0 && timeline.Segments[last].RedMode == redMode && timeline.Segments[last].BlueMode == blueMode {
			timeline.Segments[last].EndSec = segmentEndSec
		} else {
			timeline.Segments = append(timeline.Segments, HubLedSegment{sec, segmentEndSec, redMode, blueMode})
		}
	}

	addShift := func(name string, startSec, endSec float64) {
		midTime := matchTime((startSec + endSec) / 2)
		redRemaining, _ := redHub.GetActiveShiftTiming(matchStartTime, midTime)
		blueRemaining, _ := blueHub.GetActiveShiftTiming(matchStartTime, midTime)
		timeline.Shifts = append(
			timeline.Shifts, HubShiftSpan{name, startSec, endSec, redRemaining > 0, blueRemaining > 0},
		)
	}
	addShift(hubSimShiftNames[game.ShiftAuto], 0, autoEndSec)
	addShift("Pause", autoEndSec, teleopStartSec)
	shiftStartSec := teleopStartSec
	shiftEndSec := shiftStartSec + float64(game.MatchTiming.TransitionShiftDurationSec)
	addShift(hubSimShiftNames[game.ShiftTransition], shiftStartSec, shiftEndSec)
	for shift := game.Shift1; shift <= game.Shift4; shift++ {
		shiftStartSec = shiftEndSec
		shiftEndSec += float64(game.MatchTiming.ShiftDurationSec)
		addShift(hubSimShiftNames[shift], shiftStartSec, shiftEndSec)
	}
	addShift(hubSimShiftNames[game.ShiftEndgame], shiftEndSec, teleopEndSec)
	addShift("Post-Match", teleopEndSec, timeline.EndSec)

	return timeline
}

// StartHubSimMatch loads a test match with every station bypassed and starts it, so that the Hub lighting can be
// watched through a whole match without robots.
func (arena *Arena) StartHubSimMatch() error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	if arena.MatchState == PostMatch {
		if err := arena.resetHubSimMatch(); err != nil {
			return err
		}
	}
	if arena.MatchState != PreMatch {
		return fmt.Errorf("cannot start a simulated match while a match is in progress")
	}
	if arena.CurrentMatch.Type != model.Test {
		if err := arena.LoadTestMatch(); err != nil {
			return err
		}
		if err := arena.applySimTestTeams(); err != nil {
			return err
		}
	}
	// Robots that are linked get enabled by the arena; the rest are bypassed.
	for _, allianceStation := range arena.AllianceStations {
		allianceStation.Bypass = !allianceStation.simRobotLinked()
	}
	if simPlc, err := arena.simPlc(); err == nil {
		// Stand in for the FTA arming the field; loading the match cleared it.
		simPlc.SetFtaReady(true)
	}

	// With a PLC, the arena only clears each station's A-stop latch on its next loop after a match is loaded, so keep
	// trying briefly, like pressing start again a moment later, before reporting why the match can't start.
	deadline := time.Now().Add(hubSimStartRetryTime)
	for {
		err := arena.StartMatch()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(hubSimStartRetryPeriod)
	}
}

// AbortHubSimMatch aborts the running test match.
func (arena *Arena) AbortHubSimMatch() error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	if arena.CurrentMatch.Type != model.Test {
		return fmt.Errorf("the simulator can only abort test matches; use the Match Play page instead")
	}
	return arena.AbortMatch()
}

// ResetHubSimMatch returns the field to pre-match with a fresh test match loaded.
func (arena *Arena) ResetHubSimMatch() error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()
	return arena.resetHubSimMatch()
}

func (arena *Arena) resetHubSimMatch() error {
	if arena.MatchState == PostMatch && arena.CurrentMatch.Type != model.Test {
		return fmt.Errorf(
			"commit or discard the results of %s on the Match Play page first", arena.CurrentMatch.ShortName,
		)
	}
	if err := arena.ResetMatch(); err != nil {
		return err
	}
	if err := arena.LoadTestMatch(); err != nil {
		return err
	}
	return arena.applySimTestTeams()
}

// SkipHubSimMatchTime moves the clock of the running test match forward by the given number of seconds.
func (arena *Arena) SkipHubSimMatchTime(seconds float64) error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	if arena.MatchState != AutoPeriod && arena.MatchState != PausePeriod && arena.MatchState != TeleopPeriod {
		return fmt.Errorf("can only skip ahead while a match is running")
	}
	if arena.CurrentMatch.Type != model.Test {
		return fmt.Errorf("can only skip ahead in a test match")
	}
	maxSkipSec := game.GetDurationToTeleopEnd().Seconds()
	if math.IsNaN(seconds) || seconds <= 0 || seconds > maxSkipSec {
		return fmt.Errorf("can only skip ahead between 0 and %.0f seconds", maxSkipSec)
	}
	arena.MatchStartTime = arena.MatchStartTime.Add(-time.Duration(seconds * float64(time.Second)))
	return nil
}

// SetHubSimAutoWinner forces which alliance wins auto in simulated test matches: "red", "blue", or "" to decide by
// the auto Fuel counts as usual (a tie is random).
func (arena *Arena) SetHubSimAutoWinner(winner string) error {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()

	switch winner {
	case "", "red", "blue":
		arena.hubSimAutoWinner = winner
		return nil
	default:
		return fmt.Errorf("invalid auto winner %q", winner)
	}
}

// EnableSimulation turns on the simulator controls and the simulated robots, and, if no field PLC is configured, swaps
// in a simulated one so that the arena's PLC logic (stack lights, field reset light, Hub motors and lights, Fuel
// counting) runs as it would on the field. Must be called before Run.
func (arena *Arena) EnableSimulation() {
	arena.SimControlsEnabled = true
	if !arena.Plc.IsEnabled() {
		arena.Plc = plc.NewSimPlc()
		arena.Esp32.SetPlc(arena.Plc)
	}
	if arena.SimRobots == nil {
		arena.startSimRobots()
	}
}

// SetSimStationStop presses or releases a driver station's E-stop or A-stop button on the simulated PLC.
func (arena *Arena) SetSimStationStop(station string, aStop, pressed bool) error {
	simPlc, err := arena.simPlc()
	if err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()
	return simPlc.SetStationStop(station, aStop, pressed)
}

// SetSimFieldEStop presses or releases the field E-stop button on the simulated PLC.
func (arena *Arena) SetSimFieldEStop(pressed bool) error {
	simPlc, err := arena.simPlc()
	if err != nil {
		return err
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()
	simPlc.SetFieldEStop(pressed)
	return nil
}

// AddSimHubFuel counts Fuel into the given alliance's Hub ("red" or "blue") on the simulated PLC.
func (arena *Arena) AddSimHubFuel(alliance string, count int) error {
	simPlc, err := arena.simPlc()
	if err != nil {
		return err
	}
	if count < 1 || count > hubSimMaxFuelPerCommand {
		return fmt.Errorf("can only add between 1 and %d Fuel at a time", hubSimMaxFuelPerCommand)
	}
	arena.simMutex.Lock()
	defer arena.simMutex.Unlock()
	switch alliance {
	case "red":
		return simPlc.AddHubFuel(count, 0)
	case "blue":
		return simPlc.AddHubFuel(0, count)
	default:
		return fmt.Errorf("invalid alliance '%s'", alliance)
	}
}

// simPlc returns the simulated PLC, or an error if the simulator isn't enabled or a real PLC is in use.
func (arena *Arena) simPlc() (*plc.SimPlc, error) {
	if err := arena.checkSimControlsEnabled(); err != nil {
		return nil, err
	}
	simPlc, ok := arena.Plc.(*plc.SimPlc)
	if !ok {
		return nil, fmt.Errorf("a real PLC is configured, so its buttons and Fuel sensors can't be simulated")
	}
	return simPlc, nil
}

// hubSimAutoWinnerOverride returns the simulator's forced auto winner, which only applies to test matches.
func (arena *Arena) hubSimAutoWinnerOverride() string {
	if !arena.SimControlsEnabled || arena.CurrentMatch == nil || arena.CurrentMatch.Type != model.Test {
		return ""
	}
	return arena.hubSimAutoWinner
}

func (arena *Arena) checkSimControlsEnabled() error {
	if !arena.SimControlsEnabled {
		return fmt.Errorf(
			"the simulator controls are only available when Cheesy Arena is started with -hubsim or -simfield",
		)
	}
	return nil
}

// encodeHubPixels returns the pixels as a hex string of RGB triplets.
func encodeHubPixels(pixels []led.Color) string {
	data := make([]byte, 0, len(pixels)*3)
	for _, pixel := range pixels {
		data = append(data, pixel.R, pixel.G, pixel.B)
	}
	return hex.EncodeToString(data)
}
