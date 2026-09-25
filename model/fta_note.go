// Copyright 2026 Team 254. All Rights Reserved.
//
// Model and datastore CRUD methods for a timestamped, tagged note an FTA has made about a team.

package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The categories an FTA note can be filed under, in display order.
var FtaNoteTags = []string{"radio", "ethernet", "ds", "code", "can", "brownout", "battery", "bumpers", "mechanical", "other"}

type FtaNote struct {
	Id             int `db:"id"`
	TeamId         int
	MatchId        int
	MatchShortName string
	Time           time.Time
	Tag            string
	Text           string
}

// Returns an error if the note is missing required information or has an unknown tag.
func (note *FtaNote) Validate() error {
	if note.TeamId <= 0 {
		return fmt.Errorf("note must be for a team")
	}
	if strings.TrimSpace(note.Text) == "" {
		return fmt.Errorf("note text must not be empty")
	}
	for _, tag := range FtaNoteTags {
		if note.Tag == tag {
			return nil
		}
	}
	return fmt.Errorf("invalid note tag %q", note.Tag)
}

func (database *Database) CreateFtaNote(note *FtaNote) error {
	if err := note.Validate(); err != nil {
		return err
	}
	return database.ftaNoteTable.create(note)
}

func (database *Database) GetFtaNoteById(id int) (*FtaNote, error) {
	return database.ftaNoteTable.getById(id)
}

func (database *Database) DeleteFtaNote(id int) error {
	return database.ftaNoteTable.delete(id)
}

// Returns the notes for the given team, newest first.
func (database *Database) GetFtaNotesByTeamId(teamId int) ([]FtaNote, error) {
	notes, err := database.GetAllFtaNotes()
	if err != nil {
		return nil, err
	}

	var teamNotes []FtaNote
	for _, note := range notes {
		if note.TeamId == teamId {
			teamNotes = append(teamNotes, note)
		}
	}
	return teamNotes, nil
}

// Returns all notes, newest first.
func (database *Database) GetAllFtaNotes() ([]FtaNote, error) {
	notes, err := database.ftaNoteTable.getAll()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(notes, func(i, j int) bool {
		return notes[i].Id > notes[j].Id
	})
	return notes, nil
}
