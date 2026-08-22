"""AlphaARC compression-drive core, Python port for the ARC-AGI-3 Kaggle submission.

The Go codebase (pkg/macro + cmd/alphaarc-play) is canonical for R&D against the
live free API; this package re-implements the same primitives and decision core
in pure-stdlib Python 3.12 so the agent can run OFFLINE inside Kaggle's sandbox
(no internet, no calling out to the Go binary), as the competition requires.
"""
