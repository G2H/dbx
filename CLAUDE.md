# CLAUDE.md

## Status updates go in status.md (newest at the top)

Maintain status.md at the repo root as a running status log. Every time the user asks for a status update ("what's been done", "give me an update", etc.) or when you finish a major piece of work, PREPEND a new timestamped [YYYY-MM-DD HH:MM] entry at the TOP of status.md (newest first), summarizing what is done and what is left. Do not rewrite older entries; they are the history.

## Ask when there's more than one way

If a request can be reasonably implemented in more than one way, ask the user before picking. Lay out the options briefly (one line each). Don't silently pick. Exception: the project plan (docs/plan.md) already resolves the known forks (tech choices, filter semantics, row limit behavior, project layout, build approach). Honor those defaults without stopping to ask; only surface a NEW fork the plan did not anticipate.

## Never use em dashes

No em dashes anywhere: UI copy, code comments, commit messages, status.md, chat. Use a comma, period, or parentheses.

## Minimize dependencies

A new dependency is justified only when it replaces thousands of lines you'd otherwise write and maintain. If 200 lines of code avoids a package, skip it.

## Simple over clever

Code reads like prose. Before adding an abstraction (base class, generic helper, config-driven indirection), check for two concrete use cases right now. If not, inline the simpler version.

## No premature optimization

Simplest thing that works first. Measure before optimizing. Performance budgets are bars to hit, not invitations to micro-optimize ahead of time.

## Test features

Every feature ships with tests, written alongside the code, not after the project is "done". Pure logic (detection, quoting, filter building, row scanning) gets unit tests. Anything touching a real database gets an integration test against temp SQLite and DuckDB files. A milestone is complete only when `make test` passes. If a bug is found, first write a failing test that reproduces it, then fix it.
