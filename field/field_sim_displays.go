// Copyright 2026 Team 254. All Rights Reserved.
//
// The displays that the field simulator shows around the field: one of every display type, configured the way it
// would be at its usual place in the venue.

package field

import (
	"fmt"
)

// FieldSimDisplay is one real display shown by the field simulator. Location says where in the venue it goes, and
// External is set for displays that show content from outside the arena (such as a video stream), which the simulator
// only loads on request.
type FieldSimDisplay struct {
	Key      string
	Title    string
	Location string
	Type     string
	Url      string
	External bool
}

type fieldSimDisplaySpec struct {
	key           string
	title         string
	location      string
	displayType   DisplayType
	configuration map[string]string
}

// fieldSimDisplaySpecs lists the displays around the field. The field monitors are reversed or not so that each one
// shows the red alliance on the side its viewers see red on: drivers look across the field from their own wall, and
// the scoring table faces the field from the side opposite the audience, so it sees red on its right.
func fieldSimDisplaySpecs() []fieldSimDisplaySpec {
	var specs []fieldSimDisplaySpec
	for _, stationId := range fieldSimStationIds {
		specs = append(
			specs,
			fieldSimDisplaySpec{
				"station-" + stationId,
				"Alliance station " + stationId,
				"Alliance wall",
				AllianceStationDisplay,
				map[string]string{"station": stationId},
			},
		)
	}
	return append(
		specs,
		fieldSimDisplaySpec{
			"red-field-monitor",
			"Red drivers' field monitor",
			"Alliance wall",
			FieldMonitorDisplay,
			map[string]string{"ds": "true", "fta": "false", "reversed": "false"},
		},
		fieldSimDisplaySpec{
			"blue-field-monitor",
			"Blue drivers' field monitor",
			"Alliance wall",
			FieldMonitorDisplay,
			map[string]string{"ds": "true", "fta": "false", "reversed": "true"},
		},
		fieldSimDisplaySpec{
			"fta-field-monitor",
			"FTA field monitor",
			"Scoring table",
			FieldMonitorDisplay,
			map[string]string{"ds": "false", "fta": "true", "reversed": "true"},
		},
		fieldSimDisplaySpec{"announcer", "Announcer", "Scoring table", AnnouncerDisplay, nil},
		fieldSimDisplaySpec{
			"twitch",
			"Stream monitor",
			"A/V table",
			TwitchStreamDisplay,
			map[string]string{"channel": "team254"},
		},
		fieldSimDisplaySpec{"spare", "Spare display (unassigned)", "A/V table", PlaceholderDisplay, nil},
		fieldSimDisplaySpec{
			"audience",
			"Audience screen",
			"Big screen",
			AudienceDisplay,
			map[string]string{"background": "#000", "reversed": "false", "overlayLocation": "bottom"},
		},
		fieldSimDisplaySpec{
			"wall",
			"Wall display",
			"Big screen",
			WallDisplay,
			map[string]string{
				"background": "#000", "message": "", "reversed": "false", "topSpacingPx": "0", "zoomFactor": "1",
			},
		},
		fieldSimDisplaySpec{"bracket", "Playoff bracket", "Big screen", BracketDisplay, nil},
		fieldSimDisplaySpec{"queueing", "Queueing", "Queueing", QueueingDisplay, nil},
		fieldSimDisplaySpec{
			"rankings", "Rankings", "Queueing", RankingsDisplay, map[string]string{"scrollMsPerRow": "1000"},
		},
		fieldSimDisplaySpec{"logo", "Logo", "Audience side", LogoDisplay, map[string]string{"message": ""}},
		fieldSimDisplaySpec{
			"webpage",
			"Web page",
			"Audience side",
			WebpageDisplay,
			map[string]string{"url": "https://www.team254.com"},
		},
	)
}

// GetFieldSimDisplays returns the displays that the field simulator shows, with the URLs that the real displays would
// be configured with.
func GetFieldSimDisplays() []FieldSimDisplay {
	var displays []FieldSimDisplay
	for _, spec := range fieldSimDisplaySpecs() {
		display := Display{
			DisplayConfiguration: DisplayConfiguration{
				Id:            "fieldsim-" + spec.key,
				Nickname:      fmt.Sprintf("Field Sim %s", spec.title),
				Type:          spec.displayType,
				Configuration: spec.configuration,
			},
		}
		displays = append(
			displays,
			FieldSimDisplay{
				Key:      spec.key,
				Title:    spec.title,
				Location: spec.location,
				Type:     DisplayTypeNames[spec.displayType],
				Url:      display.ToUrl(),
				External: spec.displayType == TwitchStreamDisplay || spec.displayType == WebpageDisplay,
			},
		)
	}
	return displays
}
