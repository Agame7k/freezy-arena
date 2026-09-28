// Copyright 2019 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Functions for managing awards and their associated lower thirds.

package tournament

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
)

// Creates or updates the given award, depending on whether or not it already exists.
func CreateOrUpdateAward(database *model.Database, award *model.Award, createIntroLowerThird bool) error {
	// Validate the award data.
	if award.AwardName == "" {
		return fmt.Errorf("Award name cannot be blank.")
	}
	var team *model.Team
	if award.TeamId > 0 {
		var err error
		team, err = database.GetTeamById(award.TeamId)
		if err != nil {
			return err
		}
		if team == nil {
			return fmt.Errorf("Team %d is not present at this event.", award.TeamId)
		}
	}

	var err error
	if award.Id == 0 {
		err = database.CreateAward(award)
	} else {
		err = database.UpdateAward(award)
	}
	if err != nil {
		return err
	}

	// Create or update associated lower thirds.
	awardIntroLowerThird := model.LowerThird{TopText: award.AwardName, AwardId: award.Id}
	awardWinnerLowerThird := model.LowerThird{
		TopText: award.AwardName, BottomText: award.PersonName, AwardId: award.Id,
	}
	if team != nil {
		if award.PersonName == "" {
			awardWinnerLowerThird.BottomText = fmt.Sprintf("Team %d, %s", team.Id, team.Nickname)
		} else {
			awardWinnerLowerThird.BottomText = fmt.Sprintf(
				"%s &ndash; Team %d, %s", award.PersonName, team.Id, team.Nickname,
			)
		}
	}
	if awardWinnerLowerThird.BottomText == "" {
		awardWinnerLowerThird.BottomText = "(No awardee assigned yet)"
	}
	lowerThirds, err := database.GetLowerThirdsByAwardId(award.Id)
	if err != nil {
		return err
	}
	bottomIndex := 0
	if createIntroLowerThird {
		if err = createOrUpdateAwardLowerThird(database, &awardIntroLowerThird, lowerThirds, 0); err != nil {
			return err
		}
		bottomIndex++
	}
	if err = createOrUpdateAwardLowerThird(database, &awardWinnerLowerThird, lowerThirds, bottomIndex); err != nil {
		return err
	}

	return nil
}

// Deletes the given award and any associated lower thirds.
func DeleteAward(database *model.Database, awardId int) error {
	if err := database.DeleteAward(awardId); err != nil {
		return err
	}

	// Delete lower thirds.
	lowerThirds, err := database.GetLowerThirdsByAwardId(awardId)
	if err != nil {
		return err
	}
	for _, lowerThird := range lowerThirds {
		if err = database.DeleteLowerThird(lowerThird.Id); err != nil {
			return err
		}
	}

	return nil
}

// Generates awards and lower thirds for the tournament winners and finalists.
func CreateOrUpdateWinnerAndFinalistAwards(database *model.Database, winnerAllianceId, finalistAllianceId int) error {
	return createOrUpdateAllianceAwards(
		database, winnerAllianceId, finalistAllianceId, model.WinnerAward, model.FinalistAward, "Winner", "Finalist", 0,
	)
}

// Generates awards and lower thirds for the winners and finalists of the given conference's bracket.
func CreateOrUpdateConferenceAwards(
	database *model.Database, conference *model.Conference, winnerAllianceId, finalistAllianceId int,
) error {
	return createOrUpdateAllianceAwards(
		database,
		winnerAllianceId,
		finalistAllianceId,
		model.ConferenceWinnerAward,
		model.ConferenceFinalistAward,
		conference.Name+" Conference Winner",
		conference.Name+" Conference Finalist",
		conference.Id,
	)
}

// Generates awards and lower thirds for the event champions and, if the championship format has one, the event
// finalists of a multi-conference event.
func CreateOrUpdateEventChampionAwards(database *model.Database, winnerAllianceId, finalistAllianceId int) error {
	return createOrUpdateAllianceAwards(
		database,
		winnerAllianceId,
		finalistAllianceId,
		model.EventChampionAward,
		model.EventFinalistAward,
		"Event Champion",
		"Event Finalist",
		0,
	)
}

// Generates the winner and finalist awards of the given types, replacing any existing awards of those types (and
// conference). A finalist alliance ID of zero skips the finalist awards.
func createOrUpdateAllianceAwards(
	database *model.Database,
	winnerAllianceId, finalistAllianceId int,
	winnerType, finalistType model.AwardType,
	winnerName, finalistName string,
	conferenceId int,
) error {
	var winnerAlliance, finalistAlliance *model.Alliance
	var err error
	if winnerAlliance, err = database.GetAllianceById(winnerAllianceId); err != nil {
		return err
	}
	if winnerAlliance == nil || len(winnerAlliance.TeamIds) == 0 {
		return fmt.Errorf("Winner and/or finalist alliances do not exist or do not contain teams.")
	}
	if finalistAllianceId > 0 {
		if finalistAlliance, err = database.GetAllianceById(finalistAllianceId); err != nil {
			return err
		}
		if finalistAlliance == nil || len(finalistAlliance.TeamIds) == 0 {
			return fmt.Errorf("Winner and/or finalist alliances do not exist or do not contain teams.")
		}
	}

	// Clear out any awards that may exist if the final match was scored more than once.
	winnerAwards, err := database.GetAwardsByType(winnerType)
	if err != nil {
		return err
	}
	finalistAwards, err := database.GetAwardsByType(finalistType)
	if err != nil {
		return err
	}
	for _, award := range append(winnerAwards, finalistAwards...) {
		if award.ConferenceId != conferenceId {
			continue
		}
		if err = DeleteAward(database, award.Id); err != nil {
			return err
		}
	}

	// Create the finalist awards first since they're usually presented first.
	if finalistAlliance != nil {
		if err = createAllianceAwards(database, finalistAlliance, finalistType, finalistName, conferenceId); err != nil {
			return err
		}
	}
	return createAllianceAwards(database, winnerAlliance, winnerType, winnerName, conferenceId)
}

// Creates an award of the given type for each team in the alliance, with an intro lower third for the first.
func createAllianceAwards(
	database *model.Database, alliance *model.Alliance, awardType model.AwardType, awardName string, conferenceId int,
) error {
	award := model.Award{AwardName: awardName, Type: awardType, TeamId: alliance.TeamIds[0], ConferenceId: conferenceId}
	if err := CreateOrUpdateAward(database, &award, true); err != nil {
		return err
	}
	for _, allianceTeamId := range alliance.TeamIds[1:] {
		award.Id = 0
		award.TeamId = allianceTeamId
		if err := CreateOrUpdateAward(database, &award, false); err != nil {
			return err
		}
	}
	return nil
}

func createOrUpdateAwardLowerThird(
	database *model.Database,
	lowerThird *model.LowerThird,
	existingLowerThirds []model.LowerThird,
	index int,
) error {
	if index < len(existingLowerThirds) {
		lowerThird.Id = existingLowerThirds[index].Id
		lowerThird.DisplayOrder = existingLowerThirds[index].DisplayOrder
		return database.UpdateLowerThird(lowerThird)
	} else {
		lowerThird.DisplayOrder = database.GetNextLowerThirdDisplayOrder()
		return database.CreateLowerThird(lowerThird)
	}
}
