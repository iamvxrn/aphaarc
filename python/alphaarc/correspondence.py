"""Correspondence / Copy -- relational compression. Port of pkg/macro/correspondence.go.

Region A described as "a copy of region B up to {Identity, ColorSwap, Reflect}
plus a residual". Closes the template/legend/copy class (s5i5, tn36, vc33-L2):
puzzles that are anti-compression to self-regularity but compression-positive
relationally. Search stays O(objects): anchors = repeated components grouped by a
loose (WxH, colour) signature, only within-class members compared, constant
transform set. Importing this module REGISTERS Correspondence in mdl.PRIMITIVES.
"""

from __future__ import annotations

from typing import Dict, List, Tuple

from .mdl import Grid, Primitive, PRIMITIVES, _components

Cell = Tuple[int, int]  # (row, col)

_POINTER_COST = 1   # bits to name template + anchor (small constant)
_MIN_AREA = 4       # a real region, not a dot/line (degenerate-cheat guard)


def _cells_bbox(cells: List[Cell]) -> Tuple[int, int, int, int]:
    rs = [c[0] for c in cells]
    cs = [c[1] for c in cells]
    return min(rs), min(cs), max(rs), max(cs)


def _subgrid(grid: Grid, minr: int, minc: int, maxr: int, maxc: int) -> Grid:
    out = []
    for r in range(minr, maxr + 1):
        row = []
        for c in range(minc, maxc + 1):
            row.append(grid[r][c] if r < len(grid) and c < len(grid[r]) else 0)
        out.append(row)
    return out


def _distinct_colors(g: Grid) -> int:
    return len({v for row in g for v in row})


def _residual_identity(a: Grid, b: Grid) -> int:
    return sum(1 for i in range(len(a)) for j in range(len(a[i])) if a[i][j] != b[i][j])


def _residual_reflect_h(a: Grid, b: Grid) -> int:
    res = 0
    for i in range(len(a)):
        w = len(a[i])
        for j in range(w):
            if a[i][j] != b[i][w - 1 - j]:
                res += 1
    return res


def _residual_reflect_v(a: Grid, b: Grid) -> int:
    res = 0
    h = len(a)
    for i in range(h):
        for j in range(len(a[i])):
            if a[i][j] != b[h - 1 - i][j]:
                res += 1
    return res


def _color_swap_map(a: Grid, b: Grid, bg: int) -> Dict[int, int]:
    """Majority map b->a in O(area). bg pinned to bg (a hole is a deletion, not a recolour)."""
    tally: Dict[int, Dict[int, int]] = {}
    for i in range(len(a)):
        for j in range(len(a[i])):
            bc, ac = b[i][j], a[i][j]
            if bc == bg:
                continue
            tally.setdefault(bc, {})
            tally[bc][ac] = tally[bc].get(ac, 0) + 1
    m = {bg: bg}
    for bc, counts in tally.items():
        best, bn = bc, -1
        for ac, n in counts.items():
            if ac == bg:
                continue
            if n > bn or (n == bn and ac < best):
                best, bn = ac, n
        m[bc] = best
    return m


def _residual_color_swap(a: Grid, b: Grid, bg: int) -> int:
    m = _color_swap_map(a, b, bg)
    return sum(1 for i in range(len(a)) for j in range(len(a[i])) if a[i][j] != m.get(b[i][j], b[i][j]))


def _min_residual(a: Grid, b: Grid, bg: int) -> int:
    return min(
        _residual_identity(a, b),
        _residual_color_swap(a, b, bg),
        _residual_reflect_h(a, b),
        _residual_reflect_v(a, b),
    )


# --- *_cells variants: the LOCAL positions in `a` that differ (click candidates) ---

def _residual_identity_cells(a: Grid, b: Grid) -> List[Cell]:
    return [(i, j) for i in range(len(a)) for j in range(len(a[i])) if a[i][j] != b[i][j]]


def _residual_reflect_h_cells(a: Grid, b: Grid) -> List[Cell]:
    out = []
    for i in range(len(a)):
        w = len(a[i])
        for j in range(w):
            if a[i][j] != b[i][w - 1 - j]:
                out.append((i, j))
    return out


def _residual_reflect_v_cells(a: Grid, b: Grid) -> List[Cell]:
    out = []
    h = len(a)
    for i in range(h):
        for j in range(len(a[i])):
            if a[i][j] != b[h - 1 - i][j]:
                out.append((i, j))
    return out


def _residual_color_swap_cells(a: Grid, b: Grid, bg: int) -> List[Cell]:
    m = _color_swap_map(a, b, bg)
    return [(i, j) for i in range(len(a)) for j in range(len(a[i])) if a[i][j] != m.get(b[i][j], b[i][j])]


def _min_residual_cells(a: Grid, b: Grid, bg: int) -> List[Cell]:
    best = _residual_identity_cells(a, b)
    for cells in (
        _residual_color_swap_cells(a, b, bg),
        _residual_reflect_h_cells(a, b),
        _residual_reflect_v_cells(a, b),
    ):
        if len(cells) < len(best):
            best = cells
    return best


def _groups(grid: Grid, bg: int) -> Dict[str, List[Tuple[int, int, int, int]]]:
    """Anchors: component bboxes grouped by the loose (WxH, colour) signature."""
    comps = _components(grid, bg)
    if len(comps) < 2:
        return {}
    groups: Dict[str, List[Tuple[int, int, int, int]]] = {}
    for color, cells in comps:
        minr, minc, maxr, maxc = _cells_bbox(cells)
        w, h = maxc - minc + 1, maxr - minr + 1
        if w * h < _MIN_AREA:
            continue
        key = "%dx%d:c%d" % (w, h, color)
        groups.setdefault(key, []).append((minr, minc, maxr, maxc))
    return groups


def _reference(groups_members):
    """Extract member subgrids and pick the fullest (most non-bg) as the reference."""
    return groups_members


def correspondence_savings(grid: Grid, bg: int) -> int:
    savings = 0
    for members in _groups(grid, bg).values():
        if len(members) < 2:
            continue
        grids = [_subgrid(grid, *m) for m in members]
        ref_idx, ref_full = 0, -1
        for i, g in enumerate(grids):
            full = sum(1 for row in g for v in row if v != bg)
            if full > ref_full:
                ref_full, ref_idx = full, i
        ref = grids[ref_idx]
        if _distinct_colors(ref) < 2:
            continue  # reference must be a real pattern, not a solid block
        area = len(ref) * len(ref[0])
        for i, g in enumerate(grids):
            if i == ref_idx:
                continue
            s = area - _min_residual(g, ref, bg) - _POINTER_COST
            if s > 0:
                savings += s
    return savings


def correspondence_preference(grid: Grid, bg: int) -> int:
    return correspondence_savings(grid, bg)


def correspondence_residual(grid: Grid, bg: int) -> List[Cell]:
    """ABSOLUTE grid cells that disagree with their class reference -- click candidates."""
    out: List[Cell] = []
    for members in _groups(grid, bg).values():
        if len(members) < 2:
            continue
        grids = [_subgrid(grid, *m) for m in members]
        ref_idx, ref_full = 0, -1
        for i, g in enumerate(grids):
            full = sum(1 for row in g for v in row if v != bg)
            if full > ref_full:
                ref_full, ref_idx = full, i
        ref = grids[ref_idx]
        if _distinct_colors(ref) < 2:
            continue
        for i, g in enumerate(grids):
            if i == ref_idx:
                continue
            minr, minc = members[i][0], members[i][1]
            for (cr, cc) in _min_residual_cells(g, ref, bg):
                out.append((minr + cr, minc + cc))
    return out


# Register Correspondence as the 4th primitive (LAST, matching Go's order so
# BestPrimitive ties resolve identically). Idempotent under re-import.
if not any(p.name == "Correspondence" for p in PRIMITIVES):
    PRIMITIVES.append(Primitive("Correspondence", correspondence_preference))
