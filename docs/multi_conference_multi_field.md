# Multi-Conference and Multi-Field Events

Two independent switches, both off by default. With both off, Freezy Arena behaves exactly as before.

- **Multi-Conference Mode** (Settings → Multi-Conference): two conferences with their own rankings, alliance
  selection and playoff bracket, optionally joined by an event championship. Works on a single FMS.
- **Multi-Field Role** (Settings → Multi-Field, or `-role`): Standalone, Hub or Node. A hub is the single source of
  truth and runs no field hardware; each node runs one field and mirrors the hub's data.

## Multi-conference setup

1. Turn on multi-conference mode and configure each conference: name, short name (1-3 letters or digits, not ending in a digit; used in labels such as `N3`),
   color, playoff format (double elimination 4–8 alliances, or single elimination/March Madness 2–16), series length
   per round (Bo1/Bo3/Bo5) and playoff field.
2. Choose the championship: champions series, crossover semifinals, double-decker (4-alliance double elimination of
   both conference finalists) or none.
3. Assign teams to conferences on the Team List page (dropdown per team, "Assign selected", or import lines like
   `254,N`). The mode can't be turned on while teams are unassigned or a conference is too small for its alliances,
   and changing a conference's number of alliances is refused if its teams can't fill them; the message says how many
   alliances would fit. The Team List page and the hub's checklist point out both problems, and an import line whose
   conference isn't recognized is reported instead of being left blank silently. Catch these before alliance
   selection: once either conference finalizes, the playoff structure is locked until Clear Playoff/Alliance Data.
4. Qualification schedules can prefer mixed-conference alliances (Match Scheduling page); the seed shown in the
   report reproduces a schedule.
5. Alliance selection has a conference picker. In "per conference" finalize mode, finalizing one conference creates
   the whole tournament and the other conference's matches fill in when it finalizes.

Rankings are still calculated over all qualification matches; `/api/rankings?conference=1|2|all` and the rankings
display (`?conference=1|2|all|alternate`) show conference ranks. Reports accept `?conference=` (rankings, backups,
bracket) and `?field=` (schedule).

## Multi-field setup

The quick way, with no restarts:

1. **Hub laptop:** Settings → Multi-Field → Role **Hub**, save (leave the secret blank and the hub makes up an
   easy-to-type one such as `k7pm-4xqr`), then open **Run → Event Control**. Its **Event Checklist** walks through
   everything else: it shows the secret (with show/copy buttons, also filled into the ready-made command line), then
   connect the fields, add teams, generate the schedule, play, select alliances and run the playoffs. It refreshes
   itself as things happen.
2. **Each field laptop:** Settings → Multi-Field → Role **Node**, Field ID 1 or 2, the hub address (just the IP from
   the hub's "How to connect a field" card; `:8080` is added for you) and the same secret. Save. The live
   **Connection to the hub** panel says what's happening in plain English (wrong secret, hub unreachable, waiting for
   approval...), and tells "the hub refused this field" apart from "the hub can't be reached".
3. **Hub:** click **Approve Field 1** / **Approve Field 2** in the checklist when each field asks to join; the checklist
   shows the address of the laptop that is asking.

If a second laptop is also set up as a field that is already connected, the hub refuses it (instead of the two taking
the connection from each other and both playing that field's matches): the refused laptop says that each field laptop
needs its own Field ID, and the hub's checklist and node table show which address tried. If the connected laptop goes
away, the other one takes over within a few seconds, which also makes swapping in a replacement laptop painless.

Each field loads its first match as soon as the hub's schedule reaches it. On a node, the team list, schedule, awards,
breaks and alliance selection pages are view-only: a notice links to the same page on the hub and the forms are
disabled. A node also doesn't offer to clear data that comes from the hub, and restoring a database backup on a node
fetches everything from the hub again. Switching a machine away from Hub needs a restart when it has real field
hardware, and the page says so.

If a settings save is refused (for example, a node without a hub address), nothing is changed and the form is shown
again with what was entered, so only the mistake needs fixing.

Each machine keeps its own role, field, hub address, secret, passwords and hardware addresses; only event-wide settings
are copied from the hub.

- **Hub**: role Hub, a shared secret. Open **Run → Event Control** to approve nodes, watch both fields, move any
  unplayed match to a field (championship matches whose field is chosen at load time, or matches from a field that is
  down; a field that has the match loaded moves on to its next one), force a resync, and import results bundles.
- **Node**: role Node, field ID 1 or 2, the hub address (e.g. `http://192.168.50.10:8080`) and the same secret.
  Match play shows only this field's matches, the hub connection status, and whether each result reached the hub.

If the hub is unreachable, nodes keep playing mirrored matches and queue results ("HUB OFFLINE: 3 results queued");
they are delivered in order on reconnect, surviving restarts of the node or the hub, and keep the time they were
committed on the field. For long outages, use **Export results bundle** on the node and **Import results bundle** on
the hub (it reports how many results were applied, were already on the hub, or couldn't be applied, and applies
playoff results whose alliances depend on other results in the same bundle in the right order), or, as a last
resort, **Promote node to standalone**.

If the hub refuses a result outright (for example, the match was moved to the other field after this field played
it), the node keeps it as **rejected** instead of discarding it: the match play banner and the hub's node table show
the count, the result is included in results bundles, and later results are still delivered. Fix the cause on the hub
and press **Resync** on the node to send rejected results again. A playoff result that arrives before the hub knows
both alliances is simply retried.

A node mirrors its teams, schedule, alliances and awards from the hub, so changes to them (team list, schedule, awards,
breaks, alliance selection, clearing data, WPA keys) are refused on a node; make those changes on the hub. A node also
can't load a match assigned to the other field, and the hub, which has no field of its own, can't start or simulate a
match. Only field IDs 1 and 2 are accepted.

Qualification field assignment (hub, Multi-Field tab): Alternate (odd/even), Blocks (a field per schedule block) or
Dynamic (nodes press "Load Next Available" and the hub hands out the next match whose teams are rested and not on the
other field).

## Testing on one machine

**One command** starts the hub and both fields in simulation mode, connected and approved, and opens Event Control:

```powershell
.\scripts\start_event.ps1                           # then press "Simulate the whole event" on Event Control
.\scripts\start_event.ps1 -RunAll                   # or play the whole event right away, printing progress
.\scripts\start_event.ps1 -Db nmrc_cmrc.db -RunAll  # the NMRC/CMRC event (simulated on a copy of the database)
.\scripts\start_event.ps1 -Stop
go run ./scripts/cluster_check                      # after stopping: check that the hub and fields agree
```

The **Simulate the whole event** button (Event Control, only with `-simulate`) creates 36 test teams if the event is
empty, has the fields play the qualifications, picks alliances from the rankings, plays the playoffs and stops at the
champion; progress shows in the checklist. Use `-Fresh` to start over, and `-IntervalSec 3` to slow it down.
`start_event.ps1 -Db <file> -RunAll` never changes the original database unless you add `-InPlace`.

The rest of this section describes the pieces those use.

Command-line flags let several instances share a laptop:

| Flag | Purpose |
| --- | --- |
| `-port 8081` | Web server port |
| `-db field1.db` | Event database file |
| `-role hub\|node\|standalone` | Saved into the settings at startup |
| `-field-id 1`, `-field-name "Field 1"` | Node identity |
| `-hub http://localhost:8080`, `-secret dev` | Hub connection |
| `-simulate` | No field hardware or driver station listeners; matches start without robots |
| `-auto-approve-nodes` | Hub approves new nodes automatically |

The scripts in `scripts/` do this for you (PowerShell):

```powershell
.\scripts\dev_cluster.ps1 -Fresh          # hub :8080, Field 1 :8081, Field 2 :8082 (all -simulate)
.\scripts\simulate_event.ps1             # seed teams, play quals on both fields, auto alliance selection, playoffs
.\scripts\dev_cluster.ps1 -Stop
go run ./scripts/cluster_check           # after stopping: hub and nodes agree, outboxes empty, no double-booked team
```

`simulate_event.ps1 -FieldAssignment dynamic` runs the qualifications with nodes claiming matches from the hub one at
a time (`-MinTurnaroundSec` sets the required rest; it defaults to 0 because simulated matches take a second).

With `-simulate`, match play shows a **Simulate Match** button, and these endpoints are available:
`POST /api/dev/simulate_match?type=qualification`, `POST /api/dev/auto_simulate?intervalSec=2&type=playoff`
(`intervalSec=0` stops), `POST /api/dev/seed_event?teams=36&matchesPerTeam=8` (hub, empty database; also takes
`roster`, `conf1Name`/`conf1Short`/`conf2Name`/`conf2Short`, `fieldAssignment` and `minTurnaroundSec`, and checks
everything before saving anything),
`POST /api/dev/auto_alliance_selection` (hub) and `GET /api/dev/status`.

A failure drill: stop the hub process, simulate a few matches on a node (they queue), restart the hub with the same
`-db`, and watch the node's outbox drain.

## Displays for two conferences and two fields

- **Standings side by side**: `/displays/rankings?conference=split` shows each conference's standings in its own
  scrolling column. It is the default for the rankings display in multi-conference mode (`?conference=auto`); other
  choices are `all`, `alternate`, `1` and `2`, set in the URL or on the Display Configuration page. The Standings PDF
  report has one page per conference.
- **Dual Field**: `/displays/dual_field` on the hub shows both fields' current match, state, timer and score.
- **Audience display modes**: the audience display radio buttons (match play, alliance selection) include
  "Standings (conferences side by side)" and "Dual Field (hub only)", which embed those displays full screen. The hub's
  Event Control page has buttons to open these displays and to push Dual Field, Standings or Bracket to every audience
  display.

## NMRC / CMRC event

`docs\nmrc_cmrc_teams.csv` lists the Northern Minnesota Robotics Conference members (from nmrconference.org) and the
Central Minnesota Robotics Hub teams (from rocorirobotics.com) as `number,conference,nickname`. Check it against this
season's rosters. Paste it into the Team List page's import box to load the teams with their conferences, or build a
ready-made database with:

```powershell
.\scripts\seed_nmrc_cmrc.ps1                 # nmrc_cmrc.db: 56 real teams, 10-match qualification schedule
.\scripts\seed_nmrc_cmrc.ps1 -Placeholder    # dummy teams instead
.\cheesy-arena.exe -db nmrc_cmrc.db -role hub -secret dev-secret -simulate
```

For a full simulated run with the real teams on the one-machine cluster:

```powershell
.\scripts\dev_cluster.ps1 -Fresh
.\scripts\simulate_event.ps1 -MatchesPerTeam 10 -Conf1Name NMRC -Conf1Short NM -Conf2Name CMRC -Conf2Short CM `
  -Roster docs\nmrc_cmrc_teams.csv
```

Instances on a port other than 8080 use `event_<port>.db` unless `-db` is given, so a second instance never tries to
open a database that is already in use.
