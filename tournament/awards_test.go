// Copyright 2019 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package tournament

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCreateOrUpdateAwardWithIntro(t *testing.T) {
	database := setupTestDb(t)
	database.CreateTeam(&model.Team{Id: 254, Nickname: "Teh Chezy Pofs"})

	award := model.Award{0, model.JudgedAward, "Safety Award", 0, "", 0}
	err := CreateOrUpdateAward(database, &award, true)
	assert.Nil(t, err)
	award2, _ := database.GetAwardById(award.Id)
	assert.Equal(t, award, *award2)
	lowerThirds, _ := database.GetAllLowerThirds()
	if assert.Equal(t, 2, len(lowerThirds)) {
		assert.Equal(t, "Safety Award", lowerThirds[0].TopText)
		assert.Equal(t, "", lowerThirds[0].BottomText)
		assert.Equal(t, "Safety Award", lowerThirds[1].TopText)
		assert.Equal(t, "(No awardee assigned yet)", lowerThirds[1].BottomText)
	}

	award.AwardName = "Saftey Award"
	award.TeamId = 254
	err = CreateOrUpdateAward(database, &award, true)
	assert.Nil(t, err)
	award2, _ = database.GetAwardById(award.Id)
	assert.Equal(t, award, *award2)
	lowerThirds, _ = database.GetAllLowerThirds()
	if assert.Equal(t, 2, len(lowerThirds)) {
		assert.Equal(t, "Saftey Award", lowerThirds[0].TopText)
		assert.Equal(t, "", lowerThirds[0].BottomText)
		assert.Equal(t, "Saftey Award", lowerThirds[1].TopText)
		assert.Equal(t, "Team 254, Teh Chezy Pofs", lowerThirds[1].BottomText)
	}

	err = DeleteAward(database, award.Id)
	assert.Nil(t, err)
	award2, _ = database.GetAwardById(award.Id)
	assert.Nil(t, award2)
	lowerThirds, _ = database.GetAllLowerThirds()
	assert.Empty(t, lowerThirds)
}

func TestCreateOrUpdateAwardWithoutIntro(t *testing.T) {
	database := setupTestDb(t)
	database.CreateTeam(&model.Team{Id: 254, Nickname: "Teh Chezy Pofs"})
	otherLowerThird := model.LowerThird{TopText: "Marco", BottomText: "Polo"}
	database.CreateLowerThird(&otherLowerThird)

	award := model.Award{0, model.WinnerAward, "Winner", 0, "Bob Dorough", 0}
	err := CreateOrUpdateAward(database, &award, false)
	assert.Nil(t, err)
	award2, _ := database.GetAwardById(award.Id)
	assert.Equal(t, award, *award2)
	lowerThirds, _ := database.GetAllLowerThirds()
	if assert.Equal(t, 2, len(lowerThirds)) {
		assert.Equal(t, otherLowerThird, lowerThirds[0])
		assert.Equal(t, "Winner", lowerThirds[1].TopText)
		assert.Equal(t, "Bob Dorough", lowerThirds[1].BottomText)
	}

	award.TeamId = 254
	err = CreateOrUpdateAward(database, &award, false)
	assert.Nil(t, err)
	award2, _ = database.GetAwardById(award.Id)
	assert.Equal(t, award, *award2)
	lowerThirds, _ = database.GetAllLowerThirds()
	if assert.Equal(t, 2, len(lowerThirds)) {
		assert.Equal(t, otherLowerThird, lowerThirds[0])
		assert.Equal(t, "Winner", lowerThirds[1].TopText)
		assert.Equal(t, "Bob Dorough &ndash; Team 254, Teh Chezy Pofs", lowerThirds[1].BottomText)
	}

	err = DeleteAward(database, award.Id)
	assert.Nil(t, err)
	award2, _ = database.GetAwardById(award.Id)
	assert.Nil(t, award2)
	lowerThirds, _ = database.GetAllLowerThirds()
	if assert.Equal(t, 1, len(lowerThirds)) {
		assert.Equal(t, otherLowerThird, lowerThirds[0])
	}
}

func TestCreateOrUpdateWinnerAndFinalistAwards(t *testing.T) {
	database := setupTestDb(t)
	CreateTestAlliances(database, 2)
	database.CreateTeam(&model.Team{Id: 101})
	database.CreateTeam(&model.Team{Id: 102})
	database.CreateTeam(&model.Team{Id: 103})
	database.CreateTeam(&model.Team{Id: 104})
	database.CreateTeam(&model.Team{Id: 201})
	database.CreateTeam(&model.Team{Id: 202})
	database.CreateTeam(&model.Team{Id: 203})
	database.CreateTeam(&model.Team{Id: 204})

	err := CreateOrUpdateWinnerAndFinalistAwards(database, 2, 1)
	assert.Nil(t, err)
	awards, _ := database.GetAllAwards()
	if assert.Equal(t, 8, len(awards)) {
		assert.Equal(t, model.Award{1, model.FinalistAward, "Finalist", 101, "", 0}, awards[0])
		assert.Equal(t, model.Award{2, model.FinalistAward, "Finalist", 102, "", 0}, awards[1])
		assert.Equal(t, model.Award{3, model.FinalistAward, "Finalist", 103, "", 0}, awards[2])
		assert.Equal(t, model.Award{4, model.FinalistAward, "Finalist", 104, "", 0}, awards[3])
		assert.Equal(t, model.Award{5, model.WinnerAward, "Winner", 201, "", 0}, awards[4])
		assert.Equal(t, model.Award{6, model.WinnerAward, "Winner", 202, "", 0}, awards[5])
		assert.Equal(t, model.Award{7, model.WinnerAward, "Winner", 203, "", 0}, awards[6])
		assert.Equal(t, model.Award{8, model.WinnerAward, "Winner", 204, "", 0}, awards[7])
	}
	lowerThirds, _ := database.GetAllLowerThirds()
	if assert.Equal(t, 10, len(lowerThirds)) {
		assert.Equal(t, "Finalist", lowerThirds[0].TopText)
		assert.Equal(t, "", lowerThirds[0].BottomText)
		assert.Equal(t, "Finalist", lowerThirds[1].TopText)
		assert.Equal(t, "Team 101, ", lowerThirds[1].BottomText)
		assert.Equal(t, "Winner", lowerThirds[5].TopText)
		assert.Equal(t, "", lowerThirds[5].BottomText)
		assert.Equal(t, "Winner", lowerThirds[6].TopText)
		assert.Equal(t, "Team 201, ", lowerThirds[6].BottomText)
	}

	err = CreateOrUpdateWinnerAndFinalistAwards(database, 1, 2)
	assert.Nil(t, err)
	awards, _ = database.GetAllAwards()
	if assert.Equal(t, 8, len(awards)) {
		assert.Equal(t, model.Award{9, model.FinalistAward, "Finalist", 201, "", 0}, awards[0])
		assert.Equal(t, model.Award{10, model.FinalistAward, "Finalist", 202, "", 0}, awards[1])
		assert.Equal(t, model.Award{11, model.FinalistAward, "Finalist", 203, "", 0}, awards[2])
		assert.Equal(t, model.Award{12, model.FinalistAward, "Finalist", 204, "", 0}, awards[3])
		assert.Equal(t, model.Award{13, model.WinnerAward, "Winner", 101, "", 0}, awards[4])
		assert.Equal(t, model.Award{14, model.WinnerAward, "Winner", 102, "", 0}, awards[5])
		assert.Equal(t, model.Award{15, model.WinnerAward, "Winner", 103, "", 0}, awards[6])
		assert.Equal(t, model.Award{16, model.WinnerAward, "Winner", 104, "", 0}, awards[7])
	}
	lowerThirds, _ = database.GetAllLowerThirds()
	if assert.Equal(t, 10, len(lowerThirds)) {
		assert.Equal(t, "Finalist", lowerThirds[0].TopText)
		assert.Equal(t, "", lowerThirds[0].BottomText)
		assert.Equal(t, "Finalist", lowerThirds[1].TopText)
		assert.Equal(t, "Team 201, ", lowerThirds[1].BottomText)
		assert.Equal(t, "Winner", lowerThirds[5].TopText)
		assert.Equal(t, "", lowerThirds[5].BottomText)
		assert.Equal(t, "Winner", lowerThirds[6].TopText)
		assert.Equal(t, "Team 101, ", lowerThirds[6].BottomText)
	}
}

func TestCreateConferenceAndChampionAwards(t *testing.T) {
	database := setupTestDb(t)
	database.CreateTeam(&model.Team{Id: 101})
	database.CreateTeam(&model.Team{Id: 102})
	database.CreateTeam(&model.Team{Id: 201})
	database.CreateTeam(&model.Team{Id: 202})
	database.CreateAlliance(&model.Alliance{Id: 1, TeamIds: []int{101, 102}, ConferenceId: 1, Seed: 1})
	database.CreateAlliance(&model.Alliance{Id: 2, TeamIds: []int{201, 202}, ConferenceId: 2, Seed: 1})
	north := model.Conference{Id: 1, Name: "North"}
	south := model.Conference{Id: 2, Name: "South"}

	assert.Nil(t, CreateOrUpdateConferenceAwards(database, &north, 1, 2))
	assert.Nil(t, CreateOrUpdateConferenceAwards(database, &south, 2, 1))
	// Recreating one conference's awards leaves the other conference's awards alone.
	assert.Nil(t, CreateOrUpdateConferenceAwards(database, &north, 1, 2))
	winners, _ := database.GetAwardsByType(model.ConferenceWinnerAward)
	assert.Equal(t, 4, len(winners))
	for _, award := range winners {
		if award.ConferenceId == 1 {
			assert.Equal(t, "North Conference Winner", award.AwardName)
		} else {
			assert.Equal(t, "South Conference Winner", award.AwardName)
		}
	}

	// A championship without a finalist creates only the champion awards.
	assert.Nil(t, CreateOrUpdateEventChampionAwards(database, 2, 0))
	champions, _ := database.GetAwardsByType(model.EventChampionAward)
	assert.Equal(t, 2, len(champions))
	assert.Equal(t, "Event Champion", champions[0].AwardName)
	finalists, _ := database.GetAwardsByType(model.EventFinalistAward)
	assert.Empty(t, finalists)
	assert.Nil(t, CreateOrUpdateEventChampionAwards(database, 1, 2))
	finalists, _ = database.GetAwardsByType(model.EventFinalistAward)
	assert.Equal(t, 2, len(finalists))
	lowerThirds, _ := database.GetAllLowerThirds()
	assert.NotEmpty(t, lowerThirds)
}
