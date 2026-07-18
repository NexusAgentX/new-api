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

Then replay only the NexusAgentX commits onto the selected release tag:

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

The `Nexus validation` workflow runs the Go backend tests and builds both the
default and classic frontends for pushes and pull requests targeting `nexus`.
The backend embeds both frontend `dist` directories, so build them before
running the full Go test suite. Run the same checks locally before pushing
when practical:

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

cd ../..
go test ./...
```

## Nexus Docker images

Nexus images are published to GHCR as `ghcr.io/nexusagentx/new-api`. The
`Nexus Docker image` workflow builds Linux amd64 and arm64 variants and then
creates a multi-arch manifest.

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
