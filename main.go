// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package main

import (
	"flag"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/network"
	"github.com/Team254/cheesy-arena/web"
	"log"
	"net"
)

const eventDbPath = "./event.db"
const httpPort = 8080

// Main entry point for the application.
func main() {
	var hubSim, fieldSim bool
	flag.BoolVar(&network.DevMode, "dev", false, "Bind driver station listeners to all IP addresses for development")
	flag.BoolVar(
		&hubSim,
		"hubsim",
		false,
		"Open the hub lighting simulator windows and enable its test match controls (implies -dev)",
	)
	flag.BoolVar(
		&fieldSim,
		"simfield",
		false,
		"Open the field simulator window (hubs, driver stations with simulated robots, signs, displays) and "+
			"enable its test match controls (implies -dev)",
	)
	flag.Parse()
	if hubSim || fieldSim {
		network.DevMode = true

		// Fail clearly if another copy is already running, rather than opening the simulator onto it.
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", httpPort))
		if err != nil {
			log.Fatalf(
				"Can't start the simulator: port %d is already in use. Is Cheesy Arena already running?", httpPort,
			)
		}
		_ = listener.Close()
	}

	arena, err := field.NewArena(eventDbPath)
	if err != nil {
		log.Fatalln("Error during startup: ", err)
	}
	if hubSim || fieldSim {
		arena.EnableSimulation()
	}

	// Start the web server in a separate goroutine.
	web := web.NewWeb(arena)
	go web.ServeWebInterface(httpPort)
	if hubSim || fieldSim {
		go web.OpenSimWindows(httpPort, hubSim, fieldSim)
	}

	// Run the arena state machine in the main thread.
	arena.Run()
}
