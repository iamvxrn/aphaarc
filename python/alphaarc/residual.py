"""Compression residual = attention. Port of pkg/macro/residual.go.

The drive not only says a grid is periodic/symmetric/repetitive -- it says WHICH
cells the regularity cannot explain. In a mostly-regular world those violating
cells are the salient, usually-interactive elements: click where compression
FAILS, not on the texture. residual_cells() is the click-candidate source the
agent's policy consumes.
"""

from __future__ import annotations

from typing import Dict, List, Tuple

from .mdl import (
    Grid,
    _components,
    _min_row_len,
    _shape_key,
    best_primitive,
    symmetry_savings,
)
from .correspondence import Cell, correspondence_residual


def _best_horiz_period(grid: Grid, bg: int) -> Tuple[int, int]:
    h = len(grid)
    if h == 0:
        return 0, 0
    w = _min_row_len(grid)
    period, savings = 0, 0
    for p in range(2, w):
        s = 0
        for r in range(h):
            for c in range(p, len(grid[r])):
                if grid[r][c] != bg and grid[r][c] == grid[r][c - p]:
                    s += 1
        if s > savings:
            savings, period = s, p
    return period, savings


def _best_vert_period(grid: Grid, bg: int) -> Tuple[int, int]:
    h = len(grid)
    if h == 0:
        return 0, 0
    period, savings = 0, 0
    for p in range(2, h):
        s = 0
        for r in range(p, h):
            w = min(len(grid[r]), len(grid[r - p]))
            for c in range(w):
                if grid[r][c] != bg and grid[r][c] == grid[r - p][c]:
                    s += 1
        if s > savings:
            savings, period = s, p
    return period, savings


def reflect_residual(grid: Grid, bg: int) -> List[Cell]:
    sh, sv = symmetry_savings(grid, bg)
    cells: List[Cell] = []
    if sh >= sv:
        for r in range(len(grid)):
            w = len(grid[r])
            for c in range(w // 2):
                mc = w - 1 - c
                if grid[r][c] == grid[r][mc]:
                    continue
                if grid[r][c] != bg:
                    cells.append((r, c))
                if grid[r][mc] != bg:
                    cells.append((r, mc))
    else:
        h = len(grid)
        for r in range(h // 2):
            mr = h - 1 - r
            w = min(len(grid[r]), len(grid[mr]))
            for c in range(w):
                if grid[r][c] == grid[mr][c]:
                    continue
                if grid[r][c] != bg:
                    cells.append((r, c))
                if grid[mr][c] != bg:
                    cells.append((mr, c))
    return cells


def _majority_color(tally: Dict[int, int]) -> int:
    best, best_n = -1, -1
    for col, n in tally.items():
        if n > best_n or (n == best_n and col < best):
            best, best_n = col, n
    return best


def translate_residual(grid: Grid, bg: int) -> List[Cell]:
    ph, sh = _best_horiz_period(grid, bg)
    pv, sv = _best_vert_period(grid, bg)
    cells: List[Cell] = []
    if sh == 0 and sv == 0:
        return cells
    if sh >= sv and ph > 0:
        for r in range(len(grid)):
            w = len(grid[r])
            for k in range(ph):
                tally: Dict[int, int] = {}
                for c in range(k, w, ph):
                    tally[grid[r][c]] = tally.get(grid[r][c], 0) + 1
                maj = _majority_color(tally)
                for c in range(k, w, ph):
                    if grid[r][c] != bg and grid[r][c] != maj:
                        cells.append((r, c))
    elif pv > 0:
        h = len(grid)
        w = _min_row_len(grid)
        for c in range(w):
            for k in range(pv):
                tally = {}
                for r in range(k, h, pv):
                    if c < len(grid[r]):
                        tally[grid[r][c]] = tally.get(grid[r][c], 0) + 1
                maj = _majority_color(tally)
                for r in range(k, h, pv):
                    if c < len(grid[r]) and grid[r][c] != bg and grid[r][c] != maj:
                        cells.append((r, c))
    return cells


def count_residual(grid: Grid, bg: int) -> List[Cell]:
    """Cells of the odd-object-out: objects NOT in the largest identical-shape group."""
    comps = _components(grid, bg)
    if not comps:
        return []
    keyed = [(_shape_key(color, cells), cells) for color, cells in comps]
    counts: Dict[str, int] = {}
    for key, _ in keyed:
        counts[key] = counts.get(key, 0) + 1
    dom_key, dom_count = "", 0
    for k, n in counts.items():
        if n > dom_count:
            dom_count, dom_key = n, k
    cells: List[Cell] = []
    for key, cs in keyed:
        if key != dom_key:
            cells.extend(cs)
    return cells


def residual_cells(grid: Grid, bg: int) -> List[Cell]:
    """Click candidates. Whichever primitive is argmax supplies its residual, and
    Correspondence residual is ALWAYS appended too (it is almost never the argmax
    by raw magnitude, so gating it behind winning would make its click-relevant
    cells invisible -- the perception-side aggregation fix, commit 925880c)."""
    bp, sav = best_primitive(grid, bg)
    if sav == 0:
        return []
    out: List[Cell] = []
    if bp.name == "Reflect":
        out = reflect_residual(grid, bg)
    elif bp.name == "Translate":
        out = translate_residual(grid, bg)
    elif bp.name == "Count":
        out = count_residual(grid, bg)
    elif bp.name == "Correspondence":
        out = correspondence_residual(grid, bg)
    if bp.name != "Correspondence":
        out = out + correspondence_residual(grid, bg)
    return out


def _cluster_residual(cells: List[Cell]) -> List[List[Cell]]:
    in_set = set(cells)
    seen = set()
    clusters: List[List[Cell]] = []
    for start in cells:
        if start in seen:
            continue
        cluster = []
        stack = [start]
        seen.add(start)
        while stack:
            cur = stack.pop()
            cluster.append(cur)
            for dr, dc in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                n = (cur[0] + dr, cur[1] + dc)
                if n in in_set and n not in seen:
                    seen.add(n)
                    stack.append(n)
        clusters.append(cluster)
    return clusters


class ResidualPoint:
    __slots__ = ("x", "y", "size")

    def __init__(self, x: int, y: int, size: int):
        self.x = x  # col
        self.y = y  # row
        self.size = size

    def __repr__(self):
        return "ResidualPoint(x=%d,y=%d,size=%d)" % (self.x, self.y, self.size)


def residual_targets(grid: Grid, bg: int, max_n: int) -> List[ResidualPoint]:
    """Up to max_n residual-cluster centroids, largest cluster first (max_n<=0 = all)."""
    cells = residual_cells(grid, bg)
    if not cells:
        return []
    clusters = _cluster_residual(cells)
    clusters.sort(key=len, reverse=True)
    out: List[ResidualPoint] = []
    for cl in clusters:
        if max_n > 0 and len(out) >= max_n:
            break
        sum_r = sum(c[0] for c in cl)
        sum_c = sum(c[1] for c in cl)
        out.append(ResidualPoint(sum_c // len(cl), sum_r // len(cl), len(cl)))
    return out


def residual_target(grid: Grid, bg: int):
    pts = residual_targets(grid, bg, 1)
    if not pts:
        return None
    return pts[0]
