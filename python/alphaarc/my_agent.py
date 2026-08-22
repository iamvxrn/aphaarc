"""MyAgent -- the ARC-AGI-3 Kaggle submission adapter.

This is the THIN glue between the arc-agi engine and our engine-agnostic,
unit-tested decision core (policy.Policy over the ported compression primitives).
Everything hard is in the tested modules; this file only (a) pulls the current
grid out of a frame and (b) turns a chosen (x, y) click into a GameAction.

>>> VERIFY-LOCALLY (cannot be run offline here -- do it with `make play-local`) <<<
The three marked spots below encode assumptions about the arc-agi API surface
(exact frame field, GameAction construction, GameState names). They match what
our Go remote client parses from the SAME engine (frame = list of 2D grids;
ACTION6 carries x,y; state is a WIN/GAME_OVER/NOT_FINISHED string), but confirm
the attribute/method names against the installed package and adjust only these
spots -- the decision logic underneath is already proven.
"""

from __future__ import annotations

from typing import List, Optional

from .perception import background_color
from .policy import Policy

# The starter kit exposes GameAction (and frame/state types). Import is kept
# local/guarded so the tested core imports without the arc-agi package present.
try:  # pragma: no cover - environment-dependent
    from arcengine import GameAction  # type: ignore
except Exception:  # pragma: no cover
    GameAction = None  # resolved in the Kaggle/starter-kit environment


def _grid_of(latest_frame) -> Optional[List[List[int]]]:
    """(1) VERIFY: current grid. Our Go client reads frame as a LIST of 2D grids
    and uses index 0. If play-local shows a stale board, switch to [-1]."""
    raw = getattr(latest_frame, "frame", None)
    if raw is None:
        raw = getattr(latest_frame, "grid", None)
    if raw is None:
        return None
    # raw is either a single 2D grid or a list of them.
    if raw and isinstance(raw[0], list) and raw[0] and isinstance(raw[0][0], list):
        return raw[0]  # list-of-grids -> current (Go parity: index 0)
    return raw          # already a single 2D grid


def _state_of(latest_frame) -> str:
    """(2) VERIFY: game state as an upper-case string (WIN/GAME_OVER/NOT_FINISHED/...)."""
    st = getattr(latest_frame, "state", "")
    return getattr(st, "name", str(st)).upper()


def _click_action(x: int, y: int):
    """(3) VERIFY: build the coordinate click (ACTION6) with x=col, y=row.

    Common arc-agi-3 form: GameAction.ACTION6 with data set to {x, y}. If the
    installed SDK differs (e.g. a constructor or a .reasoning field), change only
    this function."""
    action = GameAction.ACTION6
    if hasattr(action, "set_data"):
        action.set_data({"x": x, "y": y})
    else:  # fallbacks seen across SDK versions
        try:
            action.data = {"x": x, "y": y}  # type: ignore[attr-defined]
        except Exception:
            pass
    return action


class MyAgent:
    """Required by the Kaggle harness: is_done(...) and choose_action(...)."""

    def __init__(self, *args, **kwargs):
        self.policy = Policy()
        self._actions = 0

    def is_done(self, frames, latest_frame) -> bool:
        # Stop only when the whole game is won; otherwise keep playing (the harness
        # enforces the real action/time budget).
        return _state_of(latest_frame) == "WIN"

    def choose_action(self, frames, latest_frame):
        self._actions += 1
        grid = _grid_of(latest_frame)
        if not grid:
            # Nothing to perceive yet -> RESET to (re)start the level.
            return GameAction.RESET
        bg = background_color(grid)
        xy = self.policy.choose_click(grid, bg)
        if xy is None:
            return GameAction.RESET  # no compressible structure/candidate -> restart
        x, y = xy
        return _click_action(x, y)
