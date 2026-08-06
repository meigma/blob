---
name: journal-sync
description: Synchronize remote teammate journal branches into local Worktrunk worktrees for read-only context discovery. Invoke only when the user explicitly asks to sync, fetch, refresh, or discover team journals. Never run automatically during startup or another lifecycle command.
---

# Journal Sync

Mirror teammates' published journals locally without authoring journal content.
Canonical branch names are `journal/<github-login>`; paths such as
`.wt/journal-<github-login>` are Worktrunk-sanitized worktree paths, not branch
names.

## Guardrails

- Exclude the current user's `journal/<login>` branch from sync decisions,
  per-worktree Git status inspection, worktree creation, and pulls. The shared
  Worktrunk inventory may be used only to report whether it exists;
  `session-setup` owns that personal worktree.
- Treat every teammate journal worktree as a read-only reference mirror. Never
  edit journal files or stage, commit, push, rebase, stash, reset, clean,
  force-update, delete, or prune a local branch or worktree during sync. Fetch
  may prune stale `origin/journal/*` remote-tracking refs only.
- Leave local journal branches and worktrees that no longer exist remotely in
  place. Report them as retained; cleanup is outside this skill.
- Isolate failures by branch. A dirty, ahead, diverged, or broken peer must not
  prevent other peers from syncing.

## Workflow

1. Resolve and verify prerequisites:

   ```bash
   command -v wt
   gh auth status
   login=$(gh api user --jq .login)
   personal="journal/$login"
   git remote get-url origin
   wt config show --full
   ```

   Require the Worktrunk path template
   `{{ repo_path }}/.wt/{{ branch | sanitize }}`. Stop the entire operation if
   identity, `origin`, Worktrunk, or its path configuration cannot be resolved;
   the personal branch cannot otherwise be excluded safely.

2. Query the remote directly and retain that output as the authoritative sync
   snapshot:

   ```bash
   git ls-remote --heads origin 'refs/heads/journal/*'
   ```

   Strip `refs/heads/` to obtain branch names. Accept canonical
   `journal/<login>` refs only; report malformed nested or empty-owner refs
   without creating worktrees for them. Exclude `$personal` before inspecting
   any worktree.

3. Fetch and prune only the journal remote-tracking namespace, then inventory
   local branches and worktrees:

   ```bash
   git fetch origin --prune \
     '+refs/heads/journal/*:refs/remotes/origin/journal/*'
   wt list --format=json --branches
   ```

   Use only branches from the remote snapshot for sync decisions; stale local
   remote-tracking refs must not masquerade as current remote branches. Compare
   the inventory with the snapshot so local peer branches or worktrees whose
   remote disappeared can be reported as retained without touching them.

   Use the inventory only to determine whether the exact personal worktree is
   present. If it is missing, report that the user should run `session-setup`,
   but continue syncing peers. Do not create, inspect the status of, or pull the
   personal worktree.

4. For each teammate branch in the snapshot, locate its exact worktree in the
   Worktrunk JSON. If none exists, create or open its tracking worktree:

   ```bash
   wt switch --no-cd --no-hooks --format=json -y "journal/<login>"
   ```

   Take the path from Worktrunk's JSON rather than constructing it. If creation
   fails, refresh `wt list --format=json --branches` once: another process may
   have created the worktree concurrently. Continue when the exact branch now
   has a worktree; otherwise record a branch-local failure.

   Hooks are disabled because repository hooks may author files or continue
   asynchronously after Worktrunk returns, violating the read-only peer rule.

5. Verify the worktree checks out the expected branch. Record its starting
   `HEAD`, then inspect it without changing it:

   ```bash
   git -C "<worktree-path>" branch --show-current
   git -C "<worktree-path>" status --porcelain=v1 --untracked-files=all
   git -C "<worktree-path>" rev-list --left-right --count \
     HEAD...refs/remotes/origin/journal/<login>
   ```

   If the branch is wrong or status is non-empty, do not pull. Record the reason
   and continue with the next teammate. Interpret the revision counts as
   `<local-only> <remote-only>`; if the local-only count is nonzero, the branch
   is ahead or diverged. Do not pull it. Record the counts and continue.

6. Fast-forward from the exact remote branch and verify an exact mirror:

   ```bash
   git -C "<worktree-path>" -c core.hooksPath=/dev/null \
     pull --ff-only origin "journal/<login>"
   git -C "<worktree-path>" rev-parse HEAD
   git -C "<worktree-path>" rev-parse "refs/remotes/origin/journal/<login>"
   ```

   Success requires the two revisions to match. If the pull fails or a
   concurrent change leaves the branch ahead/diverged, classify it again with
   `git rev-list --left-right --count HEAD...refs/remotes/origin/journal/<login>`,
   leave it untouched, and continue. Never repair divergence in this skill.
   Disable Git hooks for the pull so a peer fast-forward cannot run a local
   `post-merge` hook that authors files.

7. Return one aggregate report containing remote journals discovered, the
   personal branch skipped, peer worktrees created, updated, already current,
   retained because their remote disappeared, and blocked with exact reasons.
   When no canonical teammate branches exist, return a successful no-op.
