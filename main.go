// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/network"
	"github.com/Team254/cheesy-arena/node"
	"github.com/Team254/cheesy-arena/web"
	"go.etcd.io/bbolt"
	"log"
)

const defaultEventDbPath = "./event.db"
const defaultHttpPort = 8080

// Main entry point for the application.
func main() {
	var httpPort int
	var eventDbPath string
	var simulateHardware bool
	var autoApproveNodes bool
	var overrides field.StartupOverrides
	flag.BoolVar(&network.DevMode, "dev", false, "Bind driver station listeners to all IP addresses for development")
	flag.IntVar(&httpPort, "port", defaultHttpPort, "Port on which to serve the web interface")
	flag.StringVar(
		&eventDbPath, "db", defaultEventDbPath, "Path to the event database file (default event_<port>.db on other ports)",
	)
	flag.BoolVar(
		&simulateHardware,
		"simulate",
		false,
		"Don't use any field hardware or driver station listeners, and allow matches to start without robots (for "+
			"testing, e.g. running a hub and nodes on one machine)",
	)
	flag.StringVar(&overrides.Role, "role", "", "Multi-field role to save into the settings: standalone, hub or node")
	flag.IntVar(&overrides.FieldId, "field-id", 0, "Field ID to save into the settings (nodes only)")
	flag.StringVar(&overrides.FieldName, "field-name", "", "Field name to save into the settings (nodes only)")
	flag.StringVar(&overrides.HubAddress, "hub", "", "Hub address to save into the settings, e.g. http://10.0.0.5:8080")
	flag.StringVar(&overrides.HubSharedSecret, "secret", "", "Hub shared secret to save into the settings")
	flag.BoolVar(
		&autoApproveNodes, "auto-approve-nodes", false, "On a hub, approve new nodes without admin action (testing)",
	)
	flag.Parse()

	// Give each instance on a non-default port its own database unless one was chosen, so that several instances (e.g. a
	// hub and its nodes) can run on one machine without fighting over the same file.
	dbFlagSet := false
	flag.Visit(func(f *flag.Flag) { dbFlagSet = dbFlagSet || f.Name == "db" })
	if !dbFlagSet && httpPort != defaultHttpPort {
		eventDbPath = fmt.Sprintf("./event_%d.db", httpPort)
	}
	log.Printf("Using event database %s", eventDbPath)

	arena, err := field.NewArenaWithOptions(eventDbPath, simulateHardware)
	if errors.Is(err, bbolt.ErrTimeout) {
		log.Fatalf(
			"Error during startup: the event database %s is in use by another running instance. Stop that instance "+
				"or pass a different -db file.", eventDbPath,
		)
	}
	if err != nil {
		log.Fatalln("Error during startup: ", err)
	}
	if err = arena.ApplyStartupOverrides(overrides); err != nil {
		log.Fatalln("Error applying command-line settings: ", err)
	}
	if err = arena.EnsureHubSharedSecret(); err != nil {
		log.Fatalln("Error generating the hub's shared secret: ", err)
	}

	// Start the web server in a separate goroutine.
	web := web.NewWeb(arena)
	switch {
	case arena.EventSettings.IsHub():
		eventHub := hub.New(arena)
		eventHub.AutoApproveNodes = autoApproveNodes
		web.SetHub(eventHub)
		log.Printf("Running as the multi-field hub")
	case arena.EventSettings.IsNode():
		if arena.EventSettings.HubAddress == "" {
			log.Printf("Warning: this node has no hub address; set it with -hub or on the Multi-Field settings tab.")
		}
		web.SetNode(node.Start(arena))
		log.Printf(
			"Running as the node for %s, connecting to the hub at %s", arena.EventSettings.DisplayFieldName(),
			arena.EventSettings.HubAddress,
		)
	}
	go web.ServeWebInterface(httpPort)

	// Run the arena state machine in the main thread.
	arena.Run()
}
