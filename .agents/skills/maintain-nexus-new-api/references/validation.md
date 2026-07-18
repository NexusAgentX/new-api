# Nexus Validation

Read this reference before validating a Nexus change or an upstream rebase.

## Select Checks By Changed Surface

Inspect both the upstream delta and the Nexus patch stack. A conflict-free
rebase is not proof that behavior stayed identical.

For backend changes, prefer focused package tests first when a small surface
changed:

```bash
go test ./<package>/...
```

Before pushing a patch-stack rebase or a release candidate, run the backend
baseline:

```bash
go test ./...
```

For frontend changes, build the affected theme. The fork CI builds both:

```bash
cd web
bun install --frozen-lockfile
cd default
DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$(cat ../../VERSION)" \
  bun run build

cd ..
bun install --filter ./classic --frozen-lockfile
cd classic
VITE_REACT_APP_VERSION="$(cat ../../VERSION)" bun run build
```

For Dockerfile or workflow changes, validate the YAML shape and inspect the
planned image tags. Do not push a `nexus-v*` tag merely to test the workflow;
ask the user before starting external publishing.

## Workflow Evidence

The `Nexus validation` workflow should pass on `nexus` after infrastructure
changes. The `Nexus upstream check` workflow is expected to fail when a newer
official release exists; treat that as a notification, not as a code failure.
The `Nexus Docker image` workflow is release-scoped and should run only for an
authorized `nexus-v*` tag.

## Isolate Suspected Upstream Issues

If a test fails after an upstream rebase, compare the relevant code across the
old official base, the new official base, and the Nexus patch stack. Only
classify it as an upstream issue after confirming that Nexus did not change the
affected path. Report any excluded or flaky test explicitly.

## Final Hygiene

Before pushing a rebased branch or release tag, require:

```bash
git diff --check
git status --short --branch
```

Require a clean worktree, local `nexus` at the intended commit, and local and
remote branch state understood before using `--force-with-lease`.
