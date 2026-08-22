// Command alphaarc-arc-play actually PLAYS one real ARC-AGI-3 game end to
// end, using bridge.ChooseClickAction to pick every action -- unlike
// cmd/alphaarc-arc-smoke, which deliberately never calls Step().
//
// Only games whose available_actions include ACTION6 are supported --
// ChooseClickAction only ever proposes a click, so anything else isn't
// handled honestly here; the loop stops immediately if a game doesn't
// offer ACTION6, rather than silently sending an action the game never
// asked for.
//
// Defaults are deliberately conservative for a first live run against
// completely untested territory (a real game ChooseClickAction has never
// seen before): -actions 20, not the reference Python SDK's 80, so a bad
// decision loop can't quietly burn a large number of real API calls
// before anyone notices; and -cols/-rows 16 (a fine lattice) prioritizes
// click precision over the graph-cohesion stability a coarser lattice
// would give -- see ARCHITECTURE.md's coarse-vs-fine cohesion experiment:
// an imprecise click fails immediately and hard, while lower cohesion
// degrades gracefully and needs many more cycles than a short first run
// gets to even matter.
//
// Requires ARC_API_KEY in the environment (see .env.example). Run:
//
//	go run ./cmd/alphaarc-arc-play
//	go run ./cmd/alphaarc-arc-play -game <game_id> -actions 40
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"

	"alphaarc/pkg/environment"
	"alphaarc/pkg/environment/bridge"
	"alphaarc/pkg/environment/perception"
	"alphaarc/pkg/environment/remote"
	"alphaarc/pkg/goals"
	"alphaarc/pkg/pipeline"
	"alphaarc/pkg/macro"
)

func main() {
	gameID := flag.String("game", "", "game_id to play (default: first game from /api/games)")
	maxActions := flag.Int("actions", 20, "hard cap on real Step() calls before giving up")
	maxBlobs := flag.Int("maxblobs", 5, "max blobs perceived per frame")
	cols := flag.Int("cols", 16, "grid-cell lattice columns for ChooseClickAction")
	rows := flag.Int("rows", 16, "grid-cell lattice rows for ChooseClickAction")
	curiosityStep := flag.Float64("curiosity-step", 0.1, "how much Curiosity moves per action, up on failure, down on success")
	exploreActions := flag.Float64("explore-actions", 0.2, "probability of trying a random available SIMPLE action (ACTION1-5) instead of a click, so the agent explores the whole action space, not just clicks")
	flag.Parse()

	ctx := context.Background()

	client, err := remote.NewClientFromEnv()
	if err != nil {
		log.Fatalf("client: %v", err)
	}
	fmt.Printf("base URL: %s\n", client.BaseURL())

	games, err := client.ListGames(ctx)
	if err != nil {
		log.Fatalf("list games: %v", err)
	}
	if len(games) == 0 {
		log.Fatalf("no games returned -- nothing to play")
	}
	target := *gameID
	if target == "" {
		target = games[0].GameID
	}

	cardID, err := client.OpenScorecard(ctx, []string{"alphaarc-arc-play"})
	if err != nil {
		log.Fatalf("open scorecard: %v", err)
	}
	fmt.Printf("scorecard opened: %s\n", cardID)

	sess := remote.NewSession(client, target, cardID)
	frame, err := sess.Reset()
	if err != nil {
		client.CloseScorecard(ctx, cardID) //nolint:errcheck -- best-effort cleanup before exiting on the real error
		log.Fatalf("reset %s: %v (scorecard %s closed on a best-effort basis)", target, err, cardID)
	}
	fmt.Printf("RESET %s -> state=%s available_actions=%v\n", target, frame.State, frame.AvailableActions)

	engine := pipeline.NewEngine()
	engine.HER.BeginEpisode(target) // Hindsight Experience Replay: record this attempt as an episode
	memory := bridge.NewOutcomeMemory()
	var herTarget []float64 // a win-like state to steer toward via the learned skill repertoire (set on a level completion)
	// Goal inference by hypothesis-and-test: the agent pursues one candidate goal
	// (all-one-color, symmetry, halves-match, ...) at a time. The PURSUED
	// hypothesis's satisfaction is the goal-directed drive -- the agent acts to
	// REALIZE the current guess; a completion confirms it, a plateau/reset rotates.
	tester := perception.NewHypothesisTester()
	// Object Delta Grammar: learn what clicks actually DO (from before/after), so
	// the counterfactual uses the real effect instead of a hard-guessed one.
	afford := perception.NewAffordanceTable()
	// preferenceOf is the single source of truth for prior preference over a
	// state: learned goal + how well the state satisfies the CURRENT hypothesized
	// goal (the real goal-directed term now, replacing the hand-guessed spatial
	// approach), MINUS a self-preservation term (resemblance to a state that got
	// the agent killed). One place so in-loop, init, and post-reset can't drift.
	preferenceOf := func(grid [][]int) float64 {
		vec := pipeline.ObservationVector(perception.DescribeGridStructural(grid, *maxBlobs, *cols, *rows))
		return engine.LearnedPreference(vec) + hypWeight*macro.DriveScore(grid, perception.BackgroundColor(grid)) - dangerAvoidWeight*engine.DangerProximity(vec)
	}
	actionsTaken := 0
	prevObservation := ""
	prevClickedLabel := ""
	prevLevelsCompleted := frame.LevelsCompleted
	prevPreference := preferenceOf(frame.Grid)
	deadCount := map[string]float64{}                             // per action type: decaying count of "did nothing" (inhibition of return / taboo)
	tries := map[string]int{}                                     // per action type: how many times chosen (optimism for the under-tried)
	driveGain := map[string]float64{}                             // per action token: EMA of OBSERVED compression gain (model-free reinforcement)
	stagnation := 0                                               // consecutive actions with no frame change and no level progress
	
	var startState []float64
	startStateCaptured := false
	
	lastActionTok := ""
	prevNumeric := ""                        // last frame's numeric progress tokens (nobj*/tdist*), for per-target inhibition of return
	tracker := perception.NewObjectTracker() // stable object identity + motion across frames (Core Knowledge)
	prevTopology := ""                       // last frame's object-topology signature, for conveyor-belt-aware stagnation

	var actionQueue []environment.Action
	kinematics := macro.NewMotorKinematics(0) // Assuming background color is 0
	compiler := &macro.IntentCompiler{Kinematics: kinematics}

	// Cortex pixel-repaint macros are DISABLED for the live ARC-3 agent.
	// They are a Floor-2 (static ARC-1/2) paradigm: MacroProg.Apply proposes a
	// whole target grid, then IntentCompiler diffs it into one click per differing
	// pixel -- e.g. MirrorX on vc33 compiled into 1592 clicks and drained the entire
	// 200-action budget on a single macro, so control never reached the Floor-1
	// compression-driven click selection below (the [DRIVE] path). Emptying the list
	// bypasses that branch and lets the interactive agent run: perceive -> learn
	// affordances -> pick the click whose class-macro most raises DriveScore. The
	// Program types remain for cmd/alphaarc-solve (the static Track B).
	macrosToTest := []macro.Program{}
	currentMacroIdx := 0

	// promoteMacro moves the macro at idx to the front of macrosToTest so it is
	// tried FIRST on subsequent levels, while every other macro still follows it
	// (full coverage preserved). Used to warm-start the Cortex from the macro that
	// just completed a level -- ARC-3 levels within one game usually share the rule,
	// and a level_up is the ONE real reward the environment gives, so the winning
	// macro is hard evidence about the goal, not a guessed prior.
	promoteMacro := func(idx int) {
		if idx <= 0 || idx >= len(macrosToTest) {
			return // idx 0 is already first; out-of-range is a no-op
		}
		m := macrosToTest[idx]
		macrosToTest = append(macrosToTest[:idx], macrosToTest[idx+1:]...)
		macrosToTest = append([]macro.Program{m}, macrosToTest...)
	}

	for actionsTaken < *maxActions {

		if len(actionQueue) > 0 {
			action := actionQueue[0]
			actionQueue = actionQueue[1:]
			
			if action.ID == environment.Action6 {
				fmt.Printf("action %d: [MOTOR AGENT] executing queued macro click (%d, %d)\n", actionsTaken+1, action.X, action.Y)
			} else {
				fmt.Printf("action %d: [MOTOR AGENT] executing queued button %d\n", actionsTaken+1, action.ID)
			}
			
			newFrame, stepErr := sess.Step(action)
			actionsTaken++
			if stepErr != nil {
				fmt.Printf("action %d: step failed: %v\n", actionsTaken, stepErr)
				break
			}
			
			if newFrame.State != environment.StateNotFinished {
				fmt.Printf("Motor Agent interrupted: state changed to %s\n", newFrame.State)
				actionQueue = nil
			} else if newFrame.LevelsCompleted > frame.LevelsCompleted {
				fmt.Printf("Motor Agent interrupted: LEVEL UP!\n")
				actionQueue = nil
				// The macro that built this queue just completed a level. currentMacroIdx
				// was incremented right after compiling (line ~191), so the winner is at
				// currentMacroIdx-1. Warm-start the next level by trying it FIRST.
				winnerIdx := currentMacroIdx - 1
				if winnerIdx >= 0 && winnerIdx < len(macrosToTest) {
					fmt.Printf("  [CORTEX] promoting winning macro %T to front for next level\n", macrosToTest[winnerIdx])
					promoteMacro(winnerIdx)
				}
				kinematics = macro.NewMotorKinematics(0) // Reset kinematics for new level
				compiler.Kinematics = kinematics
				currentMacroIdx = 0
			}
			frame = newFrame
			continue
		}

		// (MACRO META-SOLVER HOOK)
		// If we are fully calibrated (or clicks are allowed), propose a TargetGrid:
		canUseMacros := hasAction6(frame.AvailableActions) || kinematics.CheckCalibrated()
		if canUseMacros && currentMacroIdx < len(macrosToTest) {
			macroProg := macrosToTest[currentMacroIdx]
			fmt.Printf("action %d: [CORTEX] Testing macro hypothesis: %T\n", actionsTaken+1, macroProg)
			targetGrid := macroProg.Apply(frame.Grid)
			
			actionQueue = compiler.Compile(frame.Grid, targetGrid)
			currentMacroIdx++
			if len(actionQueue) > 0 {
				fmt.Printf("action %d: [CORTEX] Compiled %T into %d actions\n", actionsTaken+1, macroProg, len(actionQueue))
				continue
			} else {
				fmt.Printf("action %d: [CORTEX] Macro %T caused no changes, skipping\n", actionsTaken+1, macroProg)
			}
		}

		if !hasAction6(frame.AvailableActions) {
			// BUTTON-ONLY AGENT (Calibration / Fallback Mode)
			buttons := availableSimpleActions(frame.AvailableActions)
			if len(buttons) == 0 {
				fmt.Printf("no actions available at all -- stopping\n")
				break
			}

			type buttonStats struct {
				tries      int
				totalGain  float64
				lastGain   float64
				experience float64 // Epistemic experience gained from errors
			}
			btnStats := map[environment.ActionID]*buttonStats{}
			for _, b := range buttons {
				btnStats[b] = &buttonStats{}
			}

			fmt.Printf("BUTTON-ONLY MODE: Calibration Phase for %d buttons\n", len(buttons))

			for actionsTaken < *maxActions {
				var minTries = math.MaxInt32
				for _, b := range buttons {
					if btnStats[b].tries < minTries {
						minTries = btnStats[b].tries
					}
				}

				if minTries > 0 && kinematics.CheckCalibrated() {
					if currentMacroIdx < len(macrosToTest) {
						fmt.Printf("action %d: KINEMATICS CALIBRATED. Yielding to Cortex Macro Planner.\n", actionsTaken+1)
						break // Break out of inner exploration loop to let Cortex run Macros
					}
				}

				var chosenBtn environment.ActionID
				if minTries == 0 {
					for _, b := range buttons {
						if btnStats[b].tries == 0 {
							chosenBtn = b
							break
						}
					}
				} else {
					bestScore := math.Inf(-1)
					// Cortisol rises when the agent stagnates (fear of getting stuck).
					cortisol := float64(stagnation) / 15.0 

					for _, b := range buttons {
						pragmaticAvg := btnStats[b].totalGain / float64(btnStats[b].tries)
						
						// Dopamine: Gained from 'Experience' (errors/mistakes). Counteracts Cortisol.
						dopamineBonus := btnStats[b].experience * 0.05
						
						// Cortisol amplifies the drive for Epistemic exploration over Pragmatic exploitation
						exploration := (0.1 + dopamineBonus) / float64(1+btnStats[b].tries)
						if cortisol > 0.5 {
							exploration *= 2.0 // High stress = higher desperate curiosity
						}
						
						score := pragmaticAvg + exploration
						if score > bestScore {
							bestScore = score
							chosenBtn = b
						}
					}
					if bestScore < 0 && minTries >= 2 {
						chosenBtn = buttons[rand.Intn(len(buttons))]
					}
				}

				preGrid := frame.Grid
				prePref := preferenceOf(frame.Grid)

				action := environment.Action{ID: chosenBtn}
				newFrame, stepErr := sess.Step(action)
				actionsTaken++
				if stepErr != nil {
					break
				}

				// Kinematics Calibration Observation
				kinematics.Observe(chosenBtn, preGrid, newFrame.Grid)

				gridDelta := gridChangeMagnitude(preGrid, newFrame.Grid)
				newPref := preferenceOf(newFrame.Grid)
				prefDelta := newPref - prePref
				leveledUp := newFrame.LevelsCompleted > frame.LevelsCompleted

				btnStats[chosenBtn].tries++
				btnStats[chosenBtn].totalGain += prefDelta
				btnStats[chosenBtn].lastGain = prefDelta

				// 🧠 "Ошибка это не только стресс, а ещё и опыт. А полученный опыт даёт немного дофамина"
				// If we drifted away from the goal (prefDelta <= 0), we gain epistemic experience.
				if prefDelta <= 0 {
					btnStats[chosenBtn].experience += 1.0
				}

				tester.Observe(newFrame.Grid)

				fmt.Printf("action %d: CALIBRATE BUTTON %v -> grid_delta=%d pref=%+.4f sat=%.3f levels=%d\n",
					actionsTaken, chosenBtn, gridDelta, prefDelta,
					tester.Satisfaction(newFrame.Grid), newFrame.LevelsCompleted)

				frame = newFrame
				prevPreference = newPref

				if leveledUp {
					tester.WarmStart()
					engine.ForgetGoal()
					kinematics = macro.NewMotorKinematics(0)
					compiler.Kinematics = kinematics
					currentMacroIdx = 0
					for _, b := range buttons {
						btnStats[b] = &buttonStats{}
					}
				}

				if frame.State == environment.StateWin || frame.State == environment.StateGameOver {
					won := frame.State == environment.StateWin || leveledUp
					newSkills := engine.HER.EndEpisode(won)
					fmt.Printf("terminal %s (won=%v, +%d skills)\n", frame.State, won, newSkills)

					if frame.State == environment.StateGameOver {
						resetFrame, _ := sess.Reset()
						frame = resetFrame
						engine.HER.BeginEpisode(target)
						tester.Refute()
						prevPreference = preferenceOf(frame.Grid)
						kinematics = macro.NewMotorKinematics(0)
						compiler.Kinematics = kinematics
						currentMacroIdx = 0
						for _, b := range buttons {
							btnStats[b] = &buttonStats{}
						}
						continue
					}
					break
				}
				// Stagnation detection for Button games: 
				// Since grid_delta is always >0 due to cursor movement, we judge stagnation
				// by whether the preference function (which evaluates goal satisfaction) changes.
				if math.Abs(prefDelta) < 0.0001 && minTries > 0 {
					stagnation++
				} else {
					stagnation = 0
				}
				
				if stagnation >= 15 {
					fmt.Printf("action %d: HARD STAGNATION RESET (%d dead actions) -- resetting and switching hypothesis\n", actionsTaken, stagnation)
					resetFrame, resetErr := sess.Reset()
					if resetErr == nil {
						frame = resetFrame
						engine.HER.EndEpisode(false)
						engine.HER.BeginEpisode(target)
						tester.Refute()
						prevPreference = preferenceOf(frame.Grid)
						stagnation = 0
						kinematics = macro.NewMotorKinematics(0)
						compiler.Kinematics = kinematics
						currentMacroIdx = 0 // Reset Cortex to try macros again on new hypothesis!
						for _, b := range buttons {
							btnStats[b] = &buttonStats{}
						}
						fmt.Printf("reset: now testing hypothesis %q\n", tester.Current().Name)
						continue
					}
				}
			}
			if frame.State == environment.StateWin {
				break
			}
			continue
		}

		// Diagnostic: total blobs actually present this frame, UNBOUNDED by
		// -maxblobs -- confirms or refutes, from a real count instead of a
		// guessed flag value, whether a real interactive element could be
		// getting cut off by the top-N-by-size ranking before it's ever
		// perceived at all.
		totalBlobs := len(perception.FindBlobs(frame.Grid, perception.BackgroundColor(frame.Grid)))
		fmt.Printf("action %d: %d total blobs in frame (using top %d)\n", actionsTaken+1, totalBlobs, *maxBlobs)

		// Real ground truth from the game server for whether the PREVIOUS
		// action actually completed a level, computed the same way as
		// changedSinceLastFrame below (comparing this frame's value against
		// the one recorded before that previous action). Passed into
		// ChooseClickAction as a strong override on top of the "did the
		// grid change" proxy -- see the doc comment on ChooseClickAction for
		// why this override was added (2026-08-13, a 300-action live run
		// exploiting the proxy while levels_completed never moved).
		levelsCompletedIncreased := frame.LevelsCompleted > prevLevelsCompleted

		// Core Knowledge brick 2: object MOTION since the previous frame -- a
		// cross-frame fact the single-frame grid can't express -- fed into the
		// observation so the forward model perceives that bodies moved.
		// Core Knowledge: stable object identity + motion via the tracker
		// (continuity-based, robust to pixel deformation -- unlike the brittle
		// shape hash). These tokens feed the observation so the graph gets one
		// persistent node per body. topo is the frame's object-level fingerprint
		// (which bodies exist), used below for conveyor-belt-aware stagnation.
		// Object identity + motion, PLUS the numeric-magnitude channel (object
		// count, quantized distance to the salient target) so the observation
		// carries a genuine sense of number, not only categorical tokens.
		numericObs := strings.Join(perception.NumericTokens(frame.Grid), " ")
		motionObs := strings.Join(append(tracker.Track(frame.Grid), numericObs), " ")
		
		if !startStateCaptured {
			rawState := perception.DescribeGridStructural(frame.Grid, *maxBlobs, *cols, *rows) + " " + motionObs + " [GRID] " + perception.DescribeGridCells(frame.Grid, *maxBlobs, *cols, *rows)
			startState = pipeline.EmbedObservation(rawState, 256)
			startStateCaptured = true
		}
		
		topo := tracker.TopologySignature()
		topologyChanged := topo != prevTopology
		prevTopology = topo

		// Fix 3: click candidates in the graph's OWN vocabulary (obj-id labels +
		// centroids), computed after Track so ids are current. These become both
		// the selection candidates and the graph-state diagnostic's labels, so the
		// diagnostic finally shows real node/activation/edges instead of the
		// "not yet in graph" the cell-labels always produced.
		objCandidates := tracker.LabeledObjects()
		labels := make([]string, len(objCandidates))
		for i, lb := range objCandidates {
			labels[i] = lb.Label
		}

		// Inhibition of Return (fix 2): if the PREVIOUS click left the numeric
		// progress tokens (nobj*/tdist*) unchanged, it accomplished nothing
		// meaningful -- lock that target out for a few steps so the budget stops
		// draining into re-toggling an already-checked switch. Judged on the
		// numeric channel, not raw pixels: a switch that visually flips but moves
		// no body/nothing closer is exactly the dead click to refract. Applied
		// BEFORE the choice so this step can't re-pick it.
		if prevClickedLabel != "" && numericObs == prevNumeric {
			memory.Refract(prevClickedLabel, refractorySteps)
		}
		prevNumeric = numericObs

		action, observation, clickedLabel, res, err := bridge.ChooseClickAction(ctx, engine, frame.Grid, "solve the puzzle", *maxBlobs, *cols, *rows, prevObservation, levelsCompletedIncreased, *curiosityStep, rand.Float64(), memory, prevClickedLabel, motionObs, objCandidates)
		if err != nil {
			fmt.Printf("action %d: choose action failed: %v\n", actionsTaken, err)
			break
		}
		// actualSuccess fed into this call's predictive cycle was exactly
		// this comparison (see bridge.actionSucceeded) -- printed here so
		// it's visible whether the router just credited or blamed the
		// previous click for actually changing anything. Curiosity is
		// printed too since it's what determined whether this action was
		// an exploration override or the default WTA/fallback choice.
		changedSinceLastFrame := prevObservation == "" || observation != prevObservation

		// Inhibition of return + stagnation, judged on OBJECT TOPOLOGY, not raw
		// pixels (conveyor-belt fix): a body sliding under act-5 changes pixels
		// every step (changedSinceLastFrame stays true) but the set of bodies is
		// unchanged -- trivial simulation ticking, not progress. So "meaningful
		// progress" is a topology change (a body created/destroyed) OR a level
		// gain; otherwise the last action gets taboo'd and stagnation grows even
		// while pixels churn. When stagnation is sharp, spike curiosity rather
		// than let a predictable conveyor quench it.
		meaningfulProgress := topologyChanged || levelsCompletedIncreased
		if lastActionTok != "" {
			if !meaningfulProgress {
				deadCount[lastActionTok] = deadCount[lastActionTok]*0.9 + 1
				stagnation++
			} else {
				deadCount[lastActionTok] *= 0.5
				stagnation = 0
			}
		}
		if stagnation >= 4 {
			engine.Homeostasis.Curiosity = 1.0 // sharp anti-stagnation kick, not a quench
		}
		// Hard reset: if nothing meaningful has happened for 15 actions,
		// the current strategy is dead. Reset, switch hypothesis, try again.
		if stagnation >= 15 {
			if frame.LevelsCompleted > 0 {
				// We already cleared level(s). A full game reset rewinds past them
				// (vc33's reset returns to L1), throwing the progress away and looping
				// forever re-solving L1. Instead PERSIST on the current level: wipe the
				// taboo + reinforcement state so the agent re-explores THIS level's
				// mechanic from scratch, spike curiosity, rotate the hypothesis, and keep
				// the current frame -- no rewind.
				fmt.Printf("action %d: STAGNATION on level %d (%d dead) -- persisting & re-exploring (no rewind)\n",
					actionsTaken+1, frame.LevelsCompleted, stagnation)
				tester.Refute()
				stagnation = 0
				engine.Homeostasis.Curiosity = 1.0
				deadCount = map[string]float64{}
				tries = map[string]int{}
				driveGain = map[string]float64{}
				continue
			}
			fmt.Printf("action %d: HARD STAGNATION RESET (%d dead actions) -- resetting and switching hypothesis\n", actionsTaken+1, stagnation)
			resetFrame, resetErr := sess.Reset()
			if resetErr != nil {
				fmt.Printf("reset failed: %v -- continuing\n", resetErr)
			} else {
				engine.HER.EndEpisode(false)
				engine.HER.BeginEpisode(target)
				tester.Refute()
				frame = resetFrame
				prevObservation, prevClickedLabel, lastActionTok, prevNumeric = "", "", "", ""
				tracker = perception.NewObjectTracker()
				prevTopology = ""
				prevLevelsCompleted = frame.LevelsCompleted
				prevPreference = preferenceOf(frame.Grid)
				stagnation = 0
				startStateCaptured = false
				herTarget = nil
				deadCount = map[string]float64{}
				tries = map[string]int{}
				fmt.Printf("reset: now testing hypothesis %q (wins=%d)\n", tester.Current().Name, tester.Wins())
				continue
			}
		}

		fmt.Printf("action %d: perceives %q (changed since last frame: %v, levels_completed_increased: %v, curiosity=%.4f, stagnation=%d)\n",
			actionsTaken+1, observation, changedSinceLastFrame, levelsCompletedIncreased, engine.Homeostasis.Curiosity, stagnation)

		// Compression-drive diagnostic: which Core-Knowledge primitive currently
		// compresses this frame best, and by how many bits. This is the emergent
		// goal signal the agent is now driven by -- watching it climb (or not) tells
		// us the drive itself is alive independent of whether the game is solved.
		{
			dbg := perception.BackgroundColor(frame.Grid)
			bp, sav := macro.BestPrimitive(frame.Grid, dbg)
			pts := macro.ResidualTargets(frame.Grid, dbg, 8)
			fmt.Printf("action %d: [DRIVE] best=%s savings=%d score=%.3f | residualCells=%d clusters=%d\n",
				actionsTaken+1, bp.Name, sav, macro.DriveScore(frame.Grid, dbg),
				len(macro.ResidualCells(frame.Grid, dbg)), len(pts))
		}
		// Diagnostic: the predictive-coding signals from THIS cycle's internal
		// forward model. forecast_error is how wrong the PREVIOUS cycle's
		// prediction of this frame turned out to be -- a real cross-cycle
		// surprise measured against a content-based observation embedding, NOT
		// the "did the grid change" proxy on the perceives line above. dopamine
		// is the global plasticity multiplier that surprise now drives (higher
		// on surprise). What to watch for on a live run, before this signal is
		// trusted with anything beyond plasticity: does forecast_error actually
		// rise when the grid visibly changes (changed since last frame: true)
		// and settle when it doesn't, and does dopamine track it? If instead it
		// stays flat regardless of what the frame does, the forward model isn't
		// perceiving the world and the whole predictive-coding layer is cosmetic.
		// seeded=%d/%d shows attentional narrowing directly: activated concepts
		// out of what the full frame would activate. Equal normally; the first
		// number drops below the second exactly when an acute surprise narrows
		// activation to the locus of change.
		fmt.Printf("action %d: predictive-coding: forecast_error=%.4f dopamine=%.4f acute_surprise=%v seeded=%d/%d cortisol=%.4f\n",
			actionsTaken+1, res.ForecastError, engine.Homeostasis.Dopamine, res.AcuteSurprise, res.SeededConcepts, res.SeededConceptsFull, engine.Homeostasis.Cortisol)
		// Diagnostic: real internal graph state for THIS cycle's candidate
		// categories -- cluster assignment, activation, and edges between
		// them -- so diversification in the click choice can be attributed
		// to an actual mechanism (Hebbian edge weight vs. Louvain
		// re-clustering) instead of guessed from external behavior alone.
		fmt.Printf("action %d: graph state: %s\n", actionsTaken+1, bridge.DescribeCategoryGraphState(engine.Graph, labels))
		// Diagnostic: what OutcomeMemory has accumulated for the category
		// about to be clicked -- distinguishes "picked because it's proven"
		// from "picked because WTA/exploration said so with zero evidence."
		// Action-space exploration: ChooseClickAction only ever proposes a
		// click (ACTION6), so the agent has been playing with 1 of the 3
		// available actions. A game may REQUIRE a simple control (ACTION1-5)
		// that no click can substitute for -- the most likely reason a
		// click-only agent stalls at score 0 forever. With probability
		// -explore-actions, send a random available simple action instead, so
		// the agent actually tries the whole action space and can discover
		// (via levels_completed / the forecast) what those controls do. Click
		// (6) and undo (7) are excluded from this exploration.
		// Goal-directed action selection (the A+B+C synergy, live): choose the
		// action TYPE by predicted competence gain -- BestCompetenceAction picks
		// the action the forward model is learning the most from -- with
		// epsilon-exploration (-explore-actions) so untried actions get tried at
		// all. "click" uses ChooseClickAction's chosen target; "act-N" sends the
		// simple control N. The forward model is then conditioned (branch A) on
		// the type actually taken, which is also what accrues the per-action
		// learning progress this selection reads next time.
		// PER-OBJECT action selection (the fix the uniform-graph run motivated).
		// Each object is its OWN click candidate -- token "click-<objlabel>" --
		// instead of a single "click". The forward model is then conditioned on
		// that per-object token, so ActionLearningProgress/ActionPreferenceGain
		// accrue PER OBJECT and the planner can prefer the object it's learning
		// most from / that yields the most preference gain -- breaking the
		// symmetry that made every toggle-node identical when "click" was one
		// token. act-N simple controls stay in the same balance.
		candidates := []string{}
		clickByToken := map[string]perception.LabeledBlob{}
		for _, ob := range objCandidates {
			tok := "click-" + ob.Label
			candidates = append(candidates, tok)
			clickByToken[tok] = ob
		}
		if len(candidates) == 0 {
			// No obj candidates (cell-label fallback path): a bare "click" that
			// uses ChooseClickAction's own chosen target.
			candidates = append(candidates, "click")
		}
		// Residual attention: the compression drive points at the cells that BREAK
		// the best regularity -- the anomaly in a periodic/symmetric world, which is
		// the salient, usually-interactive element, as opposed to the inert texture
		// the object tracker otherwise clicks (the vc33 failure: 200 clicks on
		// periodic-texture blobs, all no-ops). Offer that anomaly as a strongly
		// prioritised click candidate so the agent probes it, not the background.
		for i, rp := range macro.ResidualTargets(frame.Grid, perception.BackgroundColor(frame.Grid), 8) {
			col := 0
			if rp.Y >= 0 && rp.Y < len(frame.Grid) && rp.X >= 0 && rp.X < len(frame.Grid[rp.Y]) {
				col = frame.Grid[rp.Y][rp.X]
			}
			tok := fmt.Sprintf("click-residual-%d", i)
			candidates = append(candidates, tok)
			clickByToken[tok] = perception.LabeledBlob{
				Label: fmt.Sprintf("residual%d", i),
				Blob:  perception.Blob{Color: col, Centroid: perception.Point{X: rp.X, Y: rp.Y}},
			}
		}
		simpleByToken := map[string]environment.ActionID{}
		for _, a := range availableSimpleActions(frame.AvailableActions) {
			tok := fmt.Sprintf("act-%d", a)
			candidates = append(candidates, tok)
			simpleByToken[tok] = a
		}
		// Plan by expected free energy (preference gain + competence), with an
		// OPTIMISM bonus for under-tried candidates, a TABOO penalty (exponential
		// in recent deadness), and a small GRAPH-PRIOR bonus for the object the
		// reconnected graph's WTA actually chose -- so at cold start (all
		// per-object gains ~0) the graph decides, and the learned per-object signal
		// takes over as it accrues.
		graphPick := "click-" + clickedLabel
		// Compression drive (the emergent goal, proven offline): score a candidate
		// class-macro by how much it RAISES structural compression -- DriveScore =
		// best-primitive bits saved, normalized to [0,1] -- instead of a hand-authored
		// invariant. The agent pursues whatever regularity (symmetry / periodicity /
		// repeated objects) compresses THIS grid, decided per grid by the data.
		frameBg := perception.BackgroundColor(frame.Grid)
		driveScore := func(g [][]int) float64 { return macro.DriveScore(g, frameBg) }
		
		// Goal Proposer (Irreversibility): if we don't have a victory-proven herTarget,
		// search memory for an irreversible milestone (a state we reached but couldn't
		// return from). This gives HER a "where to go" without needing a win.
		if herTarget == nil {
			if prop := goals.ProposeIrreversibleTarget(engine.HER, startState, 0.999); prop != nil {
				herTarget = prop
				fmt.Printf("action %d: Goal Proposer identified irreversible milestone from memory as herTarget\n", actionsTaken+1)
			}
		}
		
		// HER goal-conditioned steer: once a target state is known (from win or proposer), ask the skill
		// repertoire for the SHORTEST program that reaches a state like it (Occam),
		// and bias its first action.
		herFirst := ""
		if herTarget != nil {
			if sk, _ := engine.HER.ShortestSkillTo(herTarget, herMinSim); sk != nil && len(sk.Actions) > 0 {
				herFirst = sk.Actions[0]
			}
		}
		chosenTok, bestVal := "", math.Inf(-1)
		for _, c := range candidates {
			efe := engine.ActionPreferenceGain[c] + 0.3*engine.ActionLearningProgress[c]
			optimism := 0.05 / float64(1+tries[c])
			// Buttons (act-N) get a strong epistemic bonus when under-explored:
			// we MUST discover what each control does before we can plan.
			// Without this, clicks with graph priors always outcompete buttons.
			if _, isButton := simpleByToken[c]; isButton && tries[c] < 3 {
				optimism += 0.3 / float64(1+tries[c])
			}
			taboo := 0.1 * (1 - math.Exp(-deadCount[c]))
			prior := 0.0
			if c == graphPick {
				prior = graphPriorBonus
			}
			if c == herFirst {
				prior += herBonus // this action starts the shortest known skill to a win-like state
			}
			if strings.HasPrefix(c, "click-residual") {
				prior += residualBonus // probe a compression anomaly -- the likely interactive element
			}
			// TRANSFORMATION-MDL via MACRO-operators: value clicking this object by
			// the Δsat of transforming its WHOLE CLASS (applying the learned
			// affordance to every object of that color) -- the short rule
			// "transform all of class C", not the individual click. A single click
			// on a dense grid barely moves sat (why climb stalled); the class-wide
			// macro makes a large structural change, so a goal-advancing rule spikes.
			// The agent then realizes the macro incrementally (one class-object per
			// step). Unknown-affordance class -> epistemic bonus (babbling).
			pragmatic := 0.0
			if ob, ok := clickByToken[c]; ok {
				if v, known := afford.MacroValue(frame.Grid, ob.Blob.Color, driveScore); known {
					pragmatic = pragmaticBeta * v
				} else {
					pragmatic = epistemicBonus
				}
			}
			drive := driveGainWeight * driveGain[c] // reinforce clicks that actually raised compression
			if v := efe + optimism - taboo + prior + pragmatic + drive; v > bestVal {
				bestVal, chosenTok = v, c
			}
		}
		exploring := rand.Float64() < *exploreActions
		if exploring || chosenTok == "" {
			chosenTok = candidates[rand.Intn(len(candidates))]
		}
		tries[chosenTok]++
		lastActionTok = chosenTok
		exploredAction := false
		if ob, ok := clickByToken[chosenTok]; ok {
			// Click THIS specific object's centroid (may differ from
			// ChooseClickAction's default when the per-object EFE prefers another).
			action = environment.Action{ID: environment.Action6, X: ob.Blob.Centroid.X, Y: ob.Blob.Centroid.Y}
			clickedLabel = ob.Label
		} else if id, ok := simpleByToken[chosenTok]; ok {
			action = environment.Action{ID: id}
			clickedLabel = "" // not a click -- don't credit a blob label to this step
			exploredAction = true
		}
		// (chosenTok == "click" bare fallback keeps ChooseClickAction's action/label.)
		engine.ConditionForecastOnAction(chosenTok)

		fmt.Printf("action %d: chose %q (%s) -- pref_gain=%+.4f competence=%+.4f%s\n",
			actionsTaken+1, chosenTok,
			map[bool]string{true: "exploring", false: "by expected free energy"}[exploring],
			engine.ActionPreferenceGain[chosenTok], engine.ActionLearningProgress[chosenTok],
			simpleActionGainString(engine.ActionPreferenceGain, simpleByToken))
		if exploredAction {
			fmt.Printf("action %d: sending simple action %v (not a click)\n", actionsTaken+1, action.ID)
		} else if rate, attempts := memory.SuccessRate(clickedLabel); attempts > 0 {
			fmt.Printf("action %d: clicking %q, prior record: %d/%d successful\n", actionsTaken+1, clickedLabel, int(rate*float64(attempts)+0.5), attempts)
		} else {
			fmt.Printf("action %d: clicking %q, no prior record\n", actionsTaken+1, clickedLabel)
		}
		prevObservation = observation
		prevClickedLabel = clickedLabel
		prevLevelsCompleted = frame.LevelsCompleted

		// The state this action is about to be taken FROM -- captured before the
		// grid is reassigned, so if the action turns out fatal we can remember
		// this "about to die" configuration as a danger exemplar.
		preStepGrid := frame.Grid
		newFrame, stepErr := sess.Step(action)
		actionsTaken++
		if stepErr != nil {
			fmt.Printf("action %d: step failed: %v\n", actionsTaken, stepErr)
			break
		}
		frame = newFrame

		// Efference-copy learning: diff the clicked object before vs after to learn
		// what this click actually did, updating the affordance table. Only for a
		// real click (ACTION6); simple/undo actions aren't object-targeted.
		if !exploredAction && action.ID == environment.Action6 {
			afford.ObserveClick(preStepGrid, frame.Grid, action.X, action.Y)
		}

		// Model-free compression reinforcement. The affordance MODEL cannot learn
		// vc33-style buttons: the effect is 264 cells > maxChangeCells (dropped as a
		// cascade), keyed by colour (both 9-buttons collide), and stored relative to
		// the trigger though the effect is absolute. But the OBSERVED compression delta
		// is unambiguous -- credit each clicked token with how much it actually raised
		// the drive's savings, so the grow button (savings up) is reinforced and shrink
		// (savings down) suppressed. This is the validated gradient (probe: grow
		// 1332->1390 completes L1) doing the work the predictor cannot.
		//
		// Use BestPrimitiveDelta (max per-primitive Δsavings), not
		// DrivePreference-of-DrivePreference: the latter diffs the argmax
		// primitive of each frame, so a dominant whole-grid primitive (e.g.
		// background Translate on s5i5/tn36) can mask a smaller primitive's
		// (e.g. Correspondence) real local gain from ever reaching driveGain.
		dd := float64(macro.BestPrimitiveDelta(preStepGrid, frame.Grid, frameBg))
		driveGain[chosenTok] = 0.6*driveGain[chosenTok] + 0.4*dd

		// Pragmatic drive (source of meaning). LEARNED, not guessed (the dream):
		// if this action completed a level, remember the winning state as the
		// goal; thereafter preference = how much a state resembles a winning one
		// (LearnedPreference), plus a weak structural prior so there's some
		// gradient BEFORE the first win (until then, curiosity toward the unusual
		// does the finding). The winning cycle itself yields a big preference
		// jump -- exactly the reward that teaches which action mattered.
		leveledUp := frame.LevelsCompleted > prevLevelsCompleted
		if leveledUp {
			// SCOPED confirmation + Bayesian warm-start (fix a): the pursued
			// hypothesis just won, so keep it at the head of the queue for the next
			// level (the mechanic may carry over even though the geometry changed) --
			// but re-open rotation (each level can have a different goal) and FORGET
			// the level-specific learned goal. Reset the perceptual baselines too:
			// a new level is new objects, so old identity/topology/IoR state is stale.
			tester.WarmStart()
			engine.ForgetGoal()
			// HER: this state is a REAL success -- remember it as a target the skill
			// repertoire can steer toward (the "wooden horse" real memory among the
			// implanted ones). ShortestSkillTo(herTarget) then biases action choice.
			rawState := perception.DescribeGridStructural(frame.Grid, *maxBlobs, *cols, *rows) + " [GRID] " + perception.DescribeGridCells(frame.Grid, *maxBlobs, *cols, *rows)
			herTarget = pipeline.EmbedObservation(rawState, 256)
			tracker = perception.NewObjectTracker()
			prevTopology, prevNumeric = "", ""
			stagnation = 0
			fmt.Printf("action %d: LEVEL %d COMPLETED pursuing %q -- warm-starting it, forgetting stale goal, fresh perceptual baseline\n",
				actionsTaken, frame.LevelsCompleted, tester.Current().Name)
		}
		// Fold this frame into hypothesis tracking; a plateau rotates to the next
		// candidate goal. When it rotates, the preference target changes, so the
		// step's delta spans two different goals and must NOT be credited to the
		// action (it isn't that action's doing) -- recompute the baseline instead.
		rotated := tester.Observe(frame.Grid)
		// Goal-directed preference: how well the frame satisfies the CURRENT
		// hypothesized goal, plus learned preference, minus self-preservation.
		newPreference := preferenceOf(frame.Grid)
		preferenceDelta := newPreference - prevPreference
		// A rotation OR a level change moves the preference target, so the step's
		// delta spans two different goals and must NOT be credited to the action.
		discontinuity := rotated || leveledUp
		preferenceIncreased := !discontinuity && newPreference > prevPreference
		if preferenceIncreased && !exploredAction && clickedLabel != "" {
			memory.Record(clickedLabel, true)
		}
		if discontinuity {
			fmt.Printf("action %d: goal changed -> now pursuing %q\n", actionsTaken, tester.Current().Name)
		} else {
			// Plan: credit this action's realized preference change so BestAction
			// can steer toward the goal next time (the pragmatic half of expected
			// free energy). Attributed to the action just taken.
			engine.AttributePreferenceGain(preferenceDelta)
		}
		fmt.Printf("action %d: preference=%.4f delta=%+.4f hyp=%q sat=%.3f (wins=%d affordances=%d)%s\n",
			actionsTaken, newPreference, newPreference-prevPreference, tester.Current().Name, tester.Satisfaction(frame.Grid),
			tester.Wins(), afford.KnownCount(),
			map[bool]string{true: " <- toward goal, reinforced", false: ""}[preferenceIncreased && !exploredAction && clickedLabel != ""])
		prevPreference = newPreference

		if exploredAction {
			fmt.Printf("action %d: sent %v -> state=%s levels_completed=%d\n",
				actionsTaken, action.ID, frame.State, frame.LevelsCompleted)
		} else {
			fmt.Printf("action %d: click (%d,%d) -> state=%s levels_completed=%d (cohesion this cycle=%.4f)\n",
				actionsTaken, action.X, action.Y, frame.State, frame.LevelsCompleted, res.MaxCohesionObserved)
		}

		if frame.State == environment.StateWin || frame.State == environment.StateGameOver {
			// Register the outcome of the action that just caused this
			// terminal state before stopping. Real bug this fixes: the loop
			// used to check for terminal state at the TOP, before ever
			// calling ChooseClickAction again -- so it broke on the NEXT
			// iteration without the credit-assignment machinery ever seeing
			// the final frame or judging the action that caused WIN/GAME_OVER.
			// The action this call proposes is discarded (the game is over,
			// nothing left to click); only the actualSuccess judgment it
			// computes internally -- whether the terminal frame's perception
			// differs from the pre-terminal one, OR whether this final action
			// completed a level (frame.LevelsCompleted was just reassigned
			// from newFrame above; prevLevelsCompleted still holds the
			// pre-this-action value) -- matters here.
			terminalLevelsCompletedIncreased := frame.LevelsCompleted > prevLevelsCompleted
			if _, finalObs, _, _, regErr := bridge.ChooseClickAction(ctx, engine, frame.Grid, "solve the puzzle", *maxBlobs, *cols, *rows, prevObservation, terminalLevelsCompletedIncreased, *curiosityStep, rand.Float64(), memory, prevClickedLabel, "", nil); regErr != nil {
				fmt.Printf("terminal state %s: outcome registration failed: %v\n", frame.State, regErr)
			} else {
				fmt.Printf("terminal state %s registered (changed since last frame: %v, levels_completed_increased: %v)\n", frame.State, finalObs != prevObservation, terminalLevelsCompletedIncreased)
			}

			// HER: close the episode. Hindsight relabels the states it reached into
			// skills (implanted "I can make the board become X" memories), won or not.
			won := frame.State == environment.StateWin || terminalLevelsCompletedIncreased
			newSkills := engine.HER.EndEpisode(won)
			fmt.Printf("terminal state %s: HER episode ended (won=%v) -> +%d skills, repertoire=%d\n", frame.State, won, newSkills, engine.HER.SkillCount())

			// LEARN FROM THE LOSS, then RETRY: a GAME_OVER is a negative reward.
			// Penalize the action that ended the game and the recent click
			// sequence, then reset and keep playing with the SAME brain -- so
			// the loss actually teaches (die, learn what killed you, avoid it
			// next attempt), instead of throwing the episode away. WIN also
			// resets to keep going within the action budget.
			if frame.State == environment.StateGameOver {
				// Fix 1 -- separate a HAZARD death from a TIMEOUT/budget death. We
				// have no direct "collided with a deadly object" detector, so use
				// the sign of the fatal step's preference delta as the proxy: a
				// NEGATIVE delta means the action made things worse (hazard-like) --
				// penalize it and remember the danger state; a >= 0 delta means the
				// step was progress and the death was non-causal (ran out of the
				// level's step budget while improving, as vc33 did one click from
				// the win with delta=+0.0391) -- penalizing that would punish the
				// RIGHT move and, since click is the only tool here, collapse the
				// agent into negative-preference flailing. So only the harmful case
				// trains aversion.
				if preferenceDelta < 0 {
					engine.PenalizeLastAction(lossActionPenalty)
					memory.PenalizeSequence(lossSequenceStrength)
					engine.RememberDangerState(pipeline.ObservationVector(perception.DescribeGridStructural(preStepGrid, *maxBlobs, *cols, *rows)))
					fmt.Printf("GAME_OVER (hazard-like, delta=%+.4f): penalized fatal action %q + sequence, remembered danger, resetting\n", preferenceDelta, lastActionTok)
				} else {
					fmt.Printf("GAME_OVER (timeout-like, delta=%+.4f >= 0): fatal step was progress -- NOT penalized, resetting\n", preferenceDelta)
				}
			}
			resetFrame, resetErr := sess.Reset()
			if resetErr != nil {
				fmt.Printf("reset after %s failed: %v -- stopping\n", frame.State, resetErr)
				break
			}
			frame = resetFrame
			engine.HER.BeginEpisode(target) // start recording the next attempt
			// The attempt ended without a win -- the hypothesis we were pursuing
			// wasn't (or wasn't reachably) the goal, so try the next candidate on
			// the fresh attempt.
			tester.Refute()
			fmt.Printf("reset: testing next hypothesis %q (wins=%d)\n", tester.Current().Name, tester.Wins())
			prevObservation, prevClickedLabel, lastActionTok, prevNumeric = "", "", "", ""
			tracker = perception.NewObjectTracker()
			prevTopology = ""
			prevLevelsCompleted = frame.LevelsCompleted
			prevPreference = preferenceOf(frame.Grid)
			stagnation = 0
			startStateCaptured = false
			herTarget = nil // re-propose target on fresh attempt
			continue
		}
	}

	fmt.Printf("stopped after %d actions, final state=%s levels_completed=%d\n", actionsTaken, frame.State, frame.LevelsCompleted)

	if summary, err := client.CloseScorecard(ctx, cardID); err != nil {
		log.Printf("close scorecard: %v (scorecard %s left open on the server)", err, cardID)
	} else {
		fmt.Printf("scorecard closed: %+v\n", summary)
	}
}

func hasAction6(actions []environment.ActionID) bool {
	for _, a := range actions {
		if a == environment.Action6 {
			return true
		}
	}
	return false
}

// availableSimpleActions returns the offered "simple" actions (ACTION1-5, no
// X/Y) -- the controls the click-only agent never tries. Click (ACTION6) and
// undo (ACTION7) are deliberately excluded: 6 is what ChooseClickAction
// already handles, and randomly firing undo would mostly just reverse
// progress rather than explore a control.
// simpleActionGainString formats a per-action gain map (preference or
// competence) for the available simple actions in the live diagnostic, so a
// run shows the two signals the planner chooses on.
func simpleActionGainString(gains map[string]float64, simpleByToken map[string]environment.ActionID) string {
	out := ""
	for tok := range simpleByToken {
		out += fmt.Sprintf(" %s=%+.4f", tok, gains[tok])
	}
	return out
}

// lossActionPenalty nudges the fatal action's pragmatic value down on a
// GAME_OVER (moderate -- comparable to the other selection terms, so the
// planner avoids it without permanently killing a usually-useful control).
// lossSequenceStrength is how hard the loss drops the recent click sequence.
const (
	lossActionPenalty    = 0.05
	lossSequenceStrength = 3.0
	// dangerAvoidWeight scales how strongly resemblance to a death state lowers a
	// state's preference (DangerProximity is a cosine in [0,1]). Set ABOVE
	// spatialApproachWeight (0.15) so self-preservation overrides the pull toward
	// a hazardous key -- don't rush into death to reach the goal -- but well below
	// a learned goal's full weight so it deters without freezing the agent into
	// the dark-room "never act" corner (the anti-stagnation drive is the other
	// counterweight). A guess, tunable against a live run.
	dangerAvoidWeight = 0.3
	// refractorySteps is how many steps a target is locked out after a click
	// there changed nothing meaningful (Inhibition of Return). ~4 is the middle
	// of the "3-5 moves" the vc33 post-mortem called for: long enough to break a
	// hammering loop, short enough that a target worth revisiting comes back.
	refractorySteps = 4
	// graphPriorBonus nudges per-object selection toward the object the
	// reconnected graph's WTA chose. Comparable to the optimism bonus (max 0.05)
	// so at cold start (per-object gains ~0) the graph breaks the tie, but small
	// enough that a real learned per-object preference/competence signal overrides
	// it once accrued. A guess, tunable against a live run.
	graphPriorBonus = 0.03
	// hypWeight scales the pursued hypothesis's satisfaction as the goal-directed
	// preference term -- 1.0 so it's the dominant drive (on par with a fully
	// matched LearnedPreference), giving a real gradient to climb toward the
	// current hypothesized goal.
	hypWeight = 1.0
	// pragmaticBeta scales the counterfactual 1-step lookahead (Δsat from acting
	// on an object) injected into that object's click score. >0 large enough that
	// a genuinely goal-advancing click (a real positive Δsat) outweighs the
	// optimism/exploration noise (~0.05); a guess, tunable against a live run.
	pragmaticBeta = 2.0
	// epistemicBonus is the value of clicking an object whose EFFECT is not yet
	// learned -- the motor-babbling drive. Set above the optimism noise (~0.05)
	// so unknown-affordance objects are probed early to build the physics map,
	// then naturally yields to pragmatic Δsat once effects are known.
	epistemicBonus = 0.1
	// rolloutDepth is how many clicks the learned-model lookahead searches ahead
	// (2 = try chains of two clicks) so a preparatory move can be credited for the
	// invariant a follow-up closes -- the fix for the non-monotonic landscape.
	rolloutDepth = 2
	// herBonus nudges selection toward the first action of the shortest HER skill
	// that reaches a win-like state; herMinSim is the min cosine similarity a
	// skill's target must have to the win-state to count. Modest -- HER's payoff
	// is gated on having seen a real win to define the target.
	herBonus  = 0.08
	// residualBonus prioritizes clicking the compression anomaly (the cells that
	// break the best regularity) -- the salient, usually-interactive element in an
	// otherwise-regular world. Strong enough to beat babbling on inert texture, but
	// a genuinely learned high-value class-macro (pragmaticBeta * real Δsavings) can
	// still exceed it once affordances are learned.
	residualBonus = 0.15
	// driveGainWeight scales the model-free observed-compression reinforcement.
	// A vc33 grow click raises savings by ~19; at 0.02 that is a +0.38 bonus that
	// dominates optimism/prior, while a shrink click (~-21) is strongly suppressed,
	// so after one exploratory try of each button the agent locks onto grow. Raw
	// savings scale with grid size, so this may need per-game tuning later.
	driveGainWeight = 0.02
	herMinSim = 0.6
)

// availableSimpleActions returns every offered non-click action (ACTION1-5 and
// ACTION7) -- the keyboard controls the click-only agent never tries. Only
// ACTION6 (the click, which ChooseClickAction handles) and ACTIONRESET are
// excluded. ACTION7 is INCLUDED, not assumed to be useless undo: in a novel
// game we don't know what a control does until we try it.
func availableSimpleActions(actions []environment.ActionID) []environment.ActionID {
	simple := make([]environment.ActionID, 0, len(actions))
	for _, a := range actions {
		if a != environment.Action6 && a != environment.ActionReset {
			simple = append(simple, a)
		}
	}
	return simple
}

// gridChangeMagnitude counts how many pixels differ between two grids.
// Returns 0 if grids are identical or incomparable (different dimensions).
func gridChangeMagnitude(a, b [][]int) int {
	if len(a) != len(b) {
		return len(a) * len(a[0]) // completely different shape = maximum change
	}
	count := 0
	for r := range a {
		if r >= len(b) || len(a[r]) != len(b[r]) {
			return len(a) * len(a[0])
		}
		for c := range a[r] {
			if a[r][c] != b[r][c] {
				count++
			}
		}
	}
	return count
}
