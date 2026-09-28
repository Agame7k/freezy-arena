// Copyright 2026 Team 254. All Rights Reserved.
//
// Optional post-processing of generated schedules for multi-conference and multi-field events: a seeded search for team
// orderings that mix conferences on each alliance, a reordering pass that spreads out each team's matches when two
// fields alternate, and assignment of matches to fields.

package tournament

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"math"
	"math/rand"
	"sort"
	"time"
)

const (
	defaultSearchIterations = 5000
	sameConferencePenalty   = 100.0
	partnerSpreadPenalty    = 5.0
	fieldSplitPenalty       = 1.0
	reorderIterations       = 20000
)

// ScheduleOptions configures the optional post-processing of a generated schedule.
type ScheduleOptions struct {
	// Seed for all randomness, so that a schedule can be reproduced. Zero picks a seed from the clock.
	Seed int64
	// Search team orderings for alliances that mix conferences (requires each team's conference).
	PreferMixedConferences bool
	// Number of team orderings to try when searching; zero uses the default.
	SearchIterations int
	// Whether the matches are played across two fields (assigns fields and spreads out each team's matches).
	MultiField     bool
	AssignmentMode model.QualFieldAssignmentMode
}

// ScheduleReport summarizes the fairness of a generated schedule, for review before it is saved.
type ScheduleReport struct {
	Seed                   int64
	NumMatches             int
	MixedAlliancePercent   float64
	SameConferenceAlliance int
	// Histogram of how many teams have each number of cross-conference partners.
	CrossPartnerHistogram map[int]int
	MinTurnaroundSec      int
	AvgTurnaroundSec      int
	BackToBackCount       int
	// Minimum and average turnaround for each field-assignment mode that could be used with this schedule.
	TurnaroundByMode map[string][2]int
	// Number of matches each team plays on each field, keyed by team ID.
	TeamFieldCounts map[int][2]int
	TeamConferences map[int]int
}

// BuildScheduleWithOptions creates a schedule like BuildRandomSchedule and then applies the given post-processing,
// returning the matches along with a fairness report.
func BuildScheduleWithOptions(
	teams []model.Team, scheduleBlocks []model.ScheduleBlock, matchType model.MatchType, options ScheduleOptions,
) ([]model.Match, *ScheduleReport, error) {
	if options.Seed == 0 {
		options.Seed = time.Now().UnixNano()
	}
	random := rand.New(rand.NewSource(options.Seed))

	anonSchedule, err := loadAnonSchedule(len(teams), scheduleBlocks)
	if err != nil {
		return nil, nil, err
	}

	teamConferences := make(map[int]int, len(teams))
	for _, team := range teams {
		teamConferences[team.Id] = team.ConferenceId
	}

	// Choose the team ordering, searching for the one that best mixes conferences if requested.
	teamShuffle := random.Perm(len(teams))
	if options.PreferMixedConferences {
		iterations := options.SearchIterations
		if iterations <= 0 {
			iterations = defaultSearchIterations
		}
		bestScore := scoreTeamOrdering(anonSchedule, teams, teamShuffle, options.MultiField)
		for i := 1; i < iterations; i++ {
			candidate := random.Perm(len(teams))
			if score := scoreTeamOrdering(anonSchedule, teams, candidate, options.MultiField); score < bestScore {
				bestScore = score
				teamShuffle = candidate
			}
		}
	}

	if options.MultiField {
		// Spread out each team's matches; this only changes the order of the matches, never who plays together.
		anonSchedule = reorderForTurnaround(anonSchedule, random)
	}

	matches, err := buildScheduleFromTemplate(teams, scheduleBlocks, matchType, anonSchedule, teamShuffle)
	if err != nil {
		return nil, nil, err
	}
	if options.MultiField {
		AssignFields(matches, scheduleBlocks, options.AssignmentMode)
	}

	report := BuildScheduleReport(matches, teamConferences, scheduleBlocks)
	report.Seed = options.Seed
	return matches, report, nil
}

// Scores a team ordering for the anonymized schedule; lower is better.
func scoreTeamOrdering(anonSchedule [][12]int, teams []model.Team, teamShuffle []int, multiField bool) float64 {
	conferenceOf := func(anonTeam int) int {
		return teams[teamShuffle[anonTeam-1]].ConferenceId
	}
	score := 0.0
	crossPartners := make([]int, len(teams)+1)
	fieldCounts := make([][2]int, len(teams)+1)
	for matchIndex, anonMatch := range anonSchedule {
		for _, alliance := range [2][3]int{
			{anonMatch[0], anonMatch[2], anonMatch[4]}, {anonMatch[6], anonMatch[8], anonMatch[10]},
		} {
			first, second, third := conferenceOf(alliance[0]), conferenceOf(alliance[1]), conferenceOf(alliance[2])
			if first == second && second == third {
				score += sameConferencePenalty
			}
			for i, anonTeam := range alliance {
				for j, partner := range alliance {
					if i != j && conferenceOf(anonTeam) != conferenceOf(partner) {
						crossPartners[anonTeam]++
					}
				}
				fieldCounts[anonTeam][matchIndex%2]++
			}
		}
	}

	// Penalize teams whose cross-conference partner count strays from the average.
	total := 0
	for anonTeam := 1; anonTeam <= len(teams); anonTeam++ {
		total += crossPartners[anonTeam]
	}
	average := float64(total) / float64(len(teams))
	for anonTeam := 1; anonTeam <= len(teams); anonTeam++ {
		deviation := float64(crossPartners[anonTeam]) - average
		score += partnerSpreadPenalty * deviation * deviation
		if multiField {
			// Fields differ, so a team should play a similar number of matches on each.
			score += fieldSplitPenalty * math.Abs(float64(fieldCounts[anonTeam][0]-fieldCounts[anonTeam][1]))
		}
	}
	return score
}

// Reorders the matches to maximize each team's minimum gap between matches, never leaving a team in two consecutive
// matches (which would put it on both fields at once when the fields alternate). Only swaps whole matches.
func reorderForTurnaround(anonSchedule [][12]int, random *rand.Rand) [][12]int {
	reordered := append([][12]int(nil), anonSchedule...)
	if len(reordered) < 3 {
		return reordered
	}
	bestCost := turnaroundCost(reordered)
	for i := 0; i < reorderIterations && bestCost.backToBack+bestCost.shortGaps > 0; i++ {
		first := random.Intn(len(reordered))
		second := first + random.Intn(7) - 3
		if second < 0 || second >= len(reordered) || second == first {
			continue
		}
		reordered[first], reordered[second] = reordered[second], reordered[first]
		cost := turnaroundCost(reordered)
		if cost.less(bestCost) {
			bestCost = cost
		} else {
			reordered[first], reordered[second] = reordered[second], reordered[first]
		}
	}
	return reordered
}

type scheduleCost struct {
	backToBack int
	minGap     int
	shortGaps  int
}

// Returns true if this cost is better than the other.
func (cost scheduleCost) less(other scheduleCost) bool {
	if cost.backToBack != other.backToBack {
		return cost.backToBack < other.backToBack
	}
	if cost.minGap != other.minGap {
		return cost.minGap > other.minGap
	}
	return cost.shortGaps < other.shortGaps
}

// Measures how close together each team's matches are: back-to-back appearances, the minimum gap (in matches) and the
// number of gaps of three matches or fewer.
func turnaroundCost(anonSchedule [][12]int) scheduleCost {
	lastIndex := make(map[int]int)
	cost := scheduleCost{minGap: math.MaxInt}
	for matchIndex, anonMatch := range anonSchedule {
		for position := 0; position < 12; position += 2 {
			team := anonMatch[position]
			if previous, ok := lastIndex[team]; ok {
				gap := matchIndex - previous
				if gap == 1 {
					cost.backToBack++
				}
				if gap <= 3 {
					cost.shortGaps++
				}
				if gap < cost.minGap {
					cost.minGap = gap
				}
			}
			lastIndex[team] = matchIndex
		}
	}
	if cost.minGap == math.MaxInt {
		cost.minGap = 0
	}
	return cost
}

// AssignFields sets the field of each match according to the given mode. Alternate puts odd-numbered matches on Field 1
// and even-numbered ones on Field 2; Blocks uses each schedule block's field (alternating within blocks that have none);
// Dynamic leaves every match unassigned for the hub to hand out.
func AssignFields(
	matches []model.Match, scheduleBlocks []model.ScheduleBlock, mode model.QualFieldAssignmentMode,
) {
	matchIndex := 0
	for _, block := range scheduleBlocks {
		for i := 0; i < block.NumMatches && matchIndex < len(matches); i++ {
			match := &matches[matchIndex]
			switch mode {
			case model.DynamicFieldAssignment:
				match.FieldId = 0
			case model.BlocksFieldAssignment:
				if block.FieldId > 0 {
					match.FieldId = block.FieldId
				} else {
					match.FieldId = 1 + i%2
				}
			default:
				match.FieldId = 1 + (match.TypeOrder+1)%2
			}
			matchIndex++
		}
	}
	for ; matchIndex < len(matches); matchIndex++ {
		if mode == model.DynamicFieldAssignment {
			matches[matchIndex].FieldId = 0
		} else {
			matches[matchIndex].FieldId = 1 + (matches[matchIndex].TypeOrder+1)%2
		}
	}
}

// BuildScheduleReport computes the fairness report for the given schedule.
func BuildScheduleReport(
	matches []model.Match, teamConferences map[int]int, scheduleBlocks []model.ScheduleBlock,
) *ScheduleReport {
	report := &ScheduleReport{
		NumMatches:            len(matches),
		CrossPartnerHistogram: make(map[int]int),
		TurnaroundByMode:      make(map[string][2]int),
		TeamFieldCounts:       make(map[int][2]int),
		TeamConferences:       teamConferences,
	}

	numAlliances := 0
	mixedAlliances := 0
	crossPartners := make(map[int]int)
	for _, match := range matches {
		for _, alliance := range [2][3]int{
			{match.Red1, match.Red2, match.Red3}, {match.Blue1, match.Blue2, match.Blue3},
		} {
			numAlliances++
			conferences := map[int]bool{}
			for _, teamId := range alliance {
				conferences[teamConferences[teamId]] = true
			}
			if len(conferences) > 1 {
				mixedAlliances++
			} else {
				report.SameConferenceAlliance++
			}
			for i, teamId := range alliance {
				if _, ok := crossPartners[teamId]; !ok {
					crossPartners[teamId] = 0
				}
				for j, partner := range alliance {
					if i != j && teamConferences[teamId] != teamConferences[partner] {
						crossPartners[teamId]++
					}
				}
				if match.FieldId == 1 || match.FieldId == 2 {
					counts := report.TeamFieldCounts[teamId]
					counts[match.FieldId-1]++
					report.TeamFieldCounts[teamId] = counts
				}
			}
		}
	}
	if numAlliances > 0 {
		report.MixedAlliancePercent = math.Round(1000*float64(mixedAlliances)/float64(numAlliances)) / 10
	}
	for _, count := range crossPartners {
		report.CrossPartnerHistogram[count]++
	}

	// Turnaround as scheduled, plus what each field-assignment mode would give on the same match order.
	report.MinTurnaroundSec, report.AvgTurnaroundSec, report.BackToBackCount = turnaroundStats(matches)
	for _, mode := range []struct {
		name string
		mode model.QualFieldAssignmentMode
	}{{"Alternate", model.AlternateFieldAssignment}, {"Blocks", model.BlocksFieldAssignment}} {
		assigned := append([]model.Match(nil), matches...)
		AssignFields(assigned, scheduleBlocks, mode.mode)
		minimum, average, _ := turnaroundStats(assigned)
		report.TurnaroundByMode[mode.name] = [2]int{minimum, average}
	}
	return report
}

// Returns the minimum and average time between the starts of each team's consecutive matches, and the number of times
// a team appears in two consecutive matches.
func turnaroundStats(matches []model.Match) (int, int, int) {
	sorted := append([]model.Match(nil), matches...)
	sort.Slice(
		sorted,
		func(i, j int) bool {
			return sorted[i].TypeOrder < sorted[j].TypeOrder
		},
	)
	lastTime := make(map[int]time.Time)
	lastOrder := make(map[int]int)
	minimum := math.MaxInt
	total := 0
	count := 0
	backToBack := 0
	for _, match := range sorted {
		for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
			if teamId == 0 {
				continue
			}
			if previous, ok := lastTime[teamId]; ok {
				gap := int(match.Time.Sub(previous).Seconds())
				if gap < minimum {
					minimum = gap
				}
				total += gap
				count++
				if match.TypeOrder-lastOrder[teamId] == 1 {
					backToBack++
				}
			}
			lastTime[teamId] = match.Time
			lastOrder[teamId] = match.TypeOrder
		}
	}
	if count == 0 {
		return 0, 0, backToBack
	}
	return minimum, total / count, backToBack
}

// FormatDuration formats a number of seconds as m:ss, for the schedule report.
func FormatDuration(seconds int) string {
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
