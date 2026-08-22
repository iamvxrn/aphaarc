"""Python port of the key pkg/macro/mdl_test.go properties.

Runnable without pytest: `python3 python/tests/test_mdl.py` runs all tests and
prints PASS/FAIL. The point is parity with the Go tests -- if a property holds
in Go it must hold here, or the offline submission diverges from the R&D agent.
"""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from alphaarc.mdl import (  # noqa: E402
    best_primitive,
    best_primitive_delta,
    drive_preference,
    numerosity_preference,
    symmetry_preference,
    translate_preference,
)


def _clone(g):
    return [row[:] for row in g]


def test_reflect_blank_saves_nothing():
    blank = [[0, 0, 0, 0], [0, 0, 0, 0]]
    assert symmetry_preference(blank, 0) == 0


def test_reflect_symmetric_saves():
    g = [[1, 2, 2, 1], [3, 4, 4, 3]]
    assert symmetry_preference(g, 0) == 4  # 2 mirrored pairs per row, right cell counted


def test_count_scatter_pixels_saves_nothing():
    # Many identical single-cell dots: the numerosity guard rejects size<2 shapes.
    g = [[0, 5, 0, 5, 0], [0, 0, 0, 0, 0], [5, 0, 5, 0, 5]]
    assert numerosity_preference(g, 0) == 0


def test_count_repeated_objects_save():
    # Two identical 2-cell dominoes (colour 5) => (2-1)*2 = 2 saved.
    g = [
        [5, 5, 0, 0, 0],
        [0, 0, 0, 5, 5],
        [0, 0, 0, 0, 0],
    ]
    assert numerosity_preference(g, 0) == 2


def test_aggregation_dominant_primitive_does_not_hide_small_gain():
    """Big background Translate unchanged; a small primitive (Count) improves -> delta must catch it."""
    bg = 9
    w, h = 30, 16
    # Dominant, unchanged background: a period-2 stripe over columns [0,16).
    base = [[bg] * w for _ in range(h)]
    for r in range(h):
        for c in range(16):
            base[r][c] = 5 if c % 2 == 0 else 6
    # First colour-3 vertical domino (rows 2-3, col 20) present in both frames.
    before = _clone(base)
    before[2][20] = before[3][20] = 3
    after = _clone(before)
    # The click COMPLETES a second identical domino (rows 6-7, col 25): Count goes
    # from 0 (one lone shape) to 2 ((2-1)*2). Translate/Reflect stay flat.
    after[6][25] = after[7][25] = 3

    # The striped background itself makes BOTH Translate and Count large and
    # equal in absolute size (each ~224); the click's real effect is a tiny +2 to
    # Count (the two dominoes become identical). A naive argmax-of-argmax diff
    # would report ~0; best_primitive_delta must surface the +2.
    assert translate_preference(before, bg) == translate_preference(after, bg), "setup: Translate must be unchanged"
    assert numerosity_preference(after, bg) - numerosity_preference(before, bg) == 2
    d = best_primitive_delta(before, after, bg)
    assert d == 2, "the small Count gain must register despite dominant primitives, got %d" % d


def test_aggregation_worsening_click_is_punished_not_floored():
    """A click that worsens the only applicable primitive must yield a NEGATIVE delta."""
    bg = 9
    w, h = 30, 16
    good = [[bg] * w for _ in range(h)]
    for r in range(h):
        for c in range(16):
            good[r][c] = 5 if c % 2 == 0 else 6
    bad = _clone(good)
    bad[0][2] = bg  # break the period locally -> Translate drops

    assert translate_preference(bad, bg) < translate_preference(good, bg)
    # Reflect/Count are inapplicable here (flat 0); a plain max() would floor to 0.
    assert best_primitive_delta(good, bad, bg) < 0, "worsening must be punished, not floored"


def _run():
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    failed = 0
    for t in tests:
        try:
            t()
            print("PASS", t.__name__)
        except AssertionError as e:
            failed += 1
            print("FAIL", t.__name__, "--", e)
        except Exception as e:  # noqa: BLE001
            failed += 1
            print("ERROR", t.__name__, "--", repr(e))
    print("\n%d/%d passed" % (len(tests) - failed, len(tests)))
    return failed


if __name__ == "__main__":
    sys.exit(1 if _run() else 0)
