// Copyright 2026 Team 254. All Rights Reserved.
//
// Simulated FRC Driver Stations, each with a simulated robot behind it, that connect to Cheesy Arena over the same TCP
// and UDP protocol as the real Driver Station, so that the arena's driver station handling runs unchanged.

package dssim

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// RobotState is how far a simulated robot has come up, matching what the Driver Station reports to the field.
type RobotState int

const (
	RobotOff    RobotState = iota // The Driver Station isn't running.
	DsRunning                     // The Driver Station is running but can't reach the robot's radio.
	RadioLinked                   // The radio is linked but the roboRIO isn't.
	RioLinked                     // The roboRIO is linked but no robot code is running.
	CodeRunning                   // Robot code is running.
)

var RobotStateNames = map[RobotState]string{
	RobotOff:    "Off",
	DsRunning:   "DS only",
	RadioLinked: "Radio linked",
	RioLinked:   "roboRIO linked",
	CodeRunning: "Robot code running",
}

// Connection states reported in Status.
const (
	ConnectionOff        = "off"
	ConnectionConnecting = "connecting"
	ConnectionConnected  = "connected"
	ConnectionRejected   = "rejected"
)

const (
	statusPeriod         = 50 * time.Millisecond
	keepalivePeriod      = time.Second
	connectTimeout       = 2 * time.Second
	assignmentTimeout    = 5 * time.Second
	reconnectDelay       = time.Second
	batteryVoltage       = 12.6
	enabledBatterySag    = 0.4
	simulatedTripTimeMs  = 5
	maxTcpPacketBytes    = 65537
	controlPacketMinSize = 22
)

var stationNames = []string{"R1", "R2", "R3", "B1", "B2", "B3"}

var errNotInMatch = errors.New("team is not in the current match")

// Status is what a simulated Driver Station knows, including what the arena last told its robot to do.
type Status struct {
	Station    string
	TeamId     int
	State      RobotState
	Connection string
	Enabled    bool
	Auto       bool
	EStop      bool
	AStop      bool
	GameData   string
}

// driverStation is one simulated Driver Station for one team, reconnecting until it is closed.
type driverStation struct {
	address string
	tcpPort int
	udpPort int
	station string
	teamId  int
	mutex   sync.Mutex
	state   RobotState
	status  Status
	done    chan struct{}
	once    sync.Once
}

func newDriverStation(
	address string, tcpPort, udpPort int, station string, teamId int, state RobotState,
) *driverStation {
	ds := &driverStation{
		address: address,
		tcpPort: tcpPort,
		udpPort: udpPort,
		station: station,
		teamId:  teamId,
		state:   state,
		done:    make(chan struct{}),
	}
	ds.status = Status{Station: station, TeamId: teamId, State: state, Connection: ConnectionConnecting}
	go ds.run()
	return ds
}

func (ds *driverStation) close() {
	ds.once.Do(func() {
		close(ds.done)
	})
}

func (ds *driverStation) setState(state RobotState) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	ds.state = state
	ds.status.State = state
}

func (ds *driverStation) getStatus() Status {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	return ds.status
}

func (ds *driverStation) setConnection(connection string) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	ds.status.Connection = connection
	if connection != ConnectionConnected {
		ds.status.Enabled = false
		ds.status.Auto = false
		ds.status.EStop = false
		ds.status.AStop = false
		ds.status.GameData = ""
	}
}

// run keeps the Driver Station connected to the arena until it is closed, like a real one retrying.
func (ds *driverStation) run() {
	for {
		err := ds.runSession()
		select {
		case <-ds.done:
			return
		default:
		}
		if errors.Is(err, errNotInMatch) {
			ds.setConnection(ConnectionRejected)
		} else {
			ds.setConnection(ConnectionConnecting)
		}
		select {
		case <-ds.done:
			return
		case <-time.After(reconnectDelay):
		}
	}
}

// runSession connects to the arena and exchanges packets until the connection drops or the Driver Station is closed.
func (ds *driverStation) runSession() error {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(ds.address)})
	if err != nil {
		return err
	}
	defer udpConn.Close()

	tcpConn, err := net.DialTimeout("tcp4", fmt.Sprintf("%s:%d", ds.address, ds.tcpPort), connectTimeout)
	if err != nil {
		return err
	}
	defer tcpConn.Close()

	// Introduce ourselves the way the current Driver Station does, including the port to send control packets to.
	team := strconv.Itoa(ds.teamId)
	controlPort := udpConn.LocalAddr().(*net.UDPAddr).Port
	hello := []byte{0, byte(5 + len(team)), 30, byte(controlPort >> 8), byte(controlPort & 0xff), 0, byte(len(team))}
	hello = append(hello, team...)
	if _, err = tcpConn.Write(hello); err != nil {
		return err
	}

	if err = tcpConn.SetReadDeadline(time.Now().Add(assignmentTimeout)); err != nil {
		return err
	}
	assignment, err := readTcpPacket(tcpConn)
	if err != nil {
		return err
	}
	if len(assignment) < 5 || (assignment[2] != 31 && assignment[2] != 25) {
		return fmt.Errorf("unexpected station assignment packet %v", assignment)
	}
	if assignment[4] == 2 {
		return errNotInMatch
	}
	if err = tcpConn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	ds.setConnection(ConnectionConnected)

	// Watch for the arena closing the connection, and for control packets telling the robot what to do.
	sessionErrors := make(chan error, 2)
	go func() {
		for {
			if _, err := readTcpPacket(tcpConn); err != nil {
				sessionErrors <- err
				return
			}
		}
	}()
	go func() {
		buffer := make([]byte, 1500)
		for {
			count, err := udpConn.Read(buffer)
			if err != nil {
				sessionErrors <- err
				return
			}
			ds.handleControlPacket(buffer[:count])
		}
	}()

	statusTicker := time.NewTicker(statusPeriod)
	defer statusTicker.Stop()
	keepaliveTicker := time.NewTicker(keepalivePeriod)
	defer keepaliveTicker.Stop()
	arenaUdpAddr := &net.UDPAddr{IP: net.ParseIP(ds.address), Port: ds.udpPort}
	sequence := 0
	for {
		select {
		case <-ds.done:
			return nil
		case err := <-sessionErrors:
			return err
		case <-statusTicker.C:
			if _, err := udpConn.WriteToUDP(ds.statusPacket(sequence), arenaUdpAddr); err != nil {
				return err
			}
			sequence++
		case <-keepaliveTicker.C:
			if _, err := tcpConn.Write([]byte{0, 1, 29}); err != nil {
				return err
			}
			if logPacket := ds.logPacket(); logPacket != nil {
				if _, err := tcpConn.Write(logPacket); err != nil {
					return err
				}
			}
		}
	}
}

// statusPacket builds the UDP status packet that the Driver Station sends to the field.
func (ds *driverStation) statusPacket(sequence int) []byte {
	ds.mutex.Lock()
	state := ds.state
	enabled := ds.status.Enabled
	ds.mutex.Unlock()

	var linkBits byte
	switch state {
	case RadioLinked:
		linkBits = 0x10
	case RioLinked:
		linkBits = 0x10 | 0x08
	case CodeRunning:
		linkBits = 0x10 | 0x08 | 0x20
	}

	voltage := batteryVoltage
	if enabled {
		voltage -= enabledBatterySag
	}
	volts := int(voltage)
	fraction := int((voltage - float64(volts)) * 256)

	packet := []byte{
		byte(sequence >> 8),
		byte(sequence & 0xff),
		1,
		linkBits,
		byte(ds.teamId >> 8),
		byte(ds.teamId & 0xff),
		byte(volts),
		byte(fraction),
	}
	// Tag 1: lost packets and round trip time.
	return append(packet, 6, 1, 0, 0, 0, 0, simulatedTripTimeMs)
}

// logPacket builds the TCP packet in which the Driver Station reports the robot's mode, or nil if there's no robot code
// to report on.
func (ds *driverStation) logPacket() []byte {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	if ds.state != CodeRunning {
		return nil
	}
	var status byte
	if ds.status.Auto {
		status |= 0x10
	} else {
		status |= 0x20
	}
	if !ds.status.Enabled {
		status |= 0x08
	}
	return []byte{0, 6, 22, 0, 0, 0, 0, status}
}

// handleControlPacket records what the arena told the robot to do.
func (ds *driverStation) handleControlPacket(packet []byte) {
	if len(packet) < controlPacketMinSize {
		return
	}
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	control := packet[3]
	ds.status.Auto = control&0x02 != 0
	ds.status.Enabled = control&0x04 != 0
	ds.status.AStop = control&0x40 != 0
	ds.status.EStop = control&0x80 != 0
	if int(packet[5]) < len(stationNames) {
		ds.status.Station = stationNames[packet[5]]
	}
	ds.status.GameData = ""
	if len(packet) > controlPacketMinSize+1 && packet[controlPacketMinSize+1] == 32 {
		length := int(packet[controlPacketMinSize]) - 1
		start := controlPacketMinSize + 2
		if length > 0 && start+length <= len(packet) {
			ds.status.GameData = string(packet[start : start+length])
		}
	}
}

// readTcpPacket reads one length-prefixed packet, returning it including the two length bytes.
func readTcpPacket(conn net.Conn) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := int(header[0])<<8 + int(header[1])
	if length+2 > maxTcpPacketBytes {
		return nil, fmt.Errorf("packet too long: %d bytes", length)
	}
	packet := make([]byte, 2+length)
	copy(packet, header)
	if _, err := io.ReadFull(conn, packet[2:]); err != nil {
		return nil, err
	}
	return packet, nil
}
