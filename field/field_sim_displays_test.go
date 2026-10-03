// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/stretchr/testify/assert"
	"net/url"
	"testing"
)

func TestGetFieldSimDisplays(t *testing.T) {
	displays := GetFieldSimDisplays()
	keys := make(map[string]bool)
	types := make(map[DisplayType]bool)
	for _, display := range displays {
		assert.False(t, keys[display.Key], "duplicate key %s", display.Key)
		keys[display.Key] = true

		// Each URL must be one the real display accepts and registers as its own type.
		parsedUrl, err := url.Parse(display.Url)
		if assert.Nil(t, err) {
			configuration, err := DisplayFromUrl(parsedUrl.Path+"/websocket", parsedUrl.Query())
			if assert.Nil(t, err, display.Url) {
				assert.Equal(t, "fieldsim-"+display.Key, configuration.Id)
				assert.Equal(t, DisplayTypeNames[configuration.Type], display.Type)
				types[configuration.Type] = true
			}
		}
	}

	// Every kind of display shows up somewhere around the field.
	for displayType := range DisplayTypeNames {
		assert.True(t, types[displayType], "no field sim display of type %s", DisplayTypeNames[displayType])
	}
	for _, stationId := range fieldSimStationIds {
		assert.True(t, keys["station-"+stationId])
	}

	assert.Contains(t, displays[0].Url, "/displays/alliance_station?displayId=fieldsim-station-R1")
	assert.Contains(t, displays[0].Url, "&station=R1")
}
