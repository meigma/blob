---
name: session-continue
description: Continue an existing journal session by ID. Invoke only when the user explicitly asks to continue a specific session. Requires prior session-setup, then takes one argument, the session ID (e.g. "1", "003"). The argument follows the skill name in the user's invocation.
argument-hint: <session-id>
disable-model-invocation: true
---

The user is **continuing an existing session** in this workspace. The session ID is passed as the argument to this skill — treat any numeric argument the user provided as the session ID, resolving it against the zero-padded folder names under the journal root's `.journal/` (e.g. `1` → `001`, `12` → `012`).

Follow the session protocol defined in `.session.md` at the workspace root — do not restate it, follow it.

Specifically:

1. **Verify session setup (mandatory, first):** Resolve the developer identity with `gh api user --jq .login`, then locate an existing Worktrunk worktree for `journal/<login>` with `wt list --format=json`. If no worktree exists, stop and tell the developer to run `session-setup` before continuing a session. Do not create the journal branch here.
2. **Resolve the target session:** Resolve the user-provided session ID to the matching `<journal-root>/.journal/<ID>/` folder. If the folder does not exist, stop and ask the user to clarify before doing anything else. The explicit ID is authoritative; other open sessions are expected and must not be surfaced as warnings or ambiguity. The default write set is only `.journal/<ID>/`.
3. **Prepare the journal root:** Verify `.journal/INDEX.md`, `.journal/SKILLS.md`, and `.journal/TECH_NOTES.md` exist. If they are missing, stop and tell the developer to rerun `session-setup`. Apply the concurrent journal ownership rule from `.session.md`; dirty paths outside the target write set are expected and non-blocking.
4. **Startup:** Read `<journal-root>/.journal/SKILLS.md` if present and load every required skill listed there. Read `<journal-root>/.journal/TECH_NOTES.md` if present. Then read the `SUMMARY.md` of the last three closed sessions in `<journal-root>/.journal/` (skip sessions without a `SUMMARY.md`; read fewer if fewer exist). Do **not** read their `NOTES.md` files at this step. This startup read is required in addition to the continuing-session reads below.
5. **Resume the target session:**
   - Read `<journal-root>/.journal/<ID>/NOTES.md` **in full**, top to bottom. This is your primary context for the session.
   - Read `<journal-root>/.journal/<ID>/SUMMARY.md` if it exists (the session may have been closed and is being reopened).
6. **Log the resume:** Determine first whether `<journal-root>/.journal/INDEX.md` needs an `in-progress` update. If it does, require only `INDEX.md` to have no pre-existing uncommitted edits and add it to the write set. Then append a new `## <timestamp> — Resume` entry to `NOTES.md` stating your understanding of the current state and what you're about to do, and add or update the `INDEX.md` row when required. If the session was previously `complete` or `abandoned`, say in the resume entry that it was reopened.
7. **Record the journal mutation:** Stage only the write set with `git add -f -- <write-set>`, then commit only it with `git commit --only -m "docs(journal): resume session <ID>" -- <write-set>`. Push `journal/<login>` and use the bounded rejected-push retry from `.session.md`. Never include another session's dirty or staged files.
8. **Bind the current session:** After the resume mutation is committed and pushed successfully, bind `<ID>` as the current session for this task. Replace any prior task-local binding without modifying or warning about the previously bound session. Do not change the binding if target validation or the resume mutation fails.
9. Confirm to the user that the session is resumed, that it is now the current session for this task, and which journal branch was updated, then wait for their actual request.

Do not proceed with any substantive work until the resume entry is logged.
