package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"alphaarc/pkg/environment"
	"alphaarc/pkg/environment/perception"
	"alphaarc/pkg/environment/remote"
	"alphaarc/pkg/macro"
)

func render(grid [][]int) {
	glyph := func(v int) byte {
		switch {
		case v == 0:
			return '.'
		case v < 10:
			return byte('0' + v)
		default:
			return byte('a' + (v - 10))
		}
	}
	for _, row := range grid {
		b := make([]byte, len(row))
		for c, v := range row {
			b[c] = glyph(v)
		}
		fmt.Println(string(b))
	}
}

func colorCounts(grid [][]int) map[int]int {
	m := map[int]int{}
	for _, row := range grid {
		for _, v := range row {
			m[v]++
		}
	}
	return m
}

func changedCells(a, b [][]int) int {
	n := 0
	for r := range a {
		for c := range a[r] {
			if c < len(b[r]) && a[r][c] != b[r][c] {
				n++
			}
		}
	}
	return n
}

func main() {
	ctx := context.Background()
	game := os.Getenv("GAME")
	if game == "" {
		game = "vc33-5430563c"
	}
	client, err := remote.NewClientFromEnv()
	if err != nil {
		fmt.Println("client:", err)
		os.Exit(1)
	}
	card, err := client.OpenScorecard(ctx, []string{"click"})
	if err != nil {
		fmt.Println("scorecard:", err)
		os.Exit(1)
	}
	defer client.CloseScorecard(ctx, card)
	sess := remote.NewSession(client, game, card)
	f, err := sess.Reset()
	if err != nil {
		fmt.Println("reset:", err)
		os.Exit(1)
	}
	bg := perception.BackgroundColor(f.Grid)
	fmt.Printf("=== %s  %dx%d  state=%s levels=%d actions=%v ===\n",
		game, len(f.Grid[0]), len(f.Grid), f.State, f.LevelsCompleted, f.AvailableActions)
	if os.Getenv("RENDER") != "" {
		render(f.Grid)
	}
	bp, sav := macro.BestPrimitive(f.Grid, bg)
	pts := macro.ResidualTargets(f.Grid, bg, 12)
	fmt.Printf("colors=%v bg=%d\n", colorCounts(f.Grid), bg)
	fmt.Printf("drive: best=%s savings=%d score=%.3f residualClusters=%d\n", bp.Name, sav, macro.DriveScore(f.Grid, bg), len(pts))
	fmt.Print("static per-primitive savings: ")
	for _, p := range macro.Primitives {
		fmt.Printf("%s=%d  ", p.Name, p.Savings(f.Grid, bg))
	}
	fmt.Println()

	// Residual-centroid sanity check: ResidualTargets clicks the MEAN position
	// of each residual cluster. For a convex blob the mean lands inside it, but
	// for a hollow/ring/non-convex anomaly shape the centroid can land on a
	// background cell that isn't part of the anomaly at all -- a plausible
	// candidate-generation gap distinct from the primitive/reinforcement layer.
	if resCells := macro.ResidualCells(f.Grid, bg); len(resCells) > 0 {
		cellSet := make(map[macro.Cell]bool, len(resCells))
		for _, c := range resCells {
			cellSet[c] = true
		}
		onCell, offCell := 0, 0
		for _, pt := range pts {
			if cellSet[macro.Cell{R: pt.Y, C: pt.X}] {
				onCell++
			} else {
				offCell++
				fmt.Printf("  residual centroid MISS: (%d,%d) size=%d is NOT itself a residual cell\n", pt.X, pt.Y, pt.Size)
			}
		}
		fmt.Printf("residual centroid check: %d/%d targets land ON an actual residual cell, %d miss\n", onCell, onCell+offCell, offCell)
		fmt.Println("residual targets:", pts)
	}

	corrResCells := macro.CorrespondenceResidual(f.Grid, bg)
	fmt.Printf("CorrespondenceResidual: %d cells: %v\n", len(corrResCells), corrResCells)

	// Would a Correspondence-relevant cluster survive the LIVE agent's
	// ResidualTargets(grid, bg, 8) top-8-by-cluster-SIZE cutoff? If
	// Correspondence's cells form small clusters buried behind big
	// self-regularity clusters, the candidate-generation fix (ResidualCells
	// now includes them) is necessary but not sufficient live.
	allTargets := macro.ResidualTargets(f.Grid, bg, 0) // 0 = all clusters, unranked cutoff
	top8 := macro.ResidualTargets(f.Grid, bg, 8)
	fmt.Printf("ALL residual clusters (%d total, sizes): ", len(allTargets))
	for _, t := range allTargets {
		fmt.Printf("%d ", t.Size)
	}
	fmt.Printf("\ntop-8 cutoff (what the live agent actually sees): %v\n", top8)

	// POINTS mode: instead of a grid sweep, probe exact "x,y;x,y;..." coordinates
	// and print the FULL per-primitive delta for each -- for comparing a known-good
	// click against a suspected false-positive click side by side.
	if pointsEnv := os.Getenv("POINTS"); pointsEnv != "" {
		for _, pair := range strings.Split(pointsEnv, ";") {
			var x, y int
			if _, err := fmt.Sscanf(pair, "%d,%d", &x, &y); err != nil {
				fmt.Printf("bad point %q: %v\n", pair, err)
				continue
			}
			f0, _ := sess.Reset()
			f1, err := sess.Step(environment.Action{ID: environment.Action6, X: x, Y: y})
			if err != nil {
				fmt.Println("step:", err)
				continue
			}
			nc := changedCells(f0.Grid, f1.Grid)
			fmt.Printf("POINT (%d,%d): cells=%d levels=%d state=%s | ", x, y, nc, f1.LevelsCompleted, f1.State)
			for _, p := range macro.Primitives {
				fmt.Printf("%s: %d->%d (%+d)  ", p.Name, p.Savings(f0.Grid, bg), p.Savings(f1.Grid, bg), p.Savings(f1.Grid, bg)-p.Savings(f0.Grid, bg))
			}
			fmt.Println()
		}
		return
	}

	// Step sweep: reset before each probe, click, report board-changing clicks with
	// the compression delta and whether they complete a level. Answers: is the game
	// interactive at all, and does the drive point toward the solving click?
	step := 4
	if s := os.Getenv("STEP"); s == "8" {
		step = 8
	}
	h := len(f.Grid)
	w := len(f.Grid[0])
	changers, solvers, posDelta, negDelta := 0, 0, 0, 0
	bestDelta, bestX, bestY := -1<<30, -1, -1
	corrBest, corrX, corrY := -1<<30, -1, -1
	// Per-primitive breakdown: for each primitive, the largest single-click delta
	// seen and where. This is the diagnostic for "does BestPrimitiveDelta (max
	// over primitives) get hijacked by a small/volatile primitive's noise instead
	// of the real target primitive's genuine, larger-but-not-max delta?" -- the
	// question raised by the vc33 regression after switching model-free
	// reinforcement from DrivePreference-diff to BestPrimitiveDelta.
	primMax := make([]int, len(macro.Primitives))
	primMaxAt := make([][2]int, len(macro.Primitives))
	for i := range primMax {
		primMax[i] = -1 << 30
	}
	for y := 0; y < h; y += step {
		for x := 0; x < w; x += step {
			f0, _ := sess.Reset()
			f1, err := sess.Step(environment.Action{ID: environment.Action6, X: x, Y: y})
			if err != nil {
				fmt.Println("step:", err)
				return
			}
			nc := changedCells(f0.Grid, f1.Grid)
			lv := f1.LevelsCompleted > f0.LevelsCompleted
			term := f1.State != environment.StateNotFinished
			if nc == 0 && !lv && !term {
				continue
			}
			changers++
			_, s0 := macro.BestPrimitive(f0.Grid, bg)
			_, s1 := macro.BestPrimitive(f1.Grid, bg)
			d := s1 - s0
			// also track the Correspondence-specific delta -- does the relational
			// primitive move for ANY click, even where BestPrimitive (max) hides it?
			cd := macro.CorrespondenceSavings(f1.Grid, bg) - macro.CorrespondenceSavings(f0.Grid, bg)
			if cd > corrBest {
				corrBest, corrX, corrY = cd, x, y
			}
			for i, p := range macro.Primitives {
				pd := p.Savings(f1.Grid, bg) - p.Savings(f0.Grid, bg)
				if pd > primMax[i] {
					primMax[i], primMaxAt[i] = pd, [2]int{x, y}
				}
			}
			if d > 0 {
				posDelta++
			} else if d < 0 {
				negDelta++
			}
			if d > bestDelta {
				bestDelta, bestX, bestY = d, x, y
			}
			if lv || term {
				solvers++
				fmt.Printf("  SOLVER click (%d,%d): cells=%d dSavings=%+d levels=%d state=%s\n", x, y, nc, d, f1.LevelsCompleted, f1.State)
			}
		}
	}
	fmt.Printf("SWEEP(step %d): board-changers=%d  level-solvers=%d  posDelta=%d negDelta=%d  bestDelta=%+d@(%d,%d)  bestCorrespondenceDelta=%+d@(%d,%d)\n",
		step, changers, solvers, posDelta, negDelta, bestDelta, bestX, bestY, corrBest, corrX, corrY)
	fmt.Print("per-primitive best delta: ")
	for i, p := range macro.Primitives {
		fmt.Printf("%s=%+d@(%d,%d)  ", p.Name, primMax[i], primMaxAt[i][0], primMaxAt[i][1])
	}
	fmt.Println()
}
