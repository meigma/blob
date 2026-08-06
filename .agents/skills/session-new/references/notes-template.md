# NOTES.md Template

Use this shape when `session-new` creates a session log.

```markdown
---
id: 001
title: Initial repo setup
started: 2026-04-15
---

## 2026-04-15 10:20 — Kickoff
Goal for the session: <restate>.
Current state of the world: <what is already in place>.
Plan: <rough steps>.
```

Rules:

- `NOTES.md` is append-only.
- Timestamp headings use `## YYYY-MM-DD HH:MM — short label` in the user's
  local time.
- Capture what was done, what was learned, and what is next.
- If an earlier note is wrong, append a correction later.
- Do not create `SUMMARY.md`; `session-close` writes it.
