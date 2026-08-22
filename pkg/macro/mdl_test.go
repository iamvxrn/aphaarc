package macro

import "testing"

// countForeground counts non-bg cells.
func countForeground(grid [][]int, bg int) int {
	n := 0
	for _, row := range grid {
		for _, v := range row {
			if v != bg {
				n++
			}
		}
	}
	return n
}

// isHSymmetric reports whether the grid equals its horizontal mirror.
func isHSymmetric(grid [][]int) bool {
	for _, row := range grid {
		w := len(row)
		for c := 0; c < w/2; c++ {
			if row[c] != row[w-1-c] {
				return false
			}
		}
	}
	return true
}

func cloneGrid(g [][]int) [][]int {
	out := make([][]int, len(g))
	for i := range g {
		out[i] = make([]int, len(g[i]))
		copy(out[i], g[i])
	}
	return out
}

// P1: an empty grid saves nothing — the dark room is not attractive.
func TestReflect_BlankSavesNothing(t *testing.T) {
	blank := [][]int{
		{0, 0, 0, 0},
		{0, 0, 0, 0},
	}
	if p := SymmetryPreference(blank, 0); p != 0 {
		t.Fatalf("blank grid must save 0 bits, got %d", p)
	}
}

// P2: a perfectly H-symmetric grid saves one unit per mirrored right-half cell.
func TestReflect_SymmetricSaves(t *testing.T) {
	g := [][]int{
		{1, 0, 0, 1},
		{2, 3, 3, 2},
	}
	sh, sv := SymmetrySavings(g, 0)
	// Row0: only the outer pair (1,1) is fg-matched -> 1. Row1: both pairs
	// (2,2) and (3,3) matched -> 2. Total H savings = 3.
	if sh != 3 {
		t.Fatalf("expected H savings 3, got %d", sh)
	}
	if sv != 0 {
		t.Fatalf("expected V savings 0 (rows differ), got %d", sv)
	}
	if p := SymmetryPreference(g, 0); p != 3 {
		t.Fatalf("expected preference 3, got %d", p)
	}
}

// P3: breaking a mirrored pair drops the saving by exactly 1; restoring it lifts
// it back by 1. The signal is a smooth gradient in both directions.
func TestReflect_GradientBothWays(t *testing.T) {
	g := [][]int{{1, 0, 0, 1}, {2, 3, 3, 2}}
	base := SymmetryPreference(g, 0)

	broken := cloneGrid(g)
	broken[1][3] = 5 // breaks the (2,2) pair on row1
	if got := SymmetryPreference(broken, 0); got != base-1 {
		t.Fatalf("breaking one pair should drop savings by 1: base=%d got=%d", base, got)
	}

	restored := cloneGrid(broken)
	restored[1][3] = 2 // restore
	if got := SymmetryPreference(restored, 0); got != base {
		t.Fatalf("restoring should recover savings: base=%d got=%d", base, got)
	}
}

// P4: erasing a mirrored foreground cell REDUCES savings — the drive disfavors
// deleting structure (dark-room-proof at the gradient level, not just at 0).
func TestReflect_ErasingIsPunished(t *testing.T) {
	g := [][]int{{1, 0, 0, 1}, {2, 3, 3, 2}}
	base := SymmetryPreference(g, 0)
	erased := cloneGrid(g)
	erased[1][2] = 0 // erase one cell of a mirrored pair
	if got := SymmetryPreference(erased, 0); got >= base {
		t.Fatalf("erasing structure must lower savings: base=%d got=%d", base, got)
	}
}

// P5 (the real proof): a greedy hill-climber that optimizes ONLY
// SymmetryPreference — with no symmetry scorer, no target grid, no reward —
// recovers full symmetry from a perturbed grid, and never collapses to blank.
func TestReflect_DeltaLDriveRecoversSymmetry(t *testing.T) {
	bg := 0
	palette := []int{0, 1, 2, 3}

	// Start from a symmetric grid, then perturb a few cells so the climb has a
	// gradient to follow (single-cell fixes each pay +1).
	start := [][]int{
		{1, 2, 2, 1},
		{3, 1, 1, 3},
		{2, 3, 3, 2},
	}
	grid := cloneGrid(start)
	grid[0][3] = 3 // break pair (1,1)
	grid[2][0] = 1 // break pair (2,2)

	const maxIters = 200
	for iter := 0; iter < maxIters; iter++ {
		best := SymmetryPreference(grid, bg)
		bestR, bestC, bestV := -1, -1, -1
		// Best single-cell edit that increases the compression saving.
		for r := range grid {
			for c := range grid[r] {
				orig := grid[r][c]
				for _, v := range palette {
					if v == orig {
						continue
					}
					grid[r][c] = v
					if s := SymmetryPreference(grid, bg); s > best {
						best, bestR, bestC, bestV = s, r, c, v
					}
				}
				grid[r][c] = orig
			}
		}
		if bestR == -1 {
			break // local optimum reached
		}
		grid[bestR][bestC] = bestV
	}

	if !isHSymmetric(grid) {
		t.Fatalf("delta-L drive failed to reach H-symmetry:\n%v", grid)
	}
	if countForeground(grid, bg) == 0 {
		t.Fatalf("delta-L drive collapsed to a blank grid (dark room)")
	}
	// It must not have erased structure to cheat: at least the original
	// foreground mass survives.
	if got, want := countForeground(grid, bg), countForeground(start, bg); got < want {
		t.Fatalf("drive destroyed structure: fg=%d < original=%d", got, want)
	}
}

// TestAggregation_BestPrimitiveDeltaSurvivesDominantPrimitive is the offline
// proof for the aggregation fix (2026-08-22): a click that only improves a
// SMALL primitive's savings (Correspondence) must still register a positive
// reward even while a BIG, unchanged primitive (a whole-background Translate
// stripe) dominates the absolute DrivePreference of both frames -- the exact
// failure mode found live on s5i5/tn36, where Correspondence's local template
// match never outweighs the background's Translate savings.
func TestAggregation_BestPrimitiveDeltaSurvivesDominantPrimitive(t *testing.T) {
	bg := 9
	w, h := 30, 16

	// Dominant background: a period-2 horizontal stripe over columns [0,16),
	// far bigger in raw savings than the Correspondence pair placed to its
	// right (mirrors TestEmergence_SelectsCorrespondence's placement, shifted).
	full := bgGrid(h, w, bg)
	for r := 0; r < h; r++ {
		for c := 0; c < 16; c++ {
			if c%2 == 0 {
				full[r][c] = 5
			} else {
				full[r][c] = 6
			}
		}
	}
	place(full, 1, 17, symBox)
	place(full, 9, 22, symBox)

	// partial: knock out one interior cell of the second box (top-left (9,22),
	// so its center is (11,24)), same as TestCorrespondence_FillRaisesSaving --
	// the click "fills" it back in.
	partial := cloneGrid(full)
	partial[11][24] = bg

	corrBefore := CorrespondencePreference(partial, bg)
	corrAfter := CorrespondencePreference(full, bg)
	trBefore := TranslatePreference(partial, bg)
	trAfter := TranslatePreference(full, bg)
	t.Logf("Correspondence: before=%d after=%d | Translate: before=%d after=%d", corrBefore, corrAfter, trBefore, trAfter)

	if corrAfter <= corrBefore {
		t.Fatalf("filling the cell must raise Correspondence savings: before=%d after=%d", corrBefore, corrAfter)
	}
	if trBefore <= corrAfter {
		t.Fatalf("test setup must have Translate DOMINATE Correspondence in absolute size: translate=%d correspondence(after)=%d", trBefore, corrAfter)
	}
	if trBefore != trAfter {
		t.Fatalf("test setup requires Translate UNCHANGED by the click, so it fully masks the naive diff: before=%d after=%d", trBefore, trAfter)
	}

	// The naive diff -- DrivePreference(after) - DrivePreference(before) --
	// only reflects the argmax primitive's OWN delta. Translate dominates and
	// is unchanged in both frames, so BestPrimitive picks it both times and
	// the naive diff is swamped to 0, even though Correspondence genuinely
	// improved. This is the aggregation bug cmd/alphaarc-play hit live.
	if naive := DrivePreference(full, bg) - DrivePreference(partial, bg); naive != 0 {
		t.Fatalf("test setup expected the naive DrivePreference diff to be swamped to 0, got %d", naive)
	}

	// BestPrimitiveDelta must see through the dominant primitive and credit
	// the click for the Correspondence improvement it actually caused.
	got := BestPrimitiveDelta(partial, full, bg)
	if want := corrAfter - corrBefore; got != want {
		t.Fatalf("BestPrimitiveDelta should equal the best-improving primitive's own delta: got=%d want=%d", got, want)
	}
	if got <= 0 {
		t.Fatalf("BestPrimitiveDelta must be positive when a non-dominant primitive genuinely improves, got %d", got)
	}
}
