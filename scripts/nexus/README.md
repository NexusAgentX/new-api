# NexusAgentX New API maintenance

This directory contains the maintenance tooling for the unofficial
`NexusAgentX/new-api` fork.

Coding agents should use the repository skill at
`.agents/skills/maintain-nexus-new-api/`. Its references contain the operational
invariants, validation guidance, release checklist, and failure recovery rules.
This file is the human-facing overview.

## Branches

- `main` mirrors `QuantumNous/new-api` and must not contain NexusAgentX changes.
- `nexus` is the fork's default branch. It starts at an official `v*` release
  tag and contains the NexusAgentX patch stack as small, reviewable commits.

The initial Nexus base is `v1.0.0-rc.21`, matching the production deployment
that previously used the official `calciumion/new-api` image.

## Update the upstream base

Preview the latest official release before changing any refs:

```bash
scripts/nexus/update-upstream.sh --dry-run
```

After selecting a target release but before replaying it, compact the current
Nexus patch stack against its existing official base. Work from a dated backup
branch in a temporary worktree. Fold follow-up fixes and cancelled experiments
into their owning feature commits, while keeping independent product behavior
as separate commits. Prove that the final tree did not change and review the
series rewrite before publishing it:

```bash
git diff --exit-code <backup-branch>..<repacked-branch>
git range-diff <current-base>..<backup-branch> \
  <current-base>..<repacked-branch>
git push --force-with-lease origin nexus
```

Only after the compacted stack is validated and published, replay the
NexusAgentX commits onto the selected release tag:

```bash
scripts/nexus/update-upstream.sh
```

Pass `--target vX.Y.Z...` to select a specific official release. The script
creates a backup branch before it changes `nexus` and prints a `range-diff`
command for reviewing the result.

The `Nexus upstream check` workflow compares this base with the latest official
GitHub Release every Monday at 03:23 UTC. It fails with a preview command in
the job summary when an update is available; it never rewrites the branch.
The workflow can also be run manually from GitHub Actions.

## Validation

The `Nexus validation` workflow independently vets, builds, and tests the root
and RelayKit modules, then typechecks, tests, and builds the frontend for pushes
and pull requests targeting `nexus`. The backend embeds the frontend `dist`
directory, so build it before validating the root module. Run the same checks
locally before pushing when practical:

```bash
cd web
bun install --frozen-lockfile
bun run typecheck
bun test
DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$(cat ../VERSION)" \
  bun run build

cd ..
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go test ./...

cd relaykit
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go test ./...
```

## Nexus Docker images

Nexus images are published to GHCR as `ghcr.io/nexusagentx/new-api`. The
`Nexus Docker image` workflow builds Linux amd64 and arm64 variants and then
creates a multi-arch manifest.

The GHCR package is public and allows anonymous pulls. GitHub Packages defaults
new packages to private, and package visibility can only be changed in the
GitHub web UI; after a registry or organization reset, verify anonymous access
before handing an image to deployment.

The first validated Nexus image is:

```text
ghcr.io/nexusagentx/new-api:nexus-v1.0.0-rc.21-nexus.1
source: a33728f0ec4f5741a5d5d92e66ba3d04558e9d6b
manifest: sha256:6e80325f1373c602e6c376b798d70b3da382a917f429b819f832b939393af25c
```

The current production-validated Nexus image is:

```text
ghcr.io/nexusagentx/new-api:nexus-v1.0.0-rc.21-nexus.2
source: bf65e83f910a095567b345e7605fc47ed9f3521b
manifest: sha256:5a99b136592cab5de8ffc9cdf5ae25c9c60c2b8c63822cf6a0684dc8b0297afa
```

Use annotated tags shaped like this:

```text
nexus-<official-release-tag>-nexus.<build>
```

For example, the first Nexus build based on `v1.0.0-rc.21` is:

```bash
git tag -a nexus-v1.0.0-rc.21-nexus.1 \
  -m "Nexus New API v1.0.0-rc.21-nexus.1"
git push origin nexus-v1.0.0-rc.21-nexus.1
```

Only a pushed `nexus-v*` tag starts the tagged Docker workflow. Manual workflow
runs can rebuild an existing `nexus-v*` tag. Record the resulting multi-arch
manifest digest before changing any deployment, and keep production updates on
the separately documented backup-first path.

## Production deployment

The `Nexus production deploy` workflow runs after a successful `Nexus Docker
image` workflow, or manually for an existing `nexus-v*` tag. It resolves the
exact multi-arch digest, then waits for approval in the `production-new-api`
GitHub Environment before using SSH.

The production host accepts only a restricted, forced-command deploy key. That
key runs `/usr/local/sbin/new-api-github-deploy`, which pulls the requested
image, creates a fresh backup, updates `/opt/new-api/compose.yaml`, validates
the Compose file, waits for all six services to become healthy, checks the
source and configured public endpoint, and restores the previous Compose file
on failure.

The `production-new-api` Environment selects the active host through
`PRODUCTION_HOST`, `PRODUCTION_SSH_USER`, `PRODUCTION_SSH_HOST_KEY`, and
`PRODUCTION_SSH_PRIVATE_KEY`. During a host migration, update those secrets
only after the target has passed its restore and origin checks. Environment
approval remains required regardless of which host is active.

The workflow deploys only the exact `tag@digest` resolved from GHCR. It does
not follow a moving branch or `latest` tag.

## Inherited upstream workflows

The inherited Docker Hub, GitHub Release, Electron, and PR triage workflows are
guarded with `github.repository == 'QuantumNous/new-api'` on the `nexus`
branch. They remain visible for upstream-diff purposes but must not publish
from the Nexus fork. Nexus-owned workflows use the inverse guard
`github.repository == 'NexusAgentX/new-api'`.

## Attribution and license

This fork preserves the New API and QuantumNous project identity, copyright,
license, notices, and upstream documentation. Nexus changes are published in
this repository, and Docker image labels point back to this source repository
so the corresponding source remains available under the upstream AGPL license.
