// Copyright 2026 Team 254. All Rights Reserved.
//
// Rewrites the identifiers, names, orders and alliance numbers of an existing bracket so that it can be combined with
// other brackets into a single multi-conference tournament.

package playoff

import "strings"

// bracketOptions describes how to relabel a bracket built by one of the standard bracket constructors.
type bracketOptions struct {
	// Added to every alliance number that comes directly from alliance selection (e.g. +N₁ for conference 2).
	allianceOffset int
	// Prepended to every matchup ID (e.g. "N-").
	idPrefix string
	// Prepended to every match long name (e.g. "North ").
	namePrefix string
	// Prepended to every match short name (e.g. "N").
	shortNamePrefix string
	// Maps the bracket's own match order to a tournament-wide unique order.
	order func(int) int
	// Added to every TBA set number, to keep TBA match keys unique across brackets.
	tbaSetOffset int
	// Copied into every match spec.
	fieldId      int
	conferenceId int
	timeline     int
	// Multiplier applied to each match's duration when scheduling; zero means unscaled.
	durationScale float64
}

// relabel walks the bracket rooted at the given matchup once and rewrites every matchup and match spec according to
// the given options, returning the correspondingly relabeled breaks. Matchups that were already relabeled as part of
// another bracket (e.g. the conference finals feeding a championship) are left alone and not descended into.
func relabel(root *Matchup, breakSpecs []breakSpec, options bracketOptions) []tournamentBreak {
	order := options.order
	if order == nil {
		order = func(order int) int { return order }
	}

	var visit func(matchup *Matchup)
	visitSource := func(source allianceSource) allianceSource {
		switch typedSource := source.(type) {
		case allianceSelectionSource:
			return allianceSelectionSource{typedSource.allianceId + options.allianceOffset}
		case matchupSource:
			visit(typedSource.matchup)
		}
		return source
	}
	visit = func(matchup *Matchup) {
		if matchup.relabeled {
			return
		}
		matchup.relabeled = true
		matchup.id = options.idPrefix + matchup.id
		matchup.ConferenceId = options.conferenceId
		matchup.redAllianceSource = visitSource(matchup.redAllianceSource)
		matchup.blueAllianceSource = visitSource(matchup.blueAllianceSource)
		for _, spec := range matchup.matchSpecs {
			spec.longName = options.namePrefix + spec.longName
			spec.shortName = options.shortNamePrefix + spec.shortName
			spec.order = order(spec.order)
			spec.tbaMatchKey.SetNumber += options.tbaSetOffset
			spec.fieldId = options.fieldId
			spec.conferenceId = options.conferenceId
			spec.timeline = options.timeline
			spec.durationScale = options.durationScale
		}
	}
	visit(root)

	relabeledBreaks := make([]tournamentBreak, len(breakSpecs))
	for i, spec := range breakSpecs {
		relabeledBreaks[i] = tournamentBreak{
			breakSpec: breakSpec{
				orderBefore: order(spec.orderBefore),
				durationSec: spec.durationSec,
				description: strings.TrimSpace(options.namePrefix + spec.description),
			},
			fieldId:  options.fieldId,
			timeline: options.timeline,
		}
	}
	return relabeledBreaks
}
