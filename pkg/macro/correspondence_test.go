package macro

import "testing"

// bgGrid makes an h x w grid filled with bg.
func bgGrid(h, w, bg int) [][]int {
	g := make([][]int, h)
	for r := range g {
		g[r] = make([]int, w)
		for c := range g[r] {
			g[r][c] = bg
		}
	}
	return g
}

// place stamps box's cells into grid at (top,left).
func place(grid [][]int, top, left int, box [][]int) {
	for r := range box {
		for c := range box[r] {
			grid[top+r][left+c] = box[r][c]
		}
	}
}

// symBox: a 5x5 color-2 frame with a repeated 4/3 interior (each colour occurs
// several times, so a single missing cell yields residual 1, not a color-swap
// remap). Interior cells are diagonally isolated -> not their own components.
var symBox = [][]int{
	{2, 2, 2, 2, 2},
	{2, 4, 3, 4, 2},
	{2, 3, 4, 3, 2},
	{2, 4, 3, 4, 2},
	{2, 2, 2, 2, 2},
}

// P1: filling one correct cell of a partial pattern raises the saving by exactly
// 1 -- the dopamine click on DriveScore. And blank/single give 0.
func TestCorrespondence_FillRaisesSaving(t *testing.T) {
	bg := 9
	full := bgGrid(7, 14, bg)
	place(full, 1, 1, symBox)
	place(full, 1, 8, symBox)
	sFull := CorrespondenceSavings(full, bg)

	partial := bgGrid(7, 14, bg)
	place(partial, 1, 1, symBox)
	place(partial, 1, 8, symBox)
	partial[3][10] = bg // knock out one interior cell of the second box (center 4)
	sPartial := CorrespondenceSavings(partial, bg)

	if sFull != 24 {
		t.Fatalf("full match of a 5x5 pair should save area-0-pointer=24, got %d", sFull)
	}
	if sPartial != 23 {
		t.Fatalf("one missing cell should save 23 (residual 1), got %d", sPartial)
	}
	if sFull-sPartial != 1 {
		t.Fatalf("filling one cell must raise saving by exactly 1: full=%d partial=%d", sFull, sPartial)
	}
}

func TestCorrespondence_BlankAndSingleSaveNothing(t *testing.T) {
	bg := 9
	if s := CorrespondenceSavings(bgGrid(7, 7, bg), bg); s != 0 {
		t.Fatalf("blank must save 0, got %d", s)
	}
	one := bgGrid(7, 7, bg)
	place(one, 1, 1, symBox)
	if s := CorrespondenceSavings(one, bg); s != 0 {
		t.Fatalf("a single box (no pair) must save 0, got %d", s)
	}
}

// The degenerate cheat is shut: two identical SOLID blocks (no interior pattern)
// earn nothing -- Correspondence rewards matching PATTERNS, not floods.
func TestCorrespondence_SolidBlocksEarnNothing(t *testing.T) {
	bg := 9
	solid := [][]int{{2, 2, 2, 2, 2}, {2, 2, 2, 2, 2}, {2, 2, 2, 2, 2}, {2, 2, 2, 2, 2}, {2, 2, 2, 2, 2}}
	g := bgGrid(7, 14, bg)
	place(g, 1, 1, solid)
	place(g, 1, 8, solid)
	if s := CorrespondenceSavings(g, bg); s != 0 {
		t.Fatalf("two solid blocks must earn 0 (no pattern), got %d", s)
	}
}

// ColorSwap: a box that is the template recoloured (4<->3) still matches, via the
// O(area) majority map -- residual collapses to 0.
func TestCorrespondence_ColorSwapMatches(t *testing.T) {
	bg := 9
	swapped := [][]int{
		{2, 2, 2, 2, 2},
		{2, 3, 4, 3, 2},
		{2, 4, 3, 4, 2},
		{2, 3, 4, 3, 2},
		{2, 2, 2, 2, 2},
	}
	g := bgGrid(7, 14, bg)
	place(g, 1, 1, symBox)
	place(g, 1, 8, swapped)
	if s := CorrespondenceSavings(g, bg); s != 24 {
		t.Fatalf("a recoloured copy should match via ColorSwap (save 24), got %d", s)
	}
}

// Reflect: a box that is the horizontal mirror of the template matches via Reflect.
func TestCorrespondence_ReflectMatches(t *testing.T) {
	bg := 9
	tpl := [][]int{
		{2, 2, 2, 2, 2},
		{2, 4, 9, 9, 2},
		{2, 9, 9, 9, 2},
		{2, 9, 9, 3, 2},
		{2, 2, 2, 2, 2},
	}
	mir := [][]int{
		{2, 2, 2, 2, 2},
		{2, 9, 9, 4, 2},
		{2, 9, 9, 9, 2},
		{2, 3, 9, 9, 2},
		{2, 2, 2, 2, 2},
	}
	g := bgGrid(7, 14, bg)
	place(g, 1, 1, tpl)
	place(g, 1, 8, mir)
	if s := CorrespondenceSavings(g, bg); s != 24 {
		t.Fatalf("a mirror copy should match via Reflect (save 24), got %d", s)
	}
}

// Cross-primitive emergence: two matching template boxes placed with NO global
// symmetry/periodicity -- Correspondence must out-save the self-regularity
// primitives and BestPrimitive must select it.
func TestEmergence_SelectsCorrespondence(t *testing.T) {
	bg := 9
	g := bgGrid(16, 16, bg)
	place(g, 1, 1, symBox)   // top-left
	place(g, 9, 6, symBox)   // offset down-right: not a mirror or period of the first
	corr := CorrespondencePreference(g, bg)
	refl := SymmetryPreference(g, bg)
	tr := TranslatePreference(g, bg)
	cnt := NumerosityPreference(g, bg)
	t.Logf("savings: Correspondence=%d Reflect=%d Translate=%d Count=%d", corr, refl, tr, cnt)
	if corr <= refl || corr <= tr || corr <= cnt {
		t.Fatalf("Correspondence must dominate here: C=%d R=%d T=%d Cnt=%d", corr, refl, tr, cnt)
	}
	if p, _ := BestPrimitive(g, bg); p.Name != "Correspondence" {
		t.Fatalf("should select Correspondence, got %s", p.Name)
	}
}

// correspondenceResidual must point AT the exact cell a click needs to fix --
// the click-candidate feed added 2026-08-22 after finding that ft09 had a
// real (nonzero) Correspondence signal but ResidualCells never offered a
// candidate for it in 150 live actions, because it had no Correspondence case
// at all before this fix.
func TestCorrespondenceResidual_PointsAtTheMissingCell(t *testing.T) {
	bg := 9
	partial := bgGrid(7, 14, bg)
	place(partial, 1, 1, symBox)
	place(partial, 1, 8, symBox)
	partial[3][10] = bg // same knockout as TestCorrespondence_FillRaisesSaving

	got := correspondenceResidual(partial, bg)
	if len(got) != 1 || got[0] != (Cell{R: 3, C: 10}) {
		t.Fatalf("expected exactly the knocked-out cell {3,10}, got %v", got)
	}
}

// The AGGREGATION fix for candidate generation: ResidualCells must surface
// Correspondence-relevant cells even when Correspondence is nowhere near the
// argmax primitive by raw magnitude (Translate dominates almost every real
// board -- vc33 1332 vs 0, ft09 1210 vs 89, tn36 644 vs 0). Before this fix,
// ResidualCells' switch had no Correspondence case, so a click that would
// raise Correspondence was only ever offered by coincidence.
func TestResidualCells_SurfacesCorrespondenceEvenWhenNotArgmax(t *testing.T) {
	bg := 9
	w, h := 30, 16
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
	full[11][24] = bg // knock out the second box's center cell

	if bp, _ := BestPrimitive(full, bg); bp.Name != "Translate" {
		t.Fatalf("test setup requires Translate to dominate (as on real boards), got argmax=%s", bp.Name)
	}

	cells := ResidualCells(full, bg)
	want := Cell{R: 11, C: 24}
	found := false
	for _, c := range cells {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ResidualCells must include the Correspondence-fixing cell %v even though Translate is the argmax; got %v", want, cells)
	}
}
