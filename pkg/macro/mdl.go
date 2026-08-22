package macro

// Structural MDL — the Reflect slice.
//
// This is the first primitive of a compression-as-goal drive: instead of an
// asserted "symmetry is good" scorer, we measure how many foreground cells a
// grid gets "for free" once you are allowed to describe it as
//
//     one half  +  a Reflect opcode  +  the residual that breaks the symmetry
//
// The signal is compression SAVINGS = L_full - L_reflect, i.e. the number of
// cells the Reflect primitive explains away. Two consequences make this the
// honest, dark-room-proof formulation:
//
//   1. Blank / erased grids save nothing (no foreground to mirror) => savings 0.
//      So the drive never rewards deleting structure to reach a short state.
//   2. Reflect is not privileged as a goal. It is one grammar primitive; a
//      translate or repeat primitive would count its own savings the same way.
//      Symmetry only "matters" in a given game to the extent it compresses that
//      game's grids. When more primitives exist, none is hand-picked — the agent
//      pursues whichever pays the most bits. (With a single primitive, this slice
//      proves the DELTA-L mechanism; the fully emergent, no-scorer claim lands
//      once a second primitive competes with this one.)
//
// Cost model for the slice: 1 unit per foreground cell (color is matched
// exactly, so it is folded into the equality test rather than priced separately).

// SymmetrySavings returns how many foreground cells are explained for free by
// horizontal and by vertical reflection respectively.
//
// Horizontal axis mirrors column c <-> W-1-c; a right-half cell that is
// foreground AND equals its mirror partner is determined by the left half, so it
// costs nothing to store => 1 unit saved. Vertical axis mirrors row r <-> H-1-r
// symmetrically. Center column/row (odd size) maps to itself and is skipped: it
// carries no pair, hence no saving.
func SymmetrySavings(grid [][]int, bg int) (savingsH, savingsV int) {
	h := len(grid)
	if h == 0 {
		return 0, 0
	}

	// Horizontal reflection: for each row, pair column c with its mirror.
	for r := 0; r < h; r++ {
		w := len(grid[r])
		for c := 0; c < w/2; c++ {
			mc := w - 1 - c
			// Count the RIGHT-side cell when it is foreground and matches the
			// mirror of the left, so the left half determines it.
			if grid[r][mc] != bg && grid[r][mc] == grid[r][c] {
				savingsH++
			}
		}
	}

	// Vertical reflection: pair row r with row h-1-r.
	for r := 0; r < h/2; r++ {
		mr := h - 1 - r
		w := len(grid[r])
		if len(grid[mr]) < w {
			w = len(grid[mr])
		}
		for c := 0; c < w; c++ {
			if grid[mr][c] != bg && grid[mr][c] == grid[r][c] {
				savingsV++
			}
		}
	}
	return savingsH, savingsV
}

// SymmetryPreference is the single scalar drive for the Reflect slice: the best
// compression saving available under either reflection axis. Maximizing it is
// equivalent to minimizing structural description length via the Reflect
// primitive. It is 0 on a blank grid and rises only when real foreground
// structure becomes mirror-consistent, so hill-climbing on it pushes toward
// symmetric STRUCTURE, never toward an empty canvas.
func SymmetryPreference(grid [][]int, bg int) int {
	sh, sv := SymmetrySavings(grid, bg)
	if sh > sv {
		return sh
	}
	return sv
}

// --- Primitive #2: Translate / Repeat (periodicity) ---
//
// A grid with a repeating tile can be described as one tile + a Repeat opcode:
// a cell at distance p from an equal earlier cell is "explained" by the period.
// Savings = foreground cells determined by the best period.
//
// Two honest guards distinguish real periodic STRUCTURE from the dark room's
// periodic door:
//   - period p >= 2 only (p == 1 is "every cell equals its neighbour" = uniform
//     fill, not a tile);
//   - the repeating tile must be NON-TRIVIAL (>= 2 distinct values in the first
//     band), so a solid single-colour region — which is trivially periodic at
//     every period — earns ZERO translate savings. Reflection needs contrast to
//     pay off; translation does not, so without this guard a free optimiser would
//     "compress" by flooding the canvas one colour. The guard makes Translate
//     reward patterns, not floods.

func minRowLen(grid [][]int) int {
	w := -1
	for _, row := range grid {
		if w < 0 || len(row) < w {
			w = len(row)
		}
	}
	if w < 0 {
		return 0
	}
	return w
}

// horizTileNonTrivial reports whether columns [0,p) contain >= 2 distinct values.
func horizTileNonTrivial(grid [][]int, p int) bool {
	seen := map[int]struct{}{}
	for _, row := range grid {
		for c := 0; c < p && c < len(row); c++ {
			seen[row[c]] = struct{}{}
			if len(seen) >= 2 {
				return true
			}
		}
	}
	return false
}

// vertTileNonTrivial reports whether rows [0,p) contain >= 2 distinct values.
func vertTileNonTrivial(grid [][]int, p int) bool {
	seen := map[int]struct{}{}
	for r := 0; r < p && r < len(grid); r++ {
		for _, v := range grid[r] {
			seen[v] = struct{}{}
			if len(seen) >= 2 {
				return true
			}
		}
	}
	return false
}

// TranslateSavings returns the foreground cells explained for free by the best
// non-trivial horizontal period and by the best non-trivial vertical period.
func TranslateSavings(grid [][]int, bg int) (savingsH, savingsV int) {
	h := len(grid)
	if h == 0 {
		return 0, 0
	}
	w := minRowLen(grid)

	for p := 2; p < w; p++ {
		if !horizTileNonTrivial(grid, p) {
			continue
		}
		s := 0
		for r := 0; r < h; r++ {
			for c := p; c < len(grid[r]); c++ {
				if grid[r][c] != bg && grid[r][c] == grid[r][c-p] {
					s++
				}
			}
		}
		if s > savingsH {
			savingsH = s
		}
	}

	for p := 2; p < h; p++ {
		if !vertTileNonTrivial(grid, p) {
			continue
		}
		s := 0
		for r := p; r < h; r++ {
			w2 := len(grid[r])
			if len(grid[r-p]) < w2 {
				w2 = len(grid[r-p])
			}
			for c := 0; c < w2; c++ {
				if grid[r][c] != bg && grid[r][c] == grid[r-p][c] {
					s++
				}
			}
		}
		if s > savingsV {
			savingsV = s
		}
	}
	return savingsH, savingsV
}

// TranslatePreference is the best compression saving available under the
// Repeat primitive (either axis). 0 on a blank or single-colour grid.
func TranslatePreference(grid [][]int, bg int) int {
	sh, sv := TranslateSavings(grid, bg)
	if sh > sv {
		return sh
	}
	return sv
}

// --- Emergent primitive selection ---
//
// A Primitive is one grammar operator paired with its compression-savings
// measure. The agent has NO hand-picked goal: for a given grid it scores every
// primitive and pursues the one that saves the most bits. Adding a new primitive
// = appending here; nothing marks any of them as "the" goal. Symmetry, periodicity
// and (later) counting/topology all compete on the single currency of bits saved,
// and which one "matters" is decided per grid by the data.
type Primitive struct {
	Name    string
	Savings func(grid [][]int, bg int) int
}

// Primitives is the current Core-Knowledge grammar: geometry (Reflect, Translate)
// and number (Count). None is privileged as a goal — they compete on bits saved.
var Primitives = []Primitive{
	{Name: "Reflect", Savings: SymmetryPreference},
	{Name: "Translate", Savings: TranslatePreference},
	{Name: "Count", Savings: NumerosityPreference},
	{Name: "Correspondence", Savings: CorrespondencePreference},
}

// BestPrimitive returns the primitive that compresses grid the most, and its
// saving. On a tie the earlier-listed primitive wins; a saving of 0 means no
// primitive found exploitable structure (the grid is incompressible under the
// current grammar — a signal to grow it, not to assert a goal anyway).
func BestPrimitive(grid [][]int, bg int) (Primitive, int) {
	best := Primitives[0]
	bestSave := best.Savings(grid, bg)
	for _, p := range Primitives[1:] {
		if s := p.Savings(grid, bg); s > bestSave {
			best, bestSave = p, s
		}
	}
	return best, bestSave
}

// BestPrimitiveDelta returns the largest single-primitive compression gain
// between before and after: max over primitives p of Savings_p(after) -
// Savings_p(before). This is deliberately NOT DrivePreference(after) -
// DrivePreference(before) — that diffs two possibly-DIFFERENT argmax
// primitives, so a big dominant primitive (e.g. a whole-background Translate)
// can swamp a smaller primitive's real local improvement (e.g. Correspondence
// matching one template region) out of the delta entirely. Crediting the
// best-IMPROVING axis instead of the best-ABSOLUTE axis lets model-free
// reinforcement (cmd/alphaarc-play) notice a click that helps a locally
// dominated primitive even while a bigger primitive's savings stay flat or
// dip — the aggregation fix for the s5i5/tn36 Correspondence wall.
func BestPrimitiveDelta(before, after [][]int, bg int) int {
	best := Primitives[0].Savings(after, bg) - Primitives[0].Savings(before, bg)
	for _, p := range Primitives[1:] {
		if d := p.Savings(after, bg) - p.Savings(before, bg); d > best {
			best = d
		}
	}
	return best
}
