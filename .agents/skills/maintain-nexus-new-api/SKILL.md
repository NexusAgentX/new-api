---
name: maintain-nexus-new-api
description: Maintain the NexusAgentX New API fork and preserve its fork-specific behavior. Use whenever working on the fork's nexus branch or Nexus-owned code, including official release-tag rebases, patch-stack review, main mirror updates, validation, GHCR image builds, release tags, deployment handoff, and fork maintenance diagnosis.
---

# Maintain Nexus New API

## Load The Required Context

1. Read the applicable `AGENTS.md` files for project-wide build, test, style,
   license, and attribution instructions. Keep Nexus operational policy in
   this skill.
2. Read [fork-invariants.md](references/fork-invariants.md) before changing
   Nexus-owned code, reviewing the patch stack, or resolving rebase conflicts.
3. Read [validation.md](references/validation.md) before validating a Nexus
   change or upstream rebase.
4. Read [release-runbook.md](references/release-runbook.md) before preparing,
   triggering, retrying, or verifying a Nexus Docker image release.
5. Inspect the worktree and remotes:

   ```bash
   git status --short --branch
   git remote -v
   ```

6. Require `origin` to point to `NexusAgentX/new-api` and `upstream` to point
   to `QuantumNous/new-api`. Stop and report unexpected remotes rather than
   rewriting them silently.

## Preserve The Branch Model

- Keep `main` free of Nexus changes; it is the official upstream mirror.
- Keep the custom patch stack on `nexus`, the fork's default branch.
- Base `nexus` on an official `v*` release tag, not on a moving `main` commit.
- Keep each custom behavior in a small, reviewable commit with focused tests.
- Never discard unrelated worktree changes or rewrite a shared branch without
  first checking its remote state.

## Update The Nexus Patch Stack

1. Require a clean `nexus` worktree. If user changes are present, stop and ask
   how to preserve them.
2. Preview the latest official update:

   ```bash
   scripts/nexus/update-upstream.sh --dry-run
   ```

3. Treat the scheduled `Nexus upstream check` workflow as a notification. It
   may report drift, but it must never rewrite `nexus` automatically.
4. Use `--target <official-v-tag>` when the user requested a specific release.
5. After an upstream target is approved but before changing the base, repack
   the current Nexus stack against its existing official base:
   - Create a dated backup branch and work in a temporary branch or worktree.
   - Fold follow-up and rebase-compatibility fixes into their owning feature
     commits. Drop only commits whose final behavior is fully cancelled.
   - Keep independent, reviewable product behaviors as separate commits; do
     not squash the entire stack into a broad aggregate patch.
   - Require the repacked tree to match the backup exactly with
     `git diff --exit-code <backup>..<repacked>`, then review the patch-series
     change with `git range-diff <base>..<backup> <base>..<repacked>`.
   - Run the applicable validation from [validation.md](references/validation.md)
     and update `origin/nexus` with `git push --force-with-lease` before
     starting the upstream rebase. Never use an unconditional force push.
6. Run the update only after reviewing the old base, new base, and compacted
   patch count:

   ```bash
   scripts/nexus/update-upstream.sh
   ```

7. Resolve rebase conflicts without dropping the behavior documented in
   [fork-invariants.md](references/fork-invariants.md). Put each direct
   conflict resolution into the feature commit being replayed; if validation
   later identifies a cross-feature compatibility fix, split it into focused
   fixups and fold those into the owning commits before finalizing the stack.
   Use the backup branch and `git range-diff` command printed by the script to
   compare the old and new patch stacks.
8. Follow [validation.md](references/validation.md).
9. Push rewritten history only after validation:

   ```bash
   git push --force-with-lease origin nexus
   ```

   Never use an unconditional force push.

## Maintain The Main Mirror

Update `main` separately from the Nexus patch stack. Verify that the update is
fast-forward before pushing `upstream/main` to `origin/main`. Never merge
`nexus` into `main`.

## Change Nexus Behavior

1. Work on `nexus` or a short-lived branch based on it.
2. Keep generic fixes suitable for QuantumNous upstream separable from
   Nexus-specific policy or product changes.
3. Avoid broad formatting, generated-file, or dependency churn unless the
   change requires it.
4. Preserve upstream project identity, copyright, notices, and attribution.
5. Report which commits are Nexus-only and which could be submitted upstream.

## Prepare And Publish A Nexus Image

1. Follow [release-runbook.md](references/release-runbook.md) from preflight
   through manifest verification.
2. Use tags shaped as `nexus-<official-release-tag>-nexus.N`, where `N` is a
   positive, monotonically increasing build number for that upstream release.
3. Treat workflow dispatch and tag pushes as external state changes. Do not
   dispatch a build or push a release tag unless the user explicitly
   authorized that action.
4. Publish to `ghcr.io/nexusagentx/new-api`; do not reuse the official
   `calciumion/new-api` Docker Hub repository.
5. Record the multi-arch manifest digest and exact source commit before any
   deployment handoff. Production changes require the separate backup-first
   New API deployment procedure.

## Report Results

State the old and new upstream bases, patch count, commit SHA, validations run,
remote mutations performed, workflow URL, image digest when applicable, and
any remaining deployment or rollback gaps. Distinguish a prepared image from a
deployed production change.
