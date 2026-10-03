// Copyright 2026 Team 254. All Rights Reserved.

package dssim

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestStatusPacket(t *testing.T) {
	ds := &driverStation{teamId: 254}
	tests := []struct {
		state    RobotState
		linkBits byte
	}{
		{DsRunning, 0},
		{RadioLinked, 0x10},
		{RioLinked, 0x18},
		{CodeRunning, 0x38},
	}
	for _, test := range tests {
		ds.state = test.state
		packet := ds.statusPacket(258)
		assert.Equal(t, []byte{1, 2, 1, test.linkBits, 0, 254, 12, 153, 6, 1, 0, 0, 0, 0, 5}, packet)
	}

	// The battery sags a little while the robot is enabled.
	ds.status.Enabled = true
	packet := ds.statusPacket(0)
	assert.Equal(t, byte(12), packet[6])
	assert.Equal(t, byte(51), packet[7])
}

func TestHandleControlPacket(t *testing.T) {
	ds := &driverStation{}
	packet := make([]byte, 25)
	packet[3] = 0x02 | 0x04
	packet[5] = 4
	packet[22] = 2
	packet[23] = 32
	packet[24] = 'B'
	ds.handleControlPacket(packet)
	assert.Equal(t, Status{Station: "B2", Auto: true, Enabled: true, GameData: "B"}, ds.status)

	packet = make([]byte, 22)
	packet[3] = 0x80 | 0x40
	ds.handleControlPacket(packet)
	assert.Equal(t, Status{Station: "R1", EStop: true, AStop: true}, ds.status)

	// Short packets are ignored.
	ds.handleControlPacket([]byte{0, 0, 0, 0x04})
	assert.False(t, ds.status.Enabled)
}

func TestLogPacket(t *testing.T) {
	ds := &driverStation{state: RioLinked}
	assert.Nil(t, ds.logPacket())

	ds.state = CodeRunning
	assert.Equal(t, []byte{0, 6, 22, 0, 0, 0, 0, 0x28}, ds.logPacket())
	ds.status.Auto = true
	ds.status.Enabled = true
	assert.Equal(t, []byte{0, 6, 22, 0, 0, 0, 0, 0x10}, ds.logPacket())
}

func TestManager(t *testing.T) {
	manager := NewManager("127.0.0.1", 1, 1, func(station string) int {
		return 0
	})
	defer manager.Close()

	assert.NotNil(t, manager.SetState("R4", CodeRunning))
	assert.NotNil(t, manager.SetState("R1", RobotState(9)))
	assert.Nil(t, manager.SetState("B3", CodeRunning))
	assert.Equal(t, CodeRunning, manager.GetState("B3"))

	// With no team at the station, nothing connects.
	statuses := manager.Statuses()
	assert.Equal(t, Status{Station: "B3", State: CodeRunning, Connection: ConnectionOff}, statuses[5])
	assert.Equal(t, Status{Station: "R1", Connection: ConnectionOff}, statuses[0])
}
