"""Port of pkg/macro/correspondence_test.go key properties. Run: python3 python/tests/test_correspondence.py"""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from alphaarc.mdl import best_primitive  # noqa: E402
from alphaarc.correspondence import (  # noqa: E402
    correspondence_preference,
    correspondence_residual,
    correspondence_savings,
)


def _bg_grid(h, w, bg):
    return [[bg] * w for _ in range(h)]


def _place(grid, top, left, box):
    for r in range(len(box)):
        for c in range(len(box[r])):
            grid[top + r][left + c] = box[r][c]


# 5x5 colour-2 frame with a repeated 4/3 interior (each colour occurs several
# times, so a missing cell yields residual 1, not a colour-swap remap).
SYM_BOX = [
    [2, 2, 2, 2, 2],
    [2, 4, 3, 4, 2],
    [2, 3, 4, 3, 2],
    [2, 4, 3, 4, 2],
    [2, 2, 2, 2, 2],
]


def test_fill_raises_saving():
    bg = 9
    full = _bg_grid(7, 14, bg)
    _place(full, 1, 1, SYM_BOX)
    _place(full, 1, 8, SYM_BOX)
    partial = _bg_grid(7, 14, bg)
    _place(partial, 1, 1, SYM_BOX)
    _place(partial, 1, 8, SYM_BOX)
    partial[3][10] = bg  # knock out one interior cell of the second box
    assert correspondence_savings(full, bg) == 24, correspondence_savings(full, bg)
    assert correspondence_savings(partial, bg) == 23, correspondence_savings(partial, bg)


def test_blank_and_single_save_nothing():
    bg = 9
    assert correspondence_savings(_bg_grid(7, 7, bg), bg) == 0
    one = _bg_grid(7, 7, bg)
    _place(one, 1, 1, SYM_BOX)
    assert correspondence_savings(one, bg) == 0


def test_solid_blocks_earn_nothing():
    bg = 9
    solid = [[2] * 5 for _ in range(5)]
    g = _bg_grid(7, 14, bg)
    _place(g, 1, 1, solid)
    _place(g, 1, 8, solid)
    assert correspondence_savings(g, bg) == 0


def test_colorswap_matches():
    bg = 9
    swapped = [
        [2, 2, 2, 2, 2],
        [2, 3, 4, 3, 2],
        [2, 4, 3, 4, 2],
        [2, 3, 4, 3, 2],
        [2, 2, 2, 2, 2],
    ]
    g = _bg_grid(7, 14, bg)
    _place(g, 1, 1, SYM_BOX)
    _place(g, 1, 8, swapped)
    assert correspondence_savings(g, bg) == 24, correspondence_savings(g, bg)


def test_emergence_selects_correspondence():
    bg = 9
    g = _bg_grid(16, 16, bg)
    _place(g, 1, 1, SYM_BOX)
    _place(g, 9, 6, SYM_BOX)  # offset: not a mirror or period of the first
    p, _ = best_primitive(g, bg)
    assert p.name == "Correspondence", "selected %s" % p.name


def test_residual_points_at_the_missing_cell():
    bg = 9
    partial = _bg_grid(7, 14, bg)
    _place(partial, 1, 1, SYM_BOX)
    _place(partial, 1, 8, SYM_BOX)
    partial[3][10] = bg
    got = correspondence_residual(partial, bg)
    assert got == [(3, 10)], got


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
