# Worktrunk Command Map

This is a curated operator reference for `wt 0.37.1`. Prefer local `--help` output and
official docs if anything here drifts.

## Global flags

These apply across `wt` commands:

- `-C <path>`: run as if started in another working directory
- `--config <path>`: use a specific user config file
- `-v`, `-vv`: increase logging verbosity

## Core lifecycle commands

### `wt switch`

Purpose: switch to a branch's worktree, or create a new branch/worktree when asked.

Usage:

```bash
wt switch [OPTIONS] [BRANCH] [-- <EXECUTE_ARGS>...]
```

Common flags:

- `-c`, `--create`: create a new branch
- `-b`, `--base <BASE>`: base branch for `--create`; defaults to the default branch
- `-x`, `--execute <CMD>`: replace `wt` with another command after switching
- `--clobber`: remove a stale non-worktree path at the target location
- `--no-cd`: skip directory change after switching
- `--branches`: in picker mode, include branches without worktrees
- `--remotes`: in picker mode, include remote branches
- `-y`, `--yes`: skip approval prompts
- `--no-hooks`: skip hooks
- `--format text|json`: structured output for automation

Branch shortcuts:

- `^`: default branch
- `-`: previous worktree
- `@`: current worktree
- `pr:<N>`: GitHub PR branch
- `mr:<N>`: GitLab MR branch

Notes:

- With no branch argument, `wt switch` opens an interactive picker.
- After remotes have been fetched, an explicit `wt switch <branch>` also works when the
  branch exists only on a single remote: Worktrunk creates the local tracking branch and its
  worktree. Do not pass `--create` for that case.
- `pr:<N>` requires `gh`; `mr:<N>` requires `glab`.
- For automation, prefer explicit branch names or `--format=json`.

### `wt list`

Purpose: inspect worktrees, branch state, divergence, and optional CI/summary data.

Usage:

```bash
wt list [OPTIONS]
wt list statusline
```

Common flags:

- `--format table|json`: default is `table`
- `--branches`: include branches without worktrees
- `--remotes`: include remote branches
- `--full`: include CI status, diff analysis, and LLM summaries
- `--progressive`: render fast local data first, then remote/slow data

Notes:

- `wt list --format=json` is the best entrypoint for agents.
- `statusline` is a subcommand for shell/status integrations.

### `wt remove`

Purpose: remove a worktree and optionally delete its branch.

Usage:

```bash
wt remove [OPTIONS] [BRANCHES]...
```

Common flags:

- `--no-delete-branch`: keep the branch after removal
- `-D`, `--force-delete`: delete an unmerged branch
- `-f`, `--force`: remove worktree even with untracked files
- `--foreground`: run removal synchronously
- `-y`, `--yes`: skip approval prompts
- `--no-hooks`: skip hooks
- `--format text|json`

Notes:

- Defaults to the current worktree.
- Removal runs in the background by default.
- Worktrunk can detect squash-merged or otherwise integrated branches before deleting them.

## Local integration commands

Understand these, but do not recommend them as the default agent workflow.

### `wt merge`

Purpose: merge the current branch into the target branch locally, typically with squash,
rebase, and cleanup.

Usage:

```bash
wt merge [OPTIONS] [TARGET]
```

Common flags:

- `--no-squash`
- `--no-commit`
- `--no-rebase`
- `--no-remove`
- `--no-ff`
- `--stage all|tracked|none`
- `-y`, `--yes`
- `--no-hooks`
- `--format text|json`

Important:

- This updates the local target branch, usually the default branch.
- This is not a GitHub PR merge.
- Prefer `git push` plus `gh pr create` / `gh pr status` / `gh pr checks` instead.

### `wt step`

Purpose: run lower-level lifecycle operations and utilities.

Usage:

```bash
wt step <COMMAND>
```

Subcommands:

- `commit`: stage and commit with an LLM-generated message
- `squash`: squash commits since branching
- `rebase`: rebase current branch onto target
- `push`: fast-forward the local target branch to current branch
- `diff`: show all changes since branching
- `copy-ignored`: copy gitignored files to another worktree
- `eval`: evaluate a template expression
- `for-each`: run a command in every worktree
- `promote`: swap a branch into the main worktree
- `prune`: remove integrated worktrees and branches
- `relocate`: move worktrees to expected paths

Common subcommand flags:

- `wt step commit --stage all|tracked|none --show-prompt`
- `wt step squash --stage all|tracked|none --show-prompt`
- `wt step rebase [TARGET]`
- `wt step push [TARGET] [--no-ff]`
- `wt step prune --dry-run --min-age <DURATION> --foreground`
- `wt step for-each -- <COMMAND>`

Important:

- `wt step push` is local target-branch integration. It is similar to
  `git push . HEAD:<target>`, not `git push origin`.
- `wt step prune --dry-run` is the safe preview before batch cleanup.

## Configuration and hooks

### `wt config`

Purpose: manage shell integration, config files, plugins, and internal state.

Subcommands:

- `wt config shell install`: install shell integration so `wt switch` can change directories
- `wt config shell init`: print shell integration code
- `wt config shell uninstall`
- `wt config shell show-theme`
- `wt config create`: create user config
- `wt config create --project`: create `.config/wt.toml`
- `wt config show [--full] [--format text|json]`
- `wt config update`: update deprecated config settings
- `wt config plugins`: plugin management
- `wt config state`: inspect default branch, markers, vars, logs, CI cache, and more

Useful `state` subcommands:

- `default-branch`
- `previous-branch`
- `logs`
- `ci-status`
- `marker`
- `vars`

### `wt hook`

Purpose: run or inspect lifecycle hooks and manage approvals.

Hook subcommands:

- `show`
- `pre-switch`, `post-switch`
- `pre-start`, `post-start`
- `pre-commit`, `post-commit`
- `pre-merge`, `post-merge`
- `pre-remove`, `post-remove`
- `approvals`

Notes:

- `pre-*` hooks block and can abort an operation.
- `post-*` hooks run in the background.
- Hooks can live in user config or project config.

## PR-oriented workflow to prefer

```bash
wt list --format=json --branches
wt switch --create feature/some-task
# edit, test, commit
git push -u origin HEAD
gh pr create --fill
gh pr status
gh pr checks
```

Cleanup after merge:

```bash
wt remove feature/some-task
# or, in batches:
wt step prune --dry-run
wt step prune
```
