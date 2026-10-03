// Copyright 2026 Team 254. All Rights Reserved.
//
// A PLC with no hardware behind it, used by the hub lighting and field simulators.

package plc

import (
	"fmt"
	"log"
)

// SimPlc keeps the PLC's inputs, registers, and coils in memory. It reports itself as enabled and healthy so that the
// arena drives the stack lights, field reset light, Hub motors and lights, and counts Fuel exactly as it would with the
// real PLC; the simulator presses its stop buttons and scores its Fuel.
type SimPlc struct {
	ModbusPlc
}

var simPlcStationStops = map[string][2]input{
	"R1": {red1EStop, red1AStop},
	"R2": {red2EStop, red2AStop},
	"R3": {red3EStop, red3AStop},
	"B1": {blue1EStop, blue1AStop},
	"B2": {blue2EStop, blue2AStop},
	"B3": {blue3EStop, blue3AStop},
}

// NewSimPlc returns a simulated PLC with every stop button released and every ArmorBlock connected.
func NewSimPlc() *SimPlc {
	simPlc := new(SimPlc)
	simPlc.ModbusPlc.SetAddress("")
	simPlc.ResetEstops()
	simPlc.registers[fieldIoConnection] = 1<<armorBlockCount - 1
	return simPlc
}

// SetAddress keeps the simulated PLC in place; a real PLC address takes effect after restarting without the simulator.
func (plc *SimPlc) SetAddress(address string) {
	if address != "" {
		log.Printf("Using the simulated PLC; ignoring PLC address %s until Cheesy Arena is restarted.", address)
	}
}

// IsEnabled returns true so that the arena runs its PLC logic.
func (plc *SimPlc) IsEnabled() bool {
	return true
}

// IsHealthy returns true since there is no connection to lose.
func (plc *SimPlc) IsHealthy() bool {
	return true
}

// SetStationStop presses or releases a driver station's E-stop or A-stop button ("R1" through "B3").
func (plc *SimPlc) SetStationStop(station string, aStop, pressed bool) error {
	stops, ok := simPlcStationStops[station]
	if !ok {
		return fmt.Errorf("invalid alliance station '%s'", station)
	}
	index := stops[0]
	if aStop {
		index = stops[1]
	}
	// The stop inputs are normally closed: true means the button is released.
	plc.inputs[index] = !pressed
	return nil
}

// SetFieldEStop presses or releases the field E-stop button.
func (plc *SimPlc) SetFieldEStop(pressed bool) {
	plc.inputs[fieldEStop] = !pressed
}

// AddHubFuel adds Fuel to the red and blue Hub counters, as if it had passed through the Hub sensors.
func (plc *SimPlc) AddHubFuel(redFuel, blueFuel int) error {
	if redFuel < 0 || blueFuel < 0 {
		return fmt.Errorf("fuel can't be removed from a Hub")
	}
	plc.registers[redHubTotal] += uint16(redFuel)
	plc.registers[blueHubTotal] += uint16(blueFuel)
	return nil
}

var simPlcEthernetInputs = map[string]input{
	"R1": redConnected1,
	"R2": redConnected2,
	"R3": redConnected3,
	"B1": blueConnected1,
	"B2": blueConnected2,
	"B3": blueConnected3,
}

// SetEthernetConnected plugs a driver station's Ethernet into the field, or unplugs it ("R1" through "B3").
func (plc *SimPlc) SetEthernetConnected(station string, connected bool) error {
	index, ok := simPlcEthernetInputs[station]
	if !ok {
		return fmt.Errorf("invalid alliance station '%s'", station)
	}
	plc.inputs[index] = connected
	return nil
}
