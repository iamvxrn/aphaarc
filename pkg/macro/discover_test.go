package macro

import "testing"

// P1: blank grid discovers nothing (dark-room-proof, like every primitive).
func TestDiscover_BlankFindsNothing(t *testing.T) {
	blank := [][]int{{0, 0, 0, 0}, {0, 0, 0, 0}}
	if name, s := DiscoverTransform(blank, 0); s != 0 || name != "" {
		t.Fatalf("blank must discover nothing, got %q=%d", name, s)
	}
}

// P2: parity with the hand-authored Reflect -- on a horizontally symmetric grid
// the meta-operator discovers reflectH and its savings equal SymmetryPreference.
func TestDiscover_ReproducesReflect(t *testing.T) {
	bg := 0
	g := [][]int{
		{1, 2, 2, 1},
		{3, 4, 4, 3},
	}
	name, s := DiscoverTransform(g, bg)
	if name != "reflectH" {
		t.Fatalf("expected to discover reflectH, got %q", name)
	}
	if s != SymmetryPreference(g, bg) {
		t.Fatalf("discovered savings %d must match SymmetryPreference %d", s, SymmetryPreference(g, bg))
	}
}

// P3 (the real point): a grid with 180-degree rotational symmetry but NO
// reflection or period -- a regularity the hand-authored set (Reflect/Translate/
// Count) MISSES. The meta-operator must DISCOVER rot180 from the data and beat
// every hand-authored primitive. This is operator growth in miniature: the
// specific regularity was found, not written in code.
func TestDiscover_FindsRotationTheHandSetMisses(t *testing.T) {
	bg := 0
	// Point-symmetric about the centre (g[r][c] == g[H-1-r][W-1-c]) but not
	// mirror-symmetric on either axis and not periodic.
	g := [][]int{
		{1, 2, 0, 0},
		{0, 0, 3, 4},
		{4, 3, 0, 0},
		{0, 0, 2, 1},
	}
	name, s := DiscoverTransform(g, bg)
	if name != "rot180" {
		t.Fatalf("expected to discover rot180, got %q (savings %d)", name, s)
	}
	if s <= 0 {
		t.Fatalf("rot180 must explain cells, got %d", s)
	}
	// It must strictly beat the entire hand-authored grammar on this grid.
	refl := SymmetryPreference(g, bg)
	tr := TranslatePreference(g, bg)
	cnt := NumerosityPreference(g, bg)
	corr := CorrespondencePreference(g, bg)
	t.Logf("discovered %s=%d | Reflect=%d Translate=%d Count=%d Correspondence=%d", name, s, refl, tr, cnt, corr)
	if s <= refl || s <= tr || s <= cnt || s <= corr {
		t.Fatalf("discovered rot180=%d must beat the hand-authored set (R=%d T=%d Cnt=%d Corr=%d)", s, refl, tr, cnt, corr)
	}
}

// P4: the degenerate cheat is shut -- identity is not in the family, so a grid
// with no real symmetry discovers nothing rather than "explaining" every cell.
func TestDiscover_NoFakeSymmetry(t *testing.T) {
	bg := 0
	g := [][]int{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
	}
	if name, s := DiscoverTransform(g, bg); s != 0 {
		t.Fatalf("an asymmetric grid must discover nothing, got %q=%d", name, s)
	}
}
