"""Structural MDL primitives -- Python port of pkg/macro/mdl.go + mdl_number.go.

This is the compression-drive core, ported to Python because the ARC Prize 2026
/ ARC-AGI-3 Kaggle submission is Python-only and runs OFFLINE (no calling our Go
binary). The logic mirrors the Go source 1:1 -- same "bits saved by a regularity"
currency, same degenerate-cheat guards -- so the offline Go tests and these
tests must agree.

A grid is a list[list[int]] (values 0..15); bg is the background colour.
"""

from __future__ import annotations

from typing import Callable, List, Tuple


Grid = List[List[int]]


# --- Primitive #1: Reflect (symmetry) ---

def symmetry_savings(grid: Grid, bg: int) -> Tuple[int, int]:
    """Foreground cells explained for free by horizontal and by vertical reflection.

    Horizontal mirrors column c <-> W-1-c; a right-half foreground cell equal to
    its mirror is determined by the left half => 1 saved. Vertical is the row
    analogue. Center column/row (odd size) maps to itself and is skipped.
    """
    h = len(grid)
    if h == 0:
        return 0, 0
    savings_h = 0
    for r in range(h):
        w = len(grid[r])
        for c in range(w // 2):
            mc = w - 1 - c
            if grid[r][mc] != bg and grid[r][mc] == grid[r][c]:
                savings_h += 1
    savings_v = 0
    for r in range(h // 2):
        mr = h - 1 - r
        w = min(len(grid[r]), len(grid[mr]))
        for c in range(w):
            if grid[mr][c] != bg and grid[mr][c] == grid[r][c]:
                savings_v += 1
    return savings_h, savings_v


def symmetry_preference(grid: Grid, bg: int) -> int:
    sh, sv = symmetry_savings(grid, bg)
    return sh if sh > sv else sv


# --- Primitive #2: Translate / Repeat (periodicity) ---

def _min_row_len(grid: Grid) -> int:
    w = -1
    for row in grid:
        if w < 0 or len(row) < w:
            w = len(row)
    return 0 if w < 0 else w


def _horiz_tile_nontrivial(grid: Grid, p: int) -> bool:
    """True iff columns [0,p) contain >= 2 distinct values (a pattern, not a flood)."""
    seen = set()
    for row in grid:
        for c in range(min(p, len(row))):
            seen.add(row[c])
            if len(seen) >= 2:
                return True
    return False


def _vert_tile_nontrivial(grid: Grid, p: int) -> bool:
    seen = set()
    for r in range(min(p, len(grid))):
        for v in grid[r]:
            seen.add(v)
            if len(seen) >= 2:
                return True
    return False


def translate_savings(grid: Grid, bg: int) -> Tuple[int, int]:
    """Foreground cells explained by the best non-trivial horizontal / vertical period."""
    h = len(grid)
    if h == 0:
        return 0, 0
    w = _min_row_len(grid)
    savings_h = 0
    for p in range(2, w):
        if not _horiz_tile_nontrivial(grid, p):
            continue
        s = 0
        for r in range(h):
            for c in range(p, len(grid[r])):
                if grid[r][c] != bg and grid[r][c] == grid[r][c - p]:
                    s += 1
        if s > savings_h:
            savings_h = s
    savings_v = 0
    for p in range(2, h):
        if not _vert_tile_nontrivial(grid, p):
            continue
        s = 0
        for r in range(p, h):
            w2 = min(len(grid[r]), len(grid[r - p]))
            for c in range(w2):
                if grid[r][c] != bg and grid[r][c] == grid[r - p][c]:
                    s += 1
        if s > savings_v:
            savings_v = s
    return savings_h, savings_v


def translate_preference(grid: Grid, bg: int) -> int:
    sh, sv = translate_savings(grid, bg)
    return sh if sh > sv else sv


# --- Primitive #3: Number / Numerosity (count) ---

def _shape_key(color: int, cells: List[Tuple[int, int]]) -> str:
    """Position-independent signature: colour + sorted offsets from the top-left."""
    minr = min(c[0] for c in cells)
    minc = min(c[1] for c in cells)
    offs = sorted((r - minr, c - minc) for r, c in cells)
    return "c%d:" % color + "".join("%d,%d;" % (r, c) for r, c in offs)


def _components(grid: Grid, bg: int) -> List[Tuple[int, List[Tuple[int, int]]]]:
    """4-connected same-colour foreground components as (colour, cells)."""
    h = len(grid)
    if h == 0:
        return []
    visited = [[False] * len(grid[r]) for r in range(h)]
    comps: List[Tuple[int, List[Tuple[int, int]]]] = []
    for r in range(h):
        for c in range(len(grid[r])):
            if grid[r][c] == bg or visited[r][c]:
                continue
            color = grid[r][c]
            cells: List[Tuple[int, int]] = []
            stack = [(r, c)]
            visited[r][c] = True
            while stack:
                cr, cc = stack.pop()
                cells.append((cr, cc))
                for dr, dc in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                    nr, nc = cr + dr, cc + dc
                    if nr < 0 or nr >= h or nc < 0 or nc >= len(grid[nr]):
                        continue
                    if visited[nr][nc] or grid[nr][nc] != color:
                        continue
                    visited[nr][nc] = True
                    stack.append((nr, nc))
            comps.append((color, cells))
    return comps


def numerosity_savings(grid: Grid, bg: int) -> int:
    """Sum over identical-shape groups of (count-1) * shape_size.

    Guard: single-cell shapes (size < 2) don't count -- scattering pixels is not
    counting objects.
    """
    group_count = {}
    group_cells = {}
    for color, cells in _components(grid, bg):
        key = _shape_key(color, cells)
        group_count[key] = group_count.get(key, 0) + 1
        group_cells[key] = len(cells)
    savings = 0
    for key, cnt in group_count.items():
        s = group_cells[key]
        if s < 2:
            continue
        if cnt >= 2:
            savings += (cnt - 1) * s
    return savings


def numerosity_preference(grid: Grid, bg: int) -> int:
    return numerosity_savings(grid, bg)


# --- Emergent primitive selection ---

class Primitive:
    __slots__ = ("name", "savings")

    def __init__(self, name: str, savings: Callable[[Grid, int], int]):
        self.name = name
        self.savings = savings


# Correspondence is registered by correspondence.py at import time (appended
# there) so this module stays free of the heavier relational primitive.
PRIMITIVES: List[Primitive] = [
    Primitive("Reflect", symmetry_preference),
    Primitive("Translate", translate_preference),
    Primitive("Count", numerosity_preference),
]


def best_primitive(grid: Grid, bg: int) -> Tuple[Primitive, int]:
    """The primitive that compresses grid the most, and its saving (ties: earlier wins)."""
    best = PRIMITIVES[0]
    best_save = best.savings(grid, bg)
    for p in PRIMITIVES[1:]:
        s = p.savings(grid, bg)
        if s > best_save:
            best, best_save = p, s
    return best, best_save


def drive_preference(grid: Grid, bg: int) -> int:
    _, s = best_primitive(grid, bg)
    return s


def best_primitive_delta(before: Grid, after: Grid, bg: int) -> int:
    """Signed delta of whichever primitive moved MOST by absolute value.

    Mirrors the Go fix (commits 606f770 + 4b2f6f3): NOT drive_preference diff
    (argmax-of-argmax hides a small primitive's real local gain behind a big
    dominant one), and NOT plain max(delta) (an inapplicable primitive flat at 0
    then outranks a genuine negative delta and erases punishment). Biggest-mover,
    sign preserved, does both jobs.
    """
    best = 0
    best_abs = -1
    for p in PRIMITIVES:
        d = p.savings(after, bg) - p.savings(before, bg)
        a = d if d >= 0 else -d
        if a > best_abs:
            best, best_abs = d, a
    return best
