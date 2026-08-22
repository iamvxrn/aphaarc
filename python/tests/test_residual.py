"""Port of the key residual/candidate-generation properties. Run directly (no pytest)."""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from alphaarc.mdl import best_primitive  # noqa: E402
from alphaarc.residual import reflect_residual, residual_cells  # noqa: E402

SYM_BOX = [
    [2, 2, 2, 2, 2],
    [2, 4, 3, 4, 2],
    [2, 3, 4, 3, 2],
    [2, 4, 3, 4, 2],
    [2, 2, 2, 2, 2],
]


def _bg_grid(h, w, bg):
    return [[bg] * w for _ in range(h)]


def _place(grid, top, left, box):
    for r in range(len(box)):
        for c in range(len(box[r])):
            grid[top + r][left + c] = box[r][c]


def test_reflect_residual_flags_the_broken_cell():
    # Symmetric grid with one cell broken -> that foreground cell is a residual.
    g = [[1, 2, 2, 1], [3, 4, 4, 3]]
    g[0][3] = 7  # breaks the (1,1) pair on row 0; col3 mirrors col0
    cells = reflect_residual(g, 0)
    assert (0, 3) in cells, cells


def test_residual_cells_surfaces_correspondence_even_when_not_argmax():
    """Translate dominates; the Correspondence-fixing cell must still be a candidate."""
    bg = 9
    w, h = 30, 16
    full = _bg_grid(h, w, bg)
    for r in range(h):
        for c in range(16):
            full[r][c] = 5 if c % 2 == 0 else 6
    _place(full, 1, 17, SYM_BOX)
    _place(full, 9, 22, SYM_BOX)
    full[11][24] = bg  # knock out the second box's centre cell

    bp, _ = best_primitive(full, bg)
    assert bp.name == "Translate", "setup needs Translate dominant, got %s" % bp.name
    assert (11, 24) in residual_cells(full, bg), "Correspondence-fixing cell missing from candidates"


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
