---
name: session-setup
description: First-run developer onboarding for the session journal system in a repository that already has the framework installed. Creates or opens the developer's personal journal branch, initializes .journal, pushes it, and explains how to use sessions. Invoke when the user asks for session setup, setup sessions, onboarding, or first-time journal setup.
disable-model-invocation: true
---

The user is asking to set up their developer journal for this workspace. Follow
the protocol defined in `.session.md` at the workspace root — do not restate it,
follow it.

This is the only skill that creates or initializes the developer's personal
journal branch. The personal branch is exactly `journal/<login>`, where
`<login>` is the current result of `gh api user --jq .login`.

Specifically:

1. **Verify prerequisites before changing anything:**
   - `command -v wt`
   - `gh auth status`
   - `gh api user --jq .login`
   - `git remote get-url origin`
   - `git remote show origin | sed -n 's/  HEAD branch: //p'`
   - `wt config show --full` must show `worktree-path = "{{ repo_path }}/.wt/{{ branch | sanitize }}"`
   If any prerequisite fails, stop and tell the developer what is missing. Do
   not install tools, run `gh auth login`, or edit Worktrunk config yourself.
2. **Resolve setup values:**
   - GitHub login from `gh api user --jq .login`
   - default branch from `git remote show origin`
   - personal journal branch `journal/<login>`
   - Treat only that exact branch as the writable setup and lifecycle target.
     Every `journal/<other-login>` branch belongs to a peer and must not be used
     as a fallback personal journal root.
3. **Fetch and inspect:**
   - Run `git fetch origin --prune`.
   - Run `wt list --format=json`.
   - Ignore peer journal worktrees when resolving the personal journal root.
     They are read-only context sources; only `journal-sync` may materialize or
     fast-forward them.
   - If an existing worktree for `journal/<login>` is present, use that as the journal root. Apply the concurrent journal ownership rule from `.session.md`: setup does not own active session folders, must ignore their dirty paths, and must not checkpoint or commit them. Do not run `pull --rebase` merely to prepare the shared journal worktree.
4. **Open an existing journal branch if needed:**
   - If no worktree exists but `origin/journal/<login>` exists, open it with
     `wt switch --no-cd --format=json -y journal/<login>`. Worktrunk creates the
     local tracking branch when it exists only on the fetched remote.
   - Apply the same scoped setup ownership in that worktree.
5. **Create the journal branch if needed:**
   - If neither a worktree nor `origin/journal/<login>` exists, create one with `wt switch --create --base origin/<default-branch> --no-cd --format=json journal/<login>`.
   - If the current checkout has an ignored local `.journal/`, copy its contents into the new journal worktree without overwriting existing files, for example with `rsync -a --ignore-existing .journal/ <journal-root>/.journal/`.
6. **Bootstrap and publish setup state:**
   - In the journal worktree, ensure `.journal/` exists.
   - Bootstrap any missing `.journal/INDEX.md`, `.journal/SKILLS.md`, and `.journal/TECH_NOTES.md` from the repo's scaffold files.
   - Define the setup write set as only the scaffold or imported paths created by this setup. Dirty paths outside it are expected concurrent work and must remain untouched.
   - If the setup write set changed, stage it with `git add -f -- <setup-write-set>`, commit only it with `git commit --only -m "docs(journal): initialize journal for <login>" -- <setup-write-set>`, and push with upstream set to `origin/journal/<login>`. On a rejected push, follow the bounded retry rule in `.session.md`.
   - If nothing changed, do not create an empty commit; confirm the existing journal branch is ready.
7. **Confirm and orient the developer.** End with a concise onboarding note in your own words that covers:
   - Sessions preserve agent context across days and teammates.
   - The default branch stays clean; their journal lives on `journal/<login>`.
   - Use sessions for substantial implementation, multi-step research, architecture work, or anything another agent may need to resume.
   - Do not use sessions for quick questions or one-off commands.
   - Start with `new session`, resume with `continue session <id>`, and close with `session close`.
   - Teammate journal branches are read-only context sources, not implementation
     bases or lifecycle targets; only `journal-sync` updates their local
     worktrees.
   - What setup just did, including the journal branch and journal worktree path.

Do not create a new session during setup unless the user explicitly asks for one
after setup is complete.
Do not invoke `journal-sync` during setup unless the user separately and
explicitly asks for journal synchronization.
