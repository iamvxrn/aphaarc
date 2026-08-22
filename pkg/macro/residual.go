package macro

import (
	"fmt"
	"sort"
	"strings"
)

// shapeKey is a position-independent signature of a component: colour + sorted
// cell offsets relative to the component's top-left.
func shapeKey(color int, cells []Cell) string {
	minR, minC := cells[0].R, cells[0].C
	for _, cl := range cells {
		if cl.R < minR {
			minR = cl.R
		}
		if cl.C < minC {
			minC = cl.C
		}
	}
	offs := make([][2]int, len(cells))
	for i, cl := range cells {
		offs[i] = [2]int{cl.R - minR, cl.C - minC}
	}
	sort.Slice(offs, func(i, j int) bool {
		if offs[i][0] != offs[j][0] {
			return offs[i][0] < offs[j][0]
		}
		return offs[i][1] < offs[j][1]
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "c%d:", color)
	for _, o := range offs {
		fmt.Fprintf(&sb, "%d,%d;", o[0], o[1])
	}
	return sb.String()
}

// Compression residual = attention.
//
// The drive does not only say "this grid is periodic/symmetric"; it says WHICH
// cells the regularity cannot explain. In a mostly-regular world (a periodic
// texture, a symmetric field) those violating cells are the salient, information-
// bearing, usually-interactive elements — the anomaly that breaks the pattern.
// The periodic background is already compressed (boring, high savings); the
// residual is the only place new compression progress — and usually the game's
// interactive control — can live. So the compression drive doubles as an
// attention pointer: click where compression FAILS, not on the texture.

// Cell is a grid coordinate (row, col).
type Cell struct{ R, C int }

// bestHorizPeriod returns the non-trivial horizontal period with the most
// savings (period 0 if none). Mirrors TranslateSavings' horizontal scan.
func bestHorizPeriod(grid [][]int, bg int) (period, savings int) {
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
		if s > savings {
			savings, period = s, p
		}
	}
	return period, savings
}

// bestVertPeriod is the vertical analogue.
func bestVertPeriod(grid [][]int, bg int) (period, savings int) {
	h := len(grid)
	if h == 0 {
		return 0, 0
	}
	for p := 2; p < h; p++ {
		if !vertTileNonTrivial(grid, p) {
			continue
		}
		s := 0
		for r := p; r < h; r++ {
			w := len(grid[r])
			if len(grid[r-p]) < w {
				w = len(grid[r-p])
			}
			for c := 0; c < w; c++ {
				if grid[r][c] != bg && grid[r][c] == grid[r-p][c] {
					s++
				}
			}
		}
		if s > savings {
			savings, period = s, p
		}
	}
	return period, savings
}

// reflectResidual returns cells that violate the best reflection axis.
func reflectResidual(grid [][]int, bg int) []Cell {
	sh, sv := SymmetrySavings(grid, bg)
	var cells []Cell
	if sh >= sv {
		for r := range grid {
			w := len(grid[r])
			for c := 0; c < w/2; c++ {
				mc := w - 1 - c
				if grid[r][c] == grid[r][mc] {
					continue
				}
				if grid[r][c] != bg {
					cells = append(cells, Cell{r, c})
				}
				if grid[r][mc] != bg {
					cells = append(cells, Cell{r, mc})
				}
			}
		}
	} else {
		h := len(grid)
		for r := 0; r < h/2; r++ {
			mr := h - 1 - r
			w := len(grid[r])
			if len(grid[mr]) < w {
				w = len(grid[mr])
			}
			for c := 0; c < w; c++ {
				if grid[r][c] == grid[mr][c] {
					continue
				}
				if grid[r][c] != bg {
					cells = append(cells, Cell{r, c})
				}
				if grid[mr][c] != bg {
					cells = append(cells, Cell{mr, c})
				}
			}
		}
	}
	return cells
}

// majorityColor returns the most frequent color in a tally (deterministic:
// lowest color id wins a tie).
func majorityColor(tally map[int]int) int {
	best, bestN := -1, -1
	for col, n := range tally {
		if n > bestN || (n == bestN && col < best) {
			best, bestN = col, n
		}
	}
	return best
}

// translateResidual returns cells that deviate from the majority tile of the
// best translation period — the anomalies, isolated via a per-residue majority
// vote so the regular cells neighbouring an anomaly are NOT flagged.
func translateResidual(grid [][]int, bg int) []Cell {
	ph, sh := bestHorizPeriod(grid, bg)
	pv, sv := bestVertPeriod(grid, bg)
	var cells []Cell
	if sh == 0 && sv == 0 {
		return cells
	}
	if sh >= sv && ph > 0 {
		// Horizontal period: per row, per residue class, majority-vote the tile.
		for r := range grid {
			w := len(grid[r])
			for k := 0; k < ph; k++ {
				tally := map[int]int{}
				for c := k; c < w; c += ph {
					tally[grid[r][c]]++
				}
				maj := majorityColor(tally)
				for c := k; c < w; c += ph {
					if grid[r][c] != bg && grid[r][c] != maj {
						cells = append(cells, Cell{r, c})
					}
				}
			}
		}
	} else if pv > 0 {
		// Vertical period: per column, per residue class, majority-vote the tile.
		h := len(grid)
		w := minRowLen(grid)
		for c := 0; c < w; c++ {
			for k := 0; k < pv; k++ {
				tally := map[int]int{}
				for r := k; r < h; r += pv {
					if c < len(grid[r]) {
						tally[grid[r][c]]++
					}
				}
				maj := majorityColor(tally)
				for r := k; r < h; r += pv {
					if c < len(grid[r]) && grid[r][c] != bg && grid[r][c] != maj {
						cells = append(cells, Cell{r, c})
					}
				}
			}
		}
	}
	return cells
}

// component is a 4-connected same-colour blob.
type component struct {
	color int
	cells []Cell
	key   string
}

// components extracts 4-connected same-colour foreground components with a
// normalised shape key (colour + sorted offsets).
func components(grid [][]int, bg int) []component {
	h := len(grid)
	if h == 0 {
		return nil
	}
	visited := make([][]bool, h)
	for r := range grid {
		visited[r] = make([]bool, len(grid[r]))
	}
	var comps []component
	for r := 0; r < h; r++ {
		for c := 0; c < len(grid[r]); c++ {
			if grid[r][c] == bg || visited[r][c] {
				continue
			}
			color := grid[r][c]
			var cells []Cell
			stack := []Cell{{r, c}}
			visited[r][c] = true
			for len(stack) > 0 {
				cur := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				cells = append(cells, cur)
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					nr, nc := cur.R+d[0], cur.C+d[1]
					if nr < 0 || nr >= h || nc < 0 || nc >= len(grid[nr]) {
						continue
					}
					if visited[nr][nc] || grid[nr][nc] != color {
						continue
					}
					visited[nr][nc] = true
					stack = append(stack, Cell{nr, nc})
				}
			}
			comps = append(comps, component{color: color, cells: cells, key: shapeKey(color, cells)})
		}
	}
	return comps
}

// countResidual returns the cells of the odd-object-out: objects NOT in the
// largest identical-shape group (the anomaly among repeated objects).
func countResidual(grid [][]int, bg int) []Cell {
	comps := components(grid, bg)
	if len(comps) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, cp := range comps {
		counts[cp.key]++
	}
	// Dominant group = most frequent shape key.
	domKey, domCount := "", 0
	for k, n := range counts {
		if n > domCount {
			domCount, domKey = n, k
		}
	}
	var cells []Cell
	for _, cp := range comps {
		if cp.key != domKey {
			cells = append(cells, cp.cells...)
		}
	}
	return cells
}

// ResidualCells returns the cells that violate the best-compressing primitive —
// the anomalies to attend to. Empty when the grid is incompressible (nothing to
// deviate from) or perfectly regular (no deviation).
func ResidualCells(grid [][]int, bg int) []Cell {
	bp, sav := BestPrimitive(grid, bg)
	if sav == 0 {
		return nil
	}
	var out []Cell
	switch bp.Name {
	case "Reflect":
		out = reflectResidual(grid, bg)
	case "Translate":
		out = translateResidual(grid, bg)
	case "Count":
		out = countResidual(grid, bg)
	case "Correspondence":
		out = correspondenceResidual(grid, bg)
	}
	// Correspondence residual is surfaced ADDITIONALLY even when it is not the
	// argmax primitive. By raw magnitude it is almost never the biggest
	// (Translate/Count typically dominate a whole board), which would
	// otherwise make its click-relevant cells invisible to candidate
	// generation -- the exact aggregation blind spot already fixed for
	// model-free reinforcement (BestPrimitiveDelta in mdl.go), now fixed here
	// for perception/candidate-generation too.
	if bp.Name != "Correspondence" {
		out = append(out, correspondenceResidual(grid, bg)...)
	}
	return out
}

// clusterResidual groups residual cells into 4-connected clusters.
func clusterResidual(cells []Cell) [][]Cell {
	inSet := make(map[Cell]bool, len(cells))
	for _, c := range cells {
		inSet[c] = true
	}
	seen := make(map[Cell]bool, len(cells))
	var clusters [][]Cell
	for _, start := range cells {
		if seen[start] {
			continue
		}
		var cluster []Cell
		stack := []Cell{start}
		seen[start] = true
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cluster = append(cluster, cur)
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				n := Cell{cur.R + d[0], cur.C + d[1]}
				if inSet[n] && !seen[n] {
					seen[n] = true
					stack = append(stack, n)
				}
			}
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

// ResidualPoint is a residual cluster's centroid (col=X, row=Y) plus its size.
type ResidualPoint struct{ X, Y, Size int }

// ResidualTargets returns up to maxN residual-cluster centroids, largest cluster
// first (maxN<=0 = all). A single game control is often a SMALL unique anomaly,
// not the largest texture break, so offering several distinct clusters lets the
// agent probe every anomaly systematically instead of hammering one spot.
func ResidualTargets(grid [][]int, bg, maxN int) []ResidualPoint {
	cells := ResidualCells(grid, bg)
	if len(cells) == 0 {
		return nil
	}
	clusters := clusterResidual(cells)
	sort.Slice(clusters, func(i, j int) bool { return len(clusters[i]) > len(clusters[j]) })
	var out []ResidualPoint
	for _, cl := range clusters {
		if maxN > 0 && len(out) >= maxN {
			break
		}
		sumR, sumC := 0, 0
		for _, c := range cl {
			sumR += c.R
			sumC += c.C
		}
		out = append(out, ResidualPoint{X: sumC / len(cl), Y: sumR / len(cl), Size: len(cl)})
	}
	return out
}

// ResidualTarget returns the centroid of the LARGEST residual cluster (col=x,
// row=y). ok=false when there is no residual.
func ResidualTarget(grid [][]int, bg int) (x, y int, ok bool) {
	pts := ResidualTargets(grid, bg, 1)
	if len(pts) == 0 {
		return 0, 0, false
	}
	return pts[0].X, pts[0].Y, true
}
