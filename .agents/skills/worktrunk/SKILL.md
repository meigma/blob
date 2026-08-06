---
name: worktrunk
description: >
  Manage Git worktrees with Worktrunk (`wt`) for parallel human and agent workflows.
  Use when a repository uses `wt`, `.config/wt.toml`, or worktree-based branch isolation,
  or when you need to create, inspect, clean up, or coordinate isolated worktrees safely.
  Prefer PR-based integration with `gh pr`; do not default to Worktrunk's local merge flow.
---

# Worktrunk

Use this skill as an operator guide for multi-worktree, multi-agent Git workflows.
Ground advice in the official docs and the local CLI help, not memory.

## Verified against

- Docs: https://worktrunk.dev
- GitHub: https://github.com/max-sixty/worktrunk
- Local CLI used for command grounding: `wt 0.37.1`

## Use this skill when

- A repository already uses Worktrunk or has a `.config/wt.toml`.
- You need to give each agent or user an isolated branch and worktree.
- You need to inspect existing worktrees before starting so changes do not collide.
- You need to explain or operate `wt switch`, `wt list`, `wt remove`, `wt config`,
  `wt step`, or hooks.
- You need a PR-oriented workflow around worktrees.

## Default stance

1. Give each user or agent its own implementation branch and worktree. Do not share an
   implementation working directory. The exact `journal/<current-gh-login>` worktree
   governed by `.session.md` is the deliberate exception: the current user's concurrent
   agents share it while owning disjoint journal write sets. Other `journal/<login>`
   worktrees are read-only peer references; only `journal-sync` may materialize or
   fast-forward them.
2. Use `wt` for creation, switching, inspection, and cleanup so naming, hooks, and lifecycle
   stay consistent.
3. Prefer remote PR flow for integration:
   - push the branch with normal Git
   - create and manage review with `gh pr`
4. Do not default to `wt merge` or `wt step push`.
   - `wt merge` is a local integration pipeline that rebases, updates the local target branch,
     and usually removes the worktree.
   - `wt step push` fast-forwards the local target branch. It is not `git push origin`.
5. Use `wt remove` and `wt step prune` only after review or after the PR is merged or abandoned.

## Non-negotiables for agents

- Before creating a branch, inspect existing worktrees first.
- Never assume your current directory is isolated. Verify it.
- Do not reuse another agent's implementation worktree unless the user explicitly asks for
  that handoff. Sharing a personal journal worktree is allowed only under the scoped
  ownership rules in `.session.md`.
- Resolve the current GitHub login before treating any journal worktree as personal. Never
  write lifecycle state in, reuse, or repurpose a peer's `journal/<other-login>` worktree;
  ordinary agents may only read it for context, and only `journal-sync` may update it.
- Avoid interactive picker flows in automation; prefer explicit branch names and JSON output.
- If shell integration is not installed, `wt switch` cannot change the parent shell's
  directory. Run `wt config shell install` once per machine, or use `--no-cd` plus the
  returned path/JSON in automation.

## Fast inspection workflow

Run these first when you are orienting yourself in a repo:

```bash
wt --version
git rev-parse --show-toplevel
git branch --show-current
wt list --format=json
wt config show --full
```

Use `wt list --format=json` for tool-driven inspection. Do not scrape the table output unless
you have to.

## Safe agent workflow

### 1. Inspect current state

Start by checking whether the branch or worktree already exists:

```bash
wt list --format=json
wt list --format=json --branches
```

Look for:

- `is_current`: where you are now
- `branch` and `path`: existing worktree ownership and location
- `kind == "branch"`: branches that exist without worktrees
- `symbols` / `main_state`: whether a branch is dirty, ahead, integrated, conflicted, or safe
  to prune

If the target branch already has a live worktree, treat that as owned until the user says
otherwise.

For journal branches, ownership is determined by the exact current GitHub login, not by
which journal worktree happens to be current. A peer journal worktree remains a read-only
reference even when the agent starts inside it.

### 2. Create an isolated worktree

Use a unique branch name. Good patterns include ticket IDs or explicit agent prefixes.

```bash
wt switch --create feature/some-task
wt switch --create --base main feature/some-task
wt switch --create --base @ fix/current-context
```

Notes:

- `--create` creates a new branch.
- `--base` defaults to the repo's default branch.
- `@` means the current branch/worktree, `^` means the default branch, and `-` means the
  previous worktree.
- If the branch already exists, `wt switch <branch>` switches to its worktree or creates one
  for that branch.

For agent launchers, `wt switch -x <command>` can create the worktree and hand control to the
tool immediately.

### 3. Work only inside that worktree

Once the worktree exists:

- make edits only there
- run tests there
- commit there
- avoid switching the branch in-place with `git switch` unless the user explicitly wants to
  repurpose that worktree

These edit rules apply to implementation worktrees and to the current user's personal
journal under `.session.md`. Never perform implementation or lifecycle work in a peer
journal worktree; peer journals are reference-only outside `journal-sync` fast-forwards.

Optional coordination helpers:

- `wt config state marker set <marker>` can label a branch manually
- `wt list` surfaces markers, ahead/behind state, and optional CI/summary data

### 4. Update your branch without using local merge flow

If the base branch moved, refresh it first, then rebase your branch as needed.

Use Worktrunk's rebase helper only when you mean "rebase my current branch onto the target
branch locally":

```bash
wt step rebase
wt step rebase develop
```

This is fine for keeping a worktree current. It is not the same as merging back to the source
repo.

### 5. Use normal Git push and `gh pr` for integration

Default integration flow:

```bash
git push -u origin HEAD
gh pr create --fill
gh pr status
gh pr checks
gh pr view --web
```

Use `gh pr edit`, `gh pr comment`, `gh pr ready`, `gh pr reopen`, and related `gh pr`
commands for ongoing review management.

For existing PRs, Worktrunk can still help you isolate the branch locally:

```bash
wt switch pr:123
```

That resolves the PR branch and opens or creates the corresponding worktree. It requires `gh`
to be installed and authenticated.

### 6. Clean up after merge or abandonment

After the PR is merged, or if the branch is intentionally discarded:

```bash
wt remove
wt remove feature/some-task
wt step prune --dry-run
wt step prune
```

Routine implementation cleanup must exclude every `journal/*` worktree. Do not reuse,
repurpose, or remove peer journal worktrees, and do not run `wt step prune` when its dry run
would remove one. The session protocol manages the current user's journal, while
`journal-sync` manages peer reference worktrees.

Important details:

- `wt remove` defaults to the current worktree.
- It deletes the branch only when Worktrunk determines it is merged or otherwise integrated,
  unless you force or override that behavior.
- `wt step prune` bulk-removes integrated worktrees and branches. Review with `--dry-run`
  first.

## What to avoid by default

- `wt merge`
- `wt step push`
- "just merge it back locally" advice

Why:

- PRs preserve review, CI, audit trail, and shared branch protection rules.
- Local merge flows are easier to misuse in multi-agent setups because they update local target
  branches directly.
- `wt merge` also tends to clean up immediately, which can be the wrong default while a review
  is still active.

Only recommend `wt merge` if the user explicitly wants a local-only integration workflow and
understands that it is not the same as GitHub PR merge.

## Command reference

See [references/commands.md](references/commands.md) for the current command map and common
flags.

## Maintenance checklist

Re-verify this skill when Worktrunk releases a new minor or major version:

1. Re-open:
   - https://worktrunk.dev/switch/
   - https://worktrunk.dev/list/
   - https://worktrunk.dev/remove/
   - https://worktrunk.dev/merge/
   - https://worktrunk.dev/config/
   - https://worktrunk.dev/step/
   - https://worktrunk.dev/hook/
   - https://worktrunk.dev/claude-code/
2. Re-run locally:
   - `wt --version`
   - `wt --help`
   - `wt switch --help`
   - `wt list --help`
   - `wt remove --help`
   - `wt merge --help`
   - `wt config --help`
   - `wt step --help`
   - `wt hook --help`
3. Delete or rewrite any recommendation that can no longer be grounded in the docs or CLI help.
