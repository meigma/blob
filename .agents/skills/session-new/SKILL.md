---
name: session-new
description: Start a new journal session in this workspace. Invoke only when the user explicitly asks to start a new session (e.g. "new session", "start a session"). Requires prior session-setup, then primes the next .journal/<ID>/ folder in the developer's personal journal worktree per .session.md. Does not accept arguments.
disable-model-invocation: true
---

The user is starting a **new session** in this workspace. Follow the session protocol defined in `.session.md` at the workspace root — do not restate it, follow it.

An explicit new-session request is authoritative. Start a new session even when
other sessions are already `in-progress`; their existence is expected and must
not be surfaced as a warning, reminder, ambiguity, or request for confirmation.
Do not reuse, continue, close, or otherwise modify another session instead.

Specifically:

1. **Verify session setup (mandatory, first):** Resolve the developer identity with `gh api user --jq .login`, then locate an existing Worktrunk worktree for `journal/<login>` with `wt list --format=json`. If no worktree exists, stop and tell the developer to run `session-setup` before starting a session. Do not create the journal branch here.
2. **Prepare the journal root:** Verify `.journal/INDEX.md`, `.journal/SKILLS.md`, and `.journal/TECH_NOTES.md` exist. If they are missing, stop and tell the developer to rerun `session-setup`. Apply the concurrent journal ownership rule from `.session.md`: dirty active-session folders are expected, do not block startup, and must not be checkpointed or committed by this operation.
3. **Startup:** Read `<journal-root>/.journal/SKILLS.md` if present and load every required skill listed there. Read `<journal-root>/.journal/TECH_NOTES.md` if present. Then read the `SUMMARY.md` of the last three closed sessions in `<journal-root>/.journal/` (skip sessions without a `SUMMARY.md`; read fewer if fewer exist). Do **not** read their `NOTES.md` files.
4. **Prime the new session:** Read `references/notes-template.md`, then:
   - Find the highest existing session ID under `<journal-root>/.journal/` and increment by 1 (zero-padded, 3 digits). If no session folders exist, start at `001`.
   - Define the write set as `.journal/<ID>/` plus `.journal/INDEX.md`. Before creating the session, require only `INDEX.md`—not the entire worktree—to have no pre-existing uncommitted edits.
   - Create `<journal-root>/.journal/<ID>/` as the allocation attempt. If the candidate already exists or appears during creation because another concurrent `session-new` won the race, recompute the highest ID, update `<ID>` and the write set, repeat the scoped `INDEX.md` check, and retry the allocation once. If that second candidate is also taken, stop on the actual allocation collision. Never touch the session folder that won the race.
   - Create `<journal-root>/.journal/<ID>/NOTES.md` from the template, then append an initial `## <timestamp> — Kickoff` entry capturing the user's stated goal and the current state of the world.
   - Do **not** create `SUMMARY.md` — that's written at session close.
   - Add an `in-progress` row for the new session to `.journal/INDEX.md`. Derive a short title from the user's stated goal, use today's date, keep rows ordered oldest to newest, and keep the summary cell to one sentence.
5. **Record the journal mutation:** Stage only the write set with `git add -f -- .journal/<ID> .journal/INDEX.md`, then commit only it with `git commit --only -m "docs(journal): start session <ID>" -- .journal/<ID> .journal/INDEX.md`. Push `journal/<login>` and use the bounded rejected-push retry from `.session.md`. Never include another session's dirty or staged files.
6. **Bind the current session:** After the journal mutation is committed and pushed successfully, bind `<ID>` as the current session for this task. Replace any prior task-local binding without modifying or warning about the previously bound session. Do not change the binding if priming fails.
7. Confirm to the user which session ID was created, that it is now the current session for this task, and which journal branch was updated, then wait for their actual request.

Do not proceed with any substantive work until priming is complete.
