---
name: git
description: >
  Use for day-to-day Git workflow when creating commits, choosing commit scopes,
  preparing PR titles, or deciding how changes should be merged. Enforces a
  GitHub-only squash-merge policy, Conventional Commit subjects, concise commit
  bodies, and a commit-often workflow during branch development.
---

# Git Workflow

Use this skill as a small operating policy for Git usage. The point is to make
different agents converge on the same workflow instead of inventing one from
training priors.

## Default stance

1. All integration merges happen on GitHub, never locally.
2. All GitHub merges should be squash merges.
3. PR titles must use Conventional Commit format, because the squash-merge
   commit on the main branch will inherit the PR title.
4. Commit often while working on a branch. Branch history is temporary working
   history, not the final shape of `main`.
5. Do not waste time trying to make every local commit perfect. Keep commits
   useful, focused, and frequent, then rely on squash merge to produce the
   final main-branch commit.

## Merge policy

- Do not locally merge feature branches into `main`, `master`, or other shared
  integration branches.
- Do not treat a local merge commit as the final integration step.
- Use GitHub's squash merge button or `gh pr merge --squash`.
- The PR title must already be the final commit subject you want on the target
  branch.

## Commit subject format

Use Conventional Commits:

```text
type(scope): short imperative summary
```

or, when no scope is needed:

```text
type: short imperative summary
```

Use standard types unless the repository clearly uses a different established
set:

- `feat`
- `fix`
- `refactor`
- `docs`
- `test`
- `ci`
- `build`
- `chore`
- `perf`
- `style`
- `revert`

Subject rules:

- Use imperative mood: `fix cache invalidation`, not `fixed` or `fixes`.
- Keep the type and scope lowercase.
- Do not end the subject with a period.
- Make the subject specific enough to stand alone in `git log`.

## Scope guidance

Scopes are optional in small repos and expected in larger or mixed repos.

Good natural scopes include:

- a project in a monorepo
- a Go package in a larger Go codebase
- a service, subsystem, or app in a multi-system repository
- a clearly named operational area such as `ci`, `release`, or `docs`

When in doubt, inspect recent history and reuse the existing vocabulary instead
of inventing a new scope:

```bash
git log --format='%s' -n 100
```

If the repository already has a consistent scope pattern, follow it.

## Commit bodies

There is no single standard body template. Keep bodies concise and use them to
cover what the diff alone does not explain clearly, usually:

- why the change exists
- any important constraint or tradeoff
- any reviewer or operator note that should survive in history

Default shape:

```text
type(scope): short imperative summary

Why this change is needed.
Any critical context or operational note.
```

Rules:

- Leave a blank line between subject and body.
- Omit the body entirely when the subject and diff are already obvious.
- Prefer short paragraphs or a few bullets over long prose.
- Do not invent author identity inside the message.
- If the repository requires trailers such as `Signed-off-by`, use Git's
  configured identity and built-in support such as `git commit -s` instead of
  typing made-up author lines by hand.

## PR titles

PR titles must always use Conventional Commit format.

Examples:

- `fix(api): handle empty webhook payload`
- `feat(cli): add JSON output for status`
- `docs: clarify release process`

Because the branch is squash-merged, the PR title is the final commit title on
the target branch. Treat the PR title as merge-ready, not as a temporary review
label.

## PR bodies

There is no universal PR body template worth enforcing rigidly. Keep the body
useful to reviewers and future readers. In most repos, a concise PR body should
cover:

- summary of the change
- linked issue or context
- testing or validation performed
- any rollout, migration, or reviewer notes

## Branch work cadence

- Commit early when you establish a direction.
- Commit again when a piece of work becomes testable or reviewable.
- Commit after meaningful fixes during review.
- Do not hold a large pile of unstaged work just to chase an ideal commit graph.

The branch history is allowed to be messy as long as it is understandable. The
main branch history is cleaned up by squash merge.
