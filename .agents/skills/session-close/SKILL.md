---
name: session-close
description: Close out a journal session. Invoke only when the user explicitly asks to close, wrap up, or end a session. Requires prior session-setup, commits work, opens PRs, waits for review, squash-merges, updates local default branches, then writes SUMMARY.md and updates INDEX.md in the developer's personal journal branch per .session.md. Accepts an optional session ID argument; without one it closes the current session bound to this task.
argument-hint: [session-id]
disable-model-invocation: true
---

The user is **closing a session**. Follow the session protocol defined in `.session.md` at the workspace root — do not restate it, follow it.

The close-out has three phases: **verify the personal journal worktree**, **land the work**, then **record the session**. Do them in order. Do not write `SUMMARY.md` before the PRs are merged — the summary is a postmortem of what actually happened, not what you intend to happen.

---

## Phase 0 — Verify the personal journal worktree

Resolve the developer identity with `gh api user --jq .login`, then locate an
existing Worktrunk worktree for `journal/<login>` with `wt list --format=json`
and use that worktree as the journal root. If no worktree exists, stop and tell
the developer to run `session-setup` before closing a session. Do not create the
journal branch here.

Verify `.journal/INDEX.md`, `.journal/SKILLS.md`, and `.journal/TECH_NOTES.md`
exist. If the root journal files are missing, stop and tell the developer to
rerun `session-setup`.

Resolve the session being closed before landing work:

- If the user provided a numeric session ID, normalize it to the zero-padded
  folder name (`1` -> `001`) and use that session for this close operation. An
  explicit ID selects the close target without replacing the current task-local
  binding; if it names a different session, preserve the existing binding
  unchanged.
- If no ID was provided, use the current session binding established earlier in
  this task by a successful `session-new` or `session-continue`.
- If no ID was provided and this task has no current session binding, stop and
  ask the user for an explicit session ID. Do not inspect or count
  `in-progress` rows or incomplete folders to choose a target: those sessions
  may belong to other concurrent tasks.
- Verify that `.journal/<ID>/NOTES.md` exists and that its `INDEX.md` row is
  `in-progress` before continuing. (`SUMMARY.md` may still exist after a session
  was reopened, so it is not the status authority.) If the resolved target is
  invalid, missing, or already closed, report that exact target condition and
  do not fall through to another session.

Read the resolved target's `.journal/<ID>/NOTES.md` in full before landing work;
it defines which repos and branches belong to the closeout. If a reopened
session still has a prior `SUMMARY.md`, read it as historical context while
treating the current `INDEX.md` status and `NOTES.md` as authoritative.

Treat both the task-local binding and the resolved close target as durable task
context. Preserve them across intermediate turns, context summarization, and
the Phase 1 pause for PR review. When the user returns after review, continue
closing the already resolved target instead of resolving it again.

Apply the concurrent journal ownership rule from `.session.md`. The default
write set is `.journal/<ID>/`; dirty paths in every other session folder are
expected, non-blocking parallel work and must remain untouched. Do not run
`pull --rebase` merely to prepare the shared journal worktree.

## Phase 1 — Land the work

For **every repo** with uncommitted changes or an unmerged branch that belongs to the target session, walk through these steps. Leave unrelated active-session branches alone. Do not skip a target-session repo because "nothing really changed" — run `git status` and verify.

1. **Reject journal contamination.** In every non-journal implementation branch, run `git ls-files .journal`. If it prints any tracked file, stop and remove those files from the implementation branch before continuing. Only `journal/<login>` may force-add `.journal/` paths.
2. **Commit all changes.** In each worktree that has work, stage and commit with a clear message. If there are logically distinct changes, use multiple commits. Do not commit unrelated changes together (see the Worktrunk rule in `.session.md`: one worktree per PR).
3. **Push the branch.** `git push -u origin HEAD` if the branch has no upstream; `git push` otherwise.
4. **Open a PR.** Use `gh pr create` with a title and body that explain the change. If multiple repos have work, open one PR per repo (or per logical change, if a repo has more than one). Surface the PR URL to the user.
5. **Wait for user review.** After opening each PR, **stop and wait** for the user to review. Preserve the resolved close target and current task-local binding while paused. Do not proceed to merge on your own. The user will indicate when a PR is approved and ready to merge — possibly after requesting changes that you then address in additional commits. Never merge unreviewed.
6. **Squash-merge after approval.** Once the user approves, merge with `gh pr merge --squash --delete-branch`. **Always squash.** Never use `--merge` or `--rebase`. This applies to every PR in every repo for this session, without exception.
7. **Update the local default branch.** After merge, in the main (non-worktree) checkout of that repo, detect the default branch with `git remote show origin | sed -n 's/  HEAD branch: //p'` (do not assume `main` or `master`), then fetch and fast-forward: `git fetch origin && git checkout <default> && git pull --ff-only`. If fast-forward fails, stop and surface the problem to the user rather than resolving via merge or reset.
8. **Remove the session worktree.** Once the branch is merged and the local default is updated, clean up the Worktrunk worktree with `wt remove` (see the `worktrunk` skill). Do not leave stale worktrees.

Repeat for each repo. Only once **every** PR for the session is merged and **every** local default branch is fast-forwarded should you move to Phase 2.

If the user decides to abandon rather than merge one of the PRs, close it with `gh pr close` and delete the branch; record the abandonment explicitly in `SUMMARY.md` under Outcome or Open Threads.

## Phase 2 — Record the session

Only after Phase 1 is complete:

1. **Finalize the write set before writing anything.** It always contains `.journal/<ID>/` and `.journal/INDEX.md`; add `.journal/TECH_NOTES.md` only if the session produced durable technical context. Require each shared file in this write set—not the entire worktree—to have no pre-existing uncommitted edits. Dirty paths outside this write set remain non-blocking.
2. **Write `SUMMARY.md`** at `<journal-root>/.journal/<ID>/SUMMARY.md` using `references/session-artifacts.md`. It is a postmortem written for another agent reading cold — cover goal, outcome (state plainly whether it was met), key decisions with reasons, changes, open threads, and references.
3. **Update `<journal-root>/.journal/INDEX.md`.** Add or update the row for this session. Set `Status` to `complete` (or `abandoned` if the work was dropped). Keep the summary cell to one sentence. Rows stay ordered oldest → newest.
4. **Update `<journal-root>/.journal/TECH_NOTES.md` if it is in the write set.** Revise it so future agents inherit the durable context. Keep it small; do not copy the session log into it.
5. **Append a final `NOTES.md` entry** using `references/session-artifacts.md`, pointing at the merged PRs and summarizing the hand-off state. `NOTES.md` is append-only — do not rewrite prior entries.
6. **Commit and push the journal mutation.** Stage only the write set with `git add -f -- <write-set>`, then commit only it with `git commit --only -m "docs(journal): close session <ID>" -- <write-set>`. Push `journal/<login>` and use the bounded rejected-push retry from `.session.md`. Never include another session's dirty or staged files.
7. **Update the task-local binding.** Only after the closeout journal mutation is committed and pushed successfully, clear the current binding if it equals `<ID>`. If an explicit ID selected a different session, preserve the existing binding unchanged. Do not clear or switch the binding when closeout fails or remains paused.
8. **Confirm to the user** what was recorded: the session ID, the PRs merged, the repos whose local default was updated, the fact that `SUMMARY.md` and `INDEX.md` were written, whether `TECH_NOTES.md` was updated, which journal branch was pushed, and whether the current task-local binding was cleared or preserved.

---

Do not skip the review-and-wait step in Phase 1. The whole point of opening PRs instead of merging locally is that the user sees the changes on GitHub before they land. Merging without explicit approval is a principle violation even if the change looks obviously correct.
