"""Minimal perception helpers for the Python submission core."""

from __future__ import annotations

from collections import Counter

from .mdl import Grid


def background_color(grid: Grid) -> int:
    """Most frequent cell value = background (mirrors perception.BackgroundColor)."""
    c = Counter(v for row in grid for v in row)
    if not c:
        return 0
    # Counter.most_common ties by insertion; make it deterministic (lowest colour
    # among the max count) to match a stable choice.
    best_n = max(c.values())
    return min(v for v, n in c.items() if n == best_n)
