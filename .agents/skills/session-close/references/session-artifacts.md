# Session Close Artifacts

Use this reference when writing closeout artifacts.

## SUMMARY.md

```markdown
---
id: 001
title: Initial repo setup
date: 2026-04-15
status: complete
repos_touched: [this-repo]
related_sessions: []
---

## Goal
What this session set out to do, in 1-3 sentences.

## Outcome
What actually happened. State plainly whether the goal was met, partially met,
or abandoned.

## Key Decisions
- Decision -> reason. One bullet each. Non-obvious calls only.

## Changes
- `path/to/file` - what changed and why
- Cross-repo changes listed with repo prefix, e.g. `other-repo/cmd/foo/main.go`

## Open Threads
- Anything deferred, unresolved, or intentionally left for a future session.

## References
- Links to PRs, docs, prior sessions (`.journal/000/SUMMARY.md`), external material.
```

Add `## Lessons` only when the session produced non-obvious learning a future
agent would benefit from.

## INDEX.md

```markdown
# Session Journal

| ID  | Date       | Title | Status | Summary |
|-----|------------|-------|--------|---------|
```

Rows stay ordered oldest to newest. New sessions start as `in-progress`;
closeout updates the matching row to `complete` or `abandoned`. Keep the summary
cell to one sentence.

## TECH_NOTES.md

```markdown
# Technical Notes

- Compact durable project-specific context.
```

This file is editable, not append-only. Revise it only when the session changed
technical context future agents should inherit.

## Final NOTES.md Entry

Append a final `## YYYY-MM-DD HH:MM — Close` entry pointing at merged PRs and
summarizing the handoff state. Do not rewrite earlier notes.
