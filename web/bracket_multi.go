// Copyright 2026 Team 254. All Rights Reserved.
//
// Generates the bracket SVG for a multi-conference tournament. When both conferences use single elimination ("March
// Madness"), the first conference flows left to right on the left half, the second flows right to left on the right
// half, and the championship sits in the middle. When either conference uses double elimination, the two conference
// brackets are stacked vertically and flow into the championship on the right. Brackets are laid out automatically by
// round, so any combination of formats and alliance counts works.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/playoff"
	"html"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	bracketWidth        = 1920.0
	bracketHeight       = 1080.0
	bracketTop          = 110.0
	bracketBottom       = 1060.0
	bracketMargin       = 24.0
	bracketMaxBoxWidth  = 200.0
	bracketMinBoxWidth  = 120.0
	bracketMaxBoxHeight = 86.0
	bracketColumnGap    = 26.0
)

// A rectangular area of the SVG into which one bracket is laid out.
type bracketRegion struct {
	left, right, top, bottom float64
	// Direction in which the bracket flows: 1 for left to right, -1 for right to left.
	direction float64
}

type bracketBox struct {
	matchup       *playoff.Matchup
	x, y          float64
	width, height float64
	direction     float64
}

// Returns which conference's bracket to show: 0 for all conferences, or a conference ID.
func (web *Web) bracketConferenceFilter(conferenceParam, fieldParam string, activeMatch *model.Match) int {
	switch conferenceParam {
	case "all":
		return 0
	case "":
	default:
		conferenceId, _ := strconv.Atoi(conferenceParam)
		return conferenceId
	}

	// Default: a node (or a display following a field) shows its own conference's bracket, unless the championship is
	// being played, in which case every display shows the full view.
	fieldId, _ := strconv.Atoi(fieldParam)
	if fieldId == 0 && web.arena.EventSettings.IsNode() {
		fieldId = web.arena.EventSettings.FieldId
	}
	if fieldId == 0 {
		return 0
	}
	if activeMatch != nil && activeMatch.Type == model.Playoff && activeMatch.ConferenceId == 0 &&
		activeMatch.PlayoffMatchGroupId != "" {
		return 0
	}
	for _, conference := range web.arena.PlayoffTournament.Conferences() {
		if conference.PlayoffFieldId == fieldId {
			return conference.Id
		}
	}
	return 0
}

// Writes the multi-conference bracket SVG, optionally limited to one conference.
func (web *Web) generateMultiConferenceBracketSvg(w io.Writer, activeMatch *model.Match, conferenceFilter int) error {
	tournament := web.arena.PlayoffTournament
	alliances, err := web.arena.Database.GetAllAlliances()
	if err != nil {
		return err
	}
	alliancesById := make(map[int]model.Alliance, len(alliances))
	for _, alliance := range alliances {
		alliancesById[alliance.Id] = alliance
	}
	conferences := tournament.Conferences()
	colors := map[int]string{0: "#c9a227"}
	for _, conference := range conferences {
		colors[conference.Id] = conference.Color
	}

	// Group the matchups by conference.
	byConference := make(map[int][]*playoff.Matchup)
	for _, matchGroup := range tournament.MatchGroups() {
		if matchup, ok := matchGroup.(*playoff.Matchup); ok {
			byConference[matchup.ConferenceId] = append(byConference[matchup.ConferenceId], matchup)
		}
	}

	var svg strings.Builder
	svg.WriteString(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" font-family="FuturaLT, Arial, sans-serif">`,
		bracketWidth, bracketHeight,
	))
	svg.WriteString(`<style>.detail{font-size:12px;fill:#bbb}.team{font-size:15px;fill:#fff}` +
		`.source{font-size:13px;fill:#999;font-style:italic}.status{font-size:12px;fill:#ffd23f}` +
		`.winner{font-weight:bold}.title{font-size:40px;fill:#fff}.label{font-size:30px}</style>`)

	var boxes []*bracketBox
	if conferenceFilter > 0 {
		for _, conference := range conferences {
			if conference.Id == conferenceFilter {
				writeBracketTitle(&svg, conference.Name+" Conference", bracketWidth/2, "middle", "#fff")
			}
		}
		boxes = layoutBracket(
			byConference[conferenceFilter],
			bracketRegion{bracketMargin, bracketWidth - bracketMargin, bracketTop, bracketBottom, 1},
		)
	} else {
		boxes = web.layoutAllConferences(&svg, conferences, byConference)
	}

	boxesByMatchup := make(map[*playoff.Matchup]*bracketBox, len(boxes))
	for _, box := range boxes {
		boxesByMatchup[box.matchup] = box
	}

	// Draw the connectors first so that the boxes cover their ends.
	for _, box := range boxes {
		for _, source := range box.matchup.SourceMatchups() {
			if sourceBox, ok := boxesByMatchup[source.Matchup]; ok {
				writeBracketConnector(&svg, sourceBox, box, source)
			}
		}
	}
	for _, box := range boxes {
		writeBracketBox(&svg, box, tournament, alliancesById, colors, activeMatch)
	}
	svg.WriteString(`</svg>`)
	_, err = io.WriteString(w, svg.String())
	return err
}

// Lays out every conference plus the championship, choosing a mirrored or stacked arrangement by format.
func (web *Web) layoutAllConferences(
	svg *strings.Builder, conferences []model.Conference, byConference map[int][]*playoff.Matchup,
) []*bracketBox {
	championship := byConference[0]
	stacked := false
	for _, conference := range conferences {
		if conference.PlayoffType == model.DoubleEliminationPlayoff {
			stacked = true
		}
	}

	var boxes []*bracketBox
	if stacked || len(conferences) != 2 {
		// Conferences stacked vertically, flowing into the championship on the right.
		championshipWidth := 0.0
		if len(championship) > 0 {
			championshipWidth = 260
			if len(championship) > 1 {
				championshipWidth = 520
			}
		}
		right := bracketWidth - bracketMargin - championshipWidth
		sectionHeight := (bracketBottom - bracketTop) / float64(max(1, len(conferences)))
		writeBracketTitle(svg, "Playoff Bracket", bracketWidth/2, "middle", "#fff")
		for i, conference := range conferences {
			top := bracketTop + float64(i)*sectionHeight
			svg.WriteString(fmt.Sprintf(
				`<text class="label" x="%.0f" y="%.0f" fill="%s">%s</text>`,
				bracketMargin, top+26, html.EscapeString(conference.Color), html.EscapeString(conference.Name),
			))
			boxes = append(
				boxes,
				layoutBracket(
					byConference[conference.Id],
					bracketRegion{bracketMargin, right, top + 36, top + sectionHeight - 10, 1},
				)...,
			)
		}
		if len(championship) > 0 {
			boxes = append(
				boxes,
				layoutBracket(
					championship,
					bracketRegion{right + bracketColumnGap, bracketWidth - bracketMargin, bracketTop, bracketBottom, 1},
				)...,
			)
		}
		return boxes
	}

	// Mirrored March Madness layout.
	championshipWidth := 0.0
	if len(championship) > 0 {
		championshipWidth = 300
		if len(championship) > 1 {
			championshipWidth = 560
		}
	}
	halfWidth := (bracketWidth - championshipWidth) / 2
	writeBracketTitle(svg, "Playoff Bracket", bracketWidth/2, "middle", "#fff")
	writeBracketTitle(svg, conferences[0].Name, bracketMargin, "start", conferences[0].Color)
	writeBracketTitle(svg, conferences[1].Name, bracketWidth-bracketMargin, "end", conferences[1].Color)
	boxes = append(
		boxes,
		layoutBracket(
			byConference[conferences[0].Id],
			bracketRegion{bracketMargin, halfWidth - 10, bracketTop, bracketBottom, 1},
		)...,
	)
	boxes = append(
		boxes,
		layoutBracket(
			byConference[conferences[1].Id],
			bracketRegion{halfWidth + championshipWidth + 10, bracketWidth - bracketMargin, bracketTop, bracketBottom, -1},
		)...,
	)
	if len(championship) > 0 {
		boxes = append(
			boxes,
			layoutBracket(
				championship,
				bracketRegion{halfWidth + 20, halfWidth + championshipWidth - 20, bracketTop, bracketBottom, 1},
			)...,
		)
	}
	return boxes
}

func writeBracketTitle(svg *strings.Builder, text string, x float64, anchor, color string) {
	svg.WriteString(fmt.Sprintf(
		`<text class="title" x="%.0f" y="64" text-anchor="%s" style="fill:%s">%s</text>`,
		x, anchor, html.EscapeString(color), html.EscapeString(text),
	))
}

// Lays out the given matchups in columns by round within the region. Matchups fed by a loser (the lower bracket of a
// double-elimination tournament) are placed below the others in each column.
func layoutBracket(matchups []*playoff.Matchup, region bracketRegion) []*bracketBox {
	if len(matchups) == 0 {
		return nil
	}
	inSet := make(map[*playoff.Matchup]bool, len(matchups))
	for _, matchup := range matchups {
		inSet[matchup] = true
	}

	// A matchup's round is one more than the latest round that feeds it within the same set, and it is in the lower
	// bracket if any alliance reaches it through a loss.
	rounds := make(map[*playoff.Matchup]int)
	lower := make(map[*playoff.Matchup]bool)
	var roundOf func(matchup *playoff.Matchup) int
	roundOf = func(matchup *playoff.Matchup) int {
		if round, ok := rounds[matchup]; ok {
			return round
		}
		round := 1
		for _, source := range matchup.SourceMatchups() {
			if !inSet[source.Matchup] {
				continue
			}
			if sourceRound := roundOf(source.Matchup) + 1; sourceRound > round {
				round = sourceRound
			}
			if !source.UseWinner || lower[source.Matchup] {
				lower[matchup] = true
			}
		}
		rounds[matchup] = round
		return round
	}
	numRounds := 0
	columns := make(map[int][]*playoff.Matchup)
	for _, matchup := range matchups {
		round := roundOf(matchup)
		if round > numRounds {
			numRounds = round
		}
	}
	for _, matchup := range matchups {
		// The final of a double-elimination bracket is fed by the lower bracket but belongs with the winners.
		if rounds[matchup] == numRounds {
			lower[matchup] = false
		}
		columns[rounds[matchup]] = append(columns[rounds[matchup]], matchup)
	}

	// Size the boxes to fit the columns and the tallest column.
	width := (region.right - region.left - bracketColumnGap*float64(numRounds-1)) / float64(numRounds)
	width = math.Max(bracketMinBoxWidth, math.Min(bracketMaxBoxWidth, width))
	spacing := 0.0
	if numRounds > 1 {
		spacing = (region.right - region.left - width) / float64(numRounds-1)
	}
	tallest := 1
	for _, column := range columns {
		tallest = max(tallest, len(column))
	}
	height := math.Min(bracketMaxBoxHeight, (region.bottom-region.top)/float64(tallest)-8)

	var boxes []*bracketBox
	for round := 1; round <= numRounds; round++ {
		column := columns[round]
		sort.Slice(
			column,
			func(i, j int) bool {
				if lower[column[i]] != lower[column[j]] {
					return !lower[column[i]]
				}
				return column[i].FirstMatchOrder() < column[j].FirstMatchOrder()
			},
		)
		x := region.left + float64(round-1)*spacing
		if region.direction < 0 {
			x = region.right - width - float64(round-1)*spacing
		}
		if numRounds == 1 {
			x = (region.left + region.right - width) / 2
		}
		slotHeight := (region.bottom - region.top) / float64(len(column))
		for i, matchup := range column {
			y := region.top + slotHeight*float64(i) + (slotHeight-height)/2
			boxes = append(
				boxes,
				&bracketBox{matchup: matchup, x: x, y: y, width: width, height: height, direction: region.direction},
			)
		}
	}
	return boxes
}

// Draws the line from a source matchup to the matchup it feeds; lines carrying a loser are dashed.
func writeBracketConnector(svg *strings.Builder, from, to *bracketBox, source playoff.MatchupSource) {
	fromX := from.x + from.width
	if from.direction < 0 {
		fromX = from.x
	}
	fromY := from.y + from.height/2
	toX := to.x
	if to.direction < 0 || fromX > to.x+to.width {
		toX = to.x + to.width
	}
	toY := to.y + to.height*0.45
	if !source.IsRed {
		toY = to.y + to.height*0.7
	}
	midX := (fromX + toX) / 2
	dash := ""
	if !source.UseWinner {
		dash = ` stroke-dasharray="6,5"`
	}
	svg.WriteString(fmt.Sprintf(
		`<path d="M%.0f %.0f H%.0f V%.0f H%.0f" fill="none" stroke="#888" stroke-width="2"%s/>`,
		fromX, fromY, midX, toY, toX, dash,
	))
}

// Draws one matchup box showing both alliances, their sources if not yet known, and the series status.
func writeBracketBox(
	svg *strings.Builder,
	box *bracketBox,
	tournament *playoff.PlayoffTournament,
	alliances map[int]model.Alliance,
	colors map[int]string,
	activeMatch *model.Match,
) {
	matchup := box.matchup
	stroke := "#555"
	strokeWidth := 1.5
	if activeMatch != nil && activeMatch.PlayoffMatchGroupId == matchup.Id() {
		stroke = "#ffd23f"
		strokeWidth = 4
	}
	svg.WriteString(fmt.Sprintf(
		`<g id="matchup_%s"><rect x="%.0f" y="%.0f" width="%.0f" height="%.0f" rx="6" fill="#1d1d1f" stroke="%s" `+
			`stroke-width="%.1f"/>`,
		html.EscapeString(matchup.Id()), box.x, box.y, box.width, box.height, stroke, strokeWidth,
	))
	// Conference color on the edge the bracket flows from.
	edgeX := box.x
	if box.direction < 0 {
		edgeX = box.x + box.width - 6
	}
	svg.WriteString(fmt.Sprintf(
		`<rect x="%.0f" y="%.0f" width="6" height="%.0f" fill="%s"/>`,
		edgeX, box.y, box.height, html.EscapeString(colors[matchup.ConferenceId]),
	))
	label := matchup.ShortName()
	if detail := matchup.NameDetail(); detail != "" && box.width > 150 {
		label += " · " + detail
	}
	svg.WriteString(fmt.Sprintf(
		`<text class="detail" x="%.0f" y="%.0f">%s</text>`, box.x+12, box.y+15, html.EscapeString(label),
	))

	// Space the alliance rows and the status line evenly below the label.
	rowSpacing := (box.height - 22) / 3
	winner := matchup.WinningAllianceId()
	rows := []struct {
		allianceId int
		source     string
	}{
		{matchup.RedAllianceId, matchup.RedAllianceSourceDisplayName()},
		{matchup.BlueAllianceId, matchup.BlueAllianceSourceDisplayName()},
	}
	for i, row := range rows {
		y := box.y + 18 + rowSpacing*float64(i+1)
		if row.allianceId == 0 {
			svg.WriteString(fmt.Sprintf(
				`<text class="source" x="%.0f" y="%.0f">%s</text>`,
				box.x+12, y, html.EscapeString(bracketSourceText(row.source, tournament)),
			))
			continue
		}
		class := "team"
		if winner == row.allianceId {
			class += " winner"
		}
		text := tournament.AllianceLabel(row.allianceId)
		if alliance, ok := alliances[row.allianceId]; ok && box.width > 140 {
			var teams []string
			for _, teamId := range alliance.Lineup {
				if teamId > 0 {
					teams = append(teams, strconv.Itoa(teamId))
				}
			}
			text += "  " + strings.Join(teams, " ")
		}
		fill := "#e74c3c"
		if i == 1 {
			fill = "#3b82f6"
		}
		svg.WriteString(fmt.Sprintf(
			`<rect x="%.0f" y="%.0f" width="4" height="14" fill="%s"/>`, box.x+12, y-12, fill,
		))
		svg.WriteString(fmt.Sprintf(
			`<text class="%s" x="%.0f" y="%.0f">%s</text>`, class, box.x+20, y, html.EscapeString(text),
		))
	}
	if _, status := matchup.StatusText(); status != "" {
		svg.WriteString(fmt.Sprintf(
			`<text class="status" x="%.0f" y="%.0f">%s</text>`,
			box.x+12, box.y+18+rowSpacing*3, html.EscapeString(status),
		))
	}
	svg.WriteString(`</g>`)
}

// Expands a source display name like "W N-SF1" into "Winner N-SF1", or an alliance source like "A 11" into its label.
func bracketSourceText(source string, tournament *playoff.PlayoffTournament) string {
	switch {
	case strings.HasPrefix(source, "W "):
		return "Winner " + bracketMatchupName(strings.TrimPrefix(source, "W "), tournament)
	case strings.HasPrefix(source, "L "):
		return "Loser " + bracketMatchupName(strings.TrimPrefix(source, "L "), tournament)
	case strings.HasPrefix(source, "A "):
		if allianceId, err := strconv.Atoi(strings.TrimPrefix(source, "A ")); err == nil {
			return "Alliance " + tournament.AllianceLabel(allianceId)
		}
	}
	return source
}

// Returns the name to show for a matchup: its first match's short name (e.g. "NM3"), rather than its internal ID.
func bracketMatchupName(matchupId string, tournament *playoff.PlayoffTournament) string {
	if matchup, ok := tournament.MatchGroups()[matchupId].(*playoff.Matchup); ok {
		return matchup.ShortName()
	}
	return matchupId
}
