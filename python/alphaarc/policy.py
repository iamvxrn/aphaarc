"""Model-free compression-drive policy -- the essential decision loop from
cmd/alphaarc-play, ported lean.

The affordance MODEL can't learn the big/absolute/colour-colliding effects these
games have, so instead of predicting we REINFORCE on the observed compression
delta: each clicked token's value is an EMA of how much it actually moved the
drive (best_primitive_delta -- the signed biggest-mover, this session's fix).
Candidates come from residual_cells' clustered targets (click where compression
fails). This is env-agnostic pure logic; my_agent.py adapts it to arc-agi.
"""

from __future__ import annotations

import random
from typing import Dict, List, Optional, Tuple

from .mdl import Grid, best_primitive_delta
from .perception import background_color
from .residual import residual_targets


class Policy:
    def __init__(
        self,
        drive_gain_weight: float = 0.02,
        residual_bonus: float = 0.15,
        explore: float = 0.2,
        max_candidates: int = 8,
        rng: Optional[random.Random] = None,
    ):
        self.w = drive_gain_weight
        self.residual_bonus = residual_bonus
        self.explore = explore
        self.max_candidates = max_candidates
        self.rng = rng or random.Random()
        self.drive_gain: Dict[str, float] = {}   # EMA of observed compression delta per token
        self.tries: Dict[str, int] = {}          # optimism for the under-tried
        self.dead: Dict[str, float] = {}         # decaying "did nothing" count (inhibition of return)
        self._prev_grid: Optional[Grid] = None
        self._last_token: Optional[str] = None

    @staticmethod
    def _tok(x: int, y: int) -> str:
        return "%d,%d" % (x, y)

    def _credit_last(self, grid: Grid, bg: int) -> None:
        """Model-free reinforcement: credit the previous click by its observed ΔL."""
        if self._prev_grid is None or self._last_token is None:
            return
        d = float(best_primitive_delta(self._prev_grid, grid, bg))
        self.drive_gain[self._last_token] = 0.6 * self.drive_gain.get(self._last_token, 0.0) + 0.4 * d
        if grid == self._prev_grid:  # the click changed nothing -> taboo it a while
            self.dead[self._last_token] = self.dead.get(self._last_token, 0.0) + 1.0
        # decay all taboos slightly (inhibition of return fades)
        for k in list(self.dead):
            self.dead[k] *= 0.75
            if self.dead[k] < 0.05:
                del self.dead[k]

    def choose_click(self, grid: Grid, bg: Optional[int] = None) -> Optional[Tuple[int, int]]:
        """Return the (x=col, y=row) to click, or None if no candidate exists."""
        if bg is None:
            bg = background_color(grid)
        self._credit_last(grid, bg)

        pts = residual_targets(grid, bg, self.max_candidates)
        if not pts:
            self._prev_grid = [row[:] for row in grid]
            self._last_token = None
            return None

        best_tok, best_xy, best_v = None, None, -1e18
        for i, p in enumerate(pts):
            tok = self._tok(p.x, p.y)
            v = (
                self.w * self.drive_gain.get(tok, 0.0)
                + self.residual_bonus / (i + 1)          # larger clusters (earlier) rank higher
                + 0.05 / (self.tries.get(tok, 0) + 1)    # optimism for the under-tried
                - self.dead.get(tok, 0.0)                # inhibition of return
            )
            if v > best_v:
                best_v, best_tok, best_xy = v, tok, (p.x, p.y)

        # epsilon-exploration over the candidate set
        if self.explore > 0 and self.rng.random() < self.explore:
            p = self.rng.choice(pts)
            best_tok, best_xy = self._tok(p.x, p.y), (p.x, p.y)

        self.tries[best_tok] = self.tries.get(best_tok, 0) + 1
        self._prev_grid = [row[:] for row in grid]
        self._last_token = best_tok
        return best_xy
