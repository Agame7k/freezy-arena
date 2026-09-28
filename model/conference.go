// Copyright 2026 Team 254. All Rights Reserved.
//
// Model and datastore CRUD methods for a conference in a multi-conference event.

package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// NumConferences is the fixed number of conferences in a multi-conference event.
const NumConferences = 2

// Default series lengths, keyed by round, used for newly created conferences.
var DefaultConferenceSeriesLengths = map[string]int{"EF": 3, "QF": 3, "SF": 3, "F": 3}

type Conference struct {
	Id               int `db:"id,manual"`
	Name             string
	ShortName        string
	Color            string
	LogoSuffix       string
	PlayoffType      PlayoffType
	NumAlliances     int
	SeriesLengths    map[string]int
	PlayoffFieldId   int
	PlayoffStartTime time.Time
}

func (database *Database) CreateConference(conference *Conference) error {
	return database.conferenceTable.create(conference)
}

func (database *Database) GetConferenceById(id int) (*Conference, error) {
	return database.conferenceTable.getById(id)
}

func (database *Database) UpdateConference(conference *Conference) error {
	return database.conferenceTable.update(conference)
}

func (database *Database) TruncateConferences() error {
	return database.conferenceTable.truncate()
}

func (database *Database) GetAllConferences() ([]Conference, error) {
	conferences, err := database.conferenceTable.getAll()
	if err != nil {
		return nil, err
	}
	sort.Slice(
		conferences,
		func(i, j int) bool {
			return conferences[i].Id < conferences[j].Id
		},
	)
	return conferences, nil
}

// EnsureConferences creates the two conference records with default values if they don't already exist, and returns
// all conferences.
func (database *Database) EnsureConferences() ([]Conference, error) {
	defaults := []Conference{
		{
			Id:             1,
			Name:           "North",
			ShortName:      "N",
			Color:          "#1f6feb",
			PlayoffType:    DoubleEliminationPlayoff,
			NumAlliances:   8,
			PlayoffFieldId: 1,
		},
		{
			Id:             2,
			Name:           "South",
			ShortName:      "S",
			Color:          "#e36209",
			PlayoffType:    DoubleEliminationPlayoff,
			NumAlliances:   8,
			PlayoffFieldId: 2,
		},
	}
	for _, conference := range defaults {
		existing, err := database.GetConferenceById(conference.Id)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			conference.SeriesLengths = make(map[string]int)
			for round, length := range DefaultConferenceSeriesLengths {
				conference.SeriesLengths[round] = length
			}
			if err = database.CreateConference(&conference); err != nil {
				return nil, err
			}
		}
	}
	return database.GetAllConferences()
}

// SeriesLength returns the configured series length for the given round, or the default of three if not set.
func (conference Conference) SeriesLength(round string) int {
	if length, ok := conference.SeriesLengths[round]; ok && length > 0 {
		return length
	}
	return 3
}

// Validate checks the conference's playoff configuration for invalid combinations.
func (conference *Conference) Validate() error {
	if conference.Name == "" || conference.ShortName == "" {
		return fmt.Errorf("conference %d must have a name and short name", conference.Id)
	}
	if len(conference.ShortName) > 3 {
		return fmt.Errorf("%s: short name must be at most 3 characters", conference.Name)
	}
	for _, character := range conference.ShortName {
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9') {
			return fmt.Errorf("%s: short name must contain only letters and digits", conference.Name)
		}
	}
	if last := conference.ShortName[len(conference.ShortName)-1]; last >= '0' && last <= '9' {
		// Otherwise match names would be ambiguous (e.g. "N1" + "1" and "N" + "11" are both "N11").
		return fmt.Errorf("%s: short name can't end with a digit", conference.Name)
	}
	if strings.EqualFold(conference.Name, "Championship") {
		return fmt.Errorf("a conference can't be named Championship")
	}
	switch conference.PlayoffType {
	case SingleEliminationPlayoff:
		if conference.NumAlliances < 2 || conference.NumAlliances > 16 {
			return fmt.Errorf(
				"%s: single elimination requires 2 to 16 alliances (got %d)", conference.Name, conference.NumAlliances,
			)
		}
	case DoubleEliminationPlayoff:
		if conference.NumAlliances < 4 || conference.NumAlliances > 8 {
			return fmt.Errorf(
				"%s: double elimination requires 4 to 8 alliances (got %d)", conference.Name, conference.NumAlliances,
			)
		}
	default:
		return fmt.Errorf("%s: invalid playoff type %d", conference.Name, conference.PlayoffType)
	}
	for round, length := range conference.SeriesLengths {
		if length != 1 && length != 3 && length != 5 {
			return fmt.Errorf("%s: series length for %s must be 1, 3 or 5 (got %d)", conference.Name, round, length)
		}
	}
	if conference.PlayoffFieldId < 0 || conference.PlayoffFieldId > 2 {
		return fmt.Errorf("%s: playoff field must be 1 or 2", conference.Name)
	}
	return nil
}

// ConferenceMap returns the given conferences keyed by ID.
func ConferenceMap(conferences []Conference) map[int]Conference {
	conferenceMap := make(map[int]Conference, len(conferences))
	for _, conference := range conferences {
		conferenceMap[conference.Id] = conference
	}
	return conferenceMap
}
