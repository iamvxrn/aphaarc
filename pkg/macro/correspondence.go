package macro

import "fmt"

// Primitive #4: Correspondence / Copy (RELATIONAL compression).
//
// Reflect/Translate/Count measure SELF-regularity (a region regular within
// itself). Correspondence measures regularity BETWEEN regions: region A described
// as "a copy of region B, up to a simple transform, plus a residual". This closes
// the copy/match/template class (s5i5's template boxes, tn36's legend, vc33-L2's
// mirror flip) -- puzzles whose solution is ANTI-compression to a self-regularity
// drive (filling a template raises A's own complexity) but compression-POSITIVE
// relationally (A becomes describable as B, so the residual shrinks).
//
// Search stays O(objects), no NP explosion:
//   - ANCHORS are repetitions, found by the SAME component grouping as Count but
//     with a LOOSE signature (bbox WxH + border/component colour) so regions with
//     different interiors still cluster (V1: exact WxH, no scale).
//   - only WITHIN-class members are compared (k small), never all-pairs.
//   - a CONSTANT transform set {Identity, ColorSwap, Reflect(H/V)} -- ColorSwap's
//     mapping is derived by an O(area) majority vote, not searched.
//
// Saving(class) = sum over non-reference members A of
//     area - residual(A, ref) - pointerCost
// so filling one correct cell of a partial pattern drops the residual by 1 and
// lifts the saving by 1 -- the dopamine click on DriveScore.

const correspondencePointerCost = 1 // bits to name the template + anchor (small constant)
const correspondenceMinArea = 4     // a real region, not a dot/line (degenerate-cheat guard)

func cellsBBox(cells []Cell) (minR, minC, maxR, maxC int) {
	minR, minC = cells[0].R, cells[0].C
	maxR, maxC = cells[0].R, cells[0].C
	for _, cl := range cells {
		if cl.R < minR {
			minR = cl.R
		}
		if cl.R > maxR {
			maxR = cl.R
		}
		if cl.C < minC {
			minC = cl.C
		}
		if cl.C > maxC {
			maxC = cl.C
		}
	}
	return
}

// subgridOf extracts the full rectangular content (all colours) of a bbox.
func subgridOf(grid [][]int, minR, minC, maxR, maxC int) [][]int {
	out := make([][]int, maxR-minR+1)
	for r := minR; r <= maxR; r++ {
		row := make([]int, maxC-minC+1)
		for c := minC; c <= maxC; c++ {
			if r < len(grid) && c < len(grid[r]) {
				row[c-minC] = grid[r][c]
			}
		}
		out[r-minR] = row
	}
	return out
}

func distinctColorCount(g [][]int) int {
	seen := map[int]struct{}{}
	for _, row := range g {
		for _, v := range row {
			seen[v] = struct{}{}
		}
	}
	return len(seen)
}

func residualIdentity(a, b [][]int) int {
	res := 0
	for i := range a {
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				res++
			}
		}
	}
	return res
}

func residualReflectH(a, b [][]int) int {
	res := 0
	for i := range a {
		w := len(a[i])
		for j := 0; j < w; j++ {
			if a[i][j] != b[i][w-1-j] {
				res++
			}
		}
	}
	return res
}

func residualReflectV(a, b [][]int) int {
	res := 0
	h := len(a)
	for i := 0; i < h; i++ {
		for j := range a[i] {
			if a[i][j] != b[h-1-i][j] {
				res++
			}
		}
	}
	return res
}

// residualColorSwap derives the majority colour map b->a in O(area) (no search)
// and counts the residual under it. Subsumes Identity when the map is the
// identity, and catches a pure recolouring of the same pattern. bg is pinned to
// bg -- a background hole is a deletion, NOT a recolouring, so ColorSwap may not
// remap bg to a foreground colour (which would hide a missing cell).
// colorSwapMap derives the majority colour map b->a in O(area) (no search).
// bg is pinned to bg -- a background hole is a deletion, NOT a recolouring,
// so ColorSwap may not remap bg to a foreground colour (which would hide a
// missing cell), nor remap a foreground colour onto bg.
func colorSwapMap(a, b [][]int, bg int) map[int]int {
	tally := map[int]map[int]int{}
	for i := range a {
		for j := range a[i] {
			bc, ac := b[i][j], a[i][j]
			if bc == bg {
				continue
			}
			if tally[bc] == nil {
				tally[bc] = map[int]int{}
			}
			tally[bc][ac]++
		}
	}
	m := map[int]int{bg: bg}
	for bc, counts := range tally {
		best, bn := bc, -1
		for ac, n := range counts {
			if ac == bg {
				continue
			}
			if n > bn || (n == bn && ac < best) {
				best, bn = ac, n
			}
		}
		m[bc] = best
	}
	return m
}

// residualColorSwap counts the residual under colorSwapMap's derived mapping.
// Subsumes Identity when the map is the identity, and catches a pure
// recolouring of the same pattern.
func residualColorSwap(a, b [][]int, bg int) int {
	m := colorSwapMap(a, b, bg)
	res := 0
	for i := range a {
		for j := range a[i] {
			if a[i][j] != m[b[i][j]] {
				res++
			}
		}
	}
	return res
}

// minResidual is the fewest cells A needs to differ from B under any allowed
// transform {Identity, ColorSwap, Reflect-H, Reflect-V}.
func minResidual(a, b [][]int, bg int) int {
	best := residualIdentity(a, b)
	for _, r := range []int{residualColorSwap(a, b, bg), residualReflectH(a, b), residualReflectV(a, b)} {
		if r < best {
			best = r
		}
	}
	return best
}

// The *Cells variants below mirror residualIdentity/residualReflectH/
// residualReflectV/residualColorSwap exactly, but return the LOCAL (row,col)
// positions inside `a` that differ, instead of just a count -- the click
// candidates for correspondenceResidual. Kept separate from the hot-path
// count-only functions (called once per grid, every candidate primitive
// comparison) to avoid allocating cell slices there.

func residualIdentityCells(a, b [][]int) []Cell {
	var out []Cell
	for i := range a {
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				out = append(out, Cell{R: i, C: j})
			}
		}
	}
	return out
}

func residualReflectHCells(a, b [][]int) []Cell {
	var out []Cell
	for i := range a {
		w := len(a[i])
		for j := 0; j < w; j++ {
			if a[i][j] != b[i][w-1-j] {
				out = append(out, Cell{R: i, C: j})
			}
		}
	}
	return out
}

func residualReflectVCells(a, b [][]int) []Cell {
	var out []Cell
	h := len(a)
	for i := 0; i < h; i++ {
		for j := range a[i] {
			if a[i][j] != b[h-1-i][j] {
				out = append(out, Cell{R: i, C: j})
			}
		}
	}
	return out
}

func residualColorSwapCells(a, b [][]int, bg int) []Cell {
	m := colorSwapMap(a, b, bg)
	var out []Cell
	for i := range a {
		for j := range a[i] {
			if a[i][j] != m[b[i][j]] {
				out = append(out, Cell{R: i, C: j})
			}
		}
	}
	return out
}

// minResidualCells returns the local (row,col) positions inside `a` that
// differ from `b` under whichever transform gives the FEWEST differences --
// matching minResidual's choice exactly, so this is "the cells that need to
// change for `a` to become a copy of `b`" under the same transform the
// savings computation already picked.
func minResidualCells(a, b [][]int, bg int) []Cell {
	best := residualIdentityCells(a, b)
	for _, cells := range [][]Cell{residualColorSwapCells(a, b, bg), residualReflectHCells(a, b), residualReflectVCells(a, b)} {
		if len(cells) < len(best) {
			best = cells
		}
	}
	return best
}

// nonBgCount counts non-background cells in a region.
func nonBgCount(g [][]int, bg int) int {
	n := 0
	for _, row := range g {
		for _, v := range row {
			if v != bg {
				n++
			}
		}
	}
	return n
}

// correspondenceRegion is one anchor member's bbox in absolute grid coords.
type correspondenceRegion struct{ minR, minC, maxR, maxC int }

// correspondenceGroups finds ANCHORS: components grouped by the loose
// (W, H, colour) signature (V1: exact WxH, no scale) -- the same grouping
// CorrespondenceSavings and correspondenceResidual both need.
func correspondenceGroups(grid [][]int, bg int) map[string][]correspondenceRegion {
	comps := components(grid, bg)
	if len(comps) < 2 {
		return nil
	}
	groups := map[string][]correspondenceRegion{}
	for _, cp := range comps {
		minR, minC, maxR, maxC := cellsBBox(cp.cells)
		w, h := maxC-minC+1, maxR-minR+1
		if w*h < correspondenceMinArea {
			continue
		}
		key := fmt.Sprintf("%dx%d:c%d", w, h, cp.color)
		groups[key] = append(groups[key], correspondenceRegion{minR, minC, maxR, maxC})
	}
	return groups
}

// CorrespondenceSavings sums the description saved by describing repeated
// same-size regions as copies of one representative (up to a simple transform).
func CorrespondenceSavings(grid [][]int, bg int) int {
	groups := correspondenceGroups(grid, bg)
	savings := 0
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		// Extract each member's full bbox content; the reference is the FULLEST
		// member (most non-bg cells) so a partial copy is scored against the
		// template, not vice-versa (a bg hole must not become the reference and get
		// remapped away).
		grids := make([][][]int, len(members))
		refIdx, refFull := 0, -1
		for i, m := range members {
			grids[i] = subgridOf(grid, m.minR, m.minC, m.maxR, m.maxC)
			if full := nonBgCount(grids[i], bg); full > refFull {
				refFull, refIdx = full, i
			}
		}
		ref := grids[refIdx]
		if distinctColorCount(ref) < 2 {
			continue // reference must be a real pattern, not a solid block (cheat guard)
		}
		area := len(ref) * len(ref[0])
		for i, g := range grids {
			if i == refIdx {
				continue
			}
			s := area - minResidual(g, ref, bg) - correspondencePointerCost
			if s > 0 {
				savings += s
			}
		}
	}
	return savings
}

// correspondenceResidual returns the ABSOLUTE grid cells that currently
// disagree with their class's reference (template) under the best-fitting
// transform -- the cells a click would need to fix to raise
// CorrespondenceSavings. This is the click-candidate feed for ResidualCells:
// Reflect/Translate/Count's residual is a DIFFERENT notion of anomaly
// (self-regularity broken), which routinely does NOT include the cell that
// matters for a relational/template puzzle (tn36's legend, s5i5's boxes, and
// the same pattern found live on ft09-0d8bbf25: Correspondence savings=89,
// nonzero, but zero candidate clicks ever targeted it in 150 real actions,
// because ResidualCells never had a case for Correspondence at all).
func correspondenceResidual(grid [][]int, bg int) []Cell {
	groups := correspondenceGroups(grid, bg)
	var out []Cell
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		grids := make([][][]int, len(members))
		refIdx, refFull := 0, -1
		for i, m := range members {
			grids[i] = subgridOf(grid, m.minR, m.minC, m.maxR, m.maxC)
			if full := nonBgCount(grids[i], bg); full > refFull {
				refFull, refIdx = full, i
			}
		}
		ref := grids[refIdx]
		if distinctColorCount(ref) < 2 {
			continue
		}
		for i, g := range grids {
			if i == refIdx {
				continue
			}
			for _, c := range minResidualCells(g, ref, bg) {
				out = append(out, Cell{R: members[i].minR + c.R, C: members[i].minC + c.C})
			}
		}
	}
	return out
}

// CorrespondencePreference is the compression saving under the Copy primitive.
func CorrespondencePreference(grid [][]int, bg int) int {
	return CorrespondenceSavings(grid, bg)
}
