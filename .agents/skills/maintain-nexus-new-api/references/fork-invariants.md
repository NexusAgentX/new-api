# Nexus Fork Invariants

Read this reference before changing Nexus-owned behavior, reviewing the patch
stack, or resolving an upstream rebase conflict.

## Repository Identity

- `origin` must be `NexusAgentX/new-api`.
- `upstream` must be `QuantumNous/new-api`.
- `main` is an upstream mirror and contains no Nexus patches.
- `nexus` is the default distribution branch. It is a linear Nexus patch stack
  based on an official `v*` release tag, never a moving `main` commit.
- Discover the current base, patch count, and release tags from Git. Do not
  hardcode a version from this reference.

Use these commands to reconstruct current state:

```bash
git status --short --branch
git remote -v
git describe --tags --match 'v[0-9]*' --abbrev=0 nexus
base="$(git describe --tags --match 'v[0-9]*' --abbrev=0 nexus)"
git rev-list --count "${base}..nexus"
git tag --list 'nexus-v*' --sort=-version:refname
```

## Behavior That Must Survive Rebases

| Area | Invariant | Primary owners |
| --- | --- | --- |
| Branch model | Keep `main` as a pure upstream mirror and keep Nexus changes only on `nexus` or short-lived branches based on it. | repository branch state |
| Upstream updates | Replay the Nexus patch stack onto official `v*` release tags; never merge `nexus` into `main`. | `scripts/nexus/update-upstream.sh` |
| Upstream notification | The scheduled check reports official release drift but never rewrites `nexus`. | `.github/workflows/nexus-upstream-check.yml` |
| Fork validation | Nexus pushes and PRs run Go backend tests and the frontend build only in the Nexus repository. | `.github/workflows/nexus-validation.yml` |
| Image identity | Publish Nexus images to `ghcr.io/nexusagentx/new-api`, never to the official `calciumion/new-api` repository. | `.github/workflows/nexus-docker.yml` |
| Release tags | Use `nexus-<official-release-tag>-nexus.N` and derive the image version from that tag. | `.github/workflows/nexus-docker.yml`, `scripts/nexus/README.md` |
| Inherited publishing | Inherited Docker Hub, Release, Electron, and PR triage jobs stay inert unless running in `QuantumNous/new-api`. | upstream-owned files under `.github/workflows/` |
| Production deploy | Production deployment uses the gated `Nexus production deploy` workflow, the `production-new-api` environment, and a restricted forced-command SSH key. It deploys exact `tag@digest` images and must preserve backup and rollback behavior. | `.github/workflows/nexus-production-deploy.yml`, `/usr/local/sbin/new-api-github-deploy` on the production host |
| Update checker | Nexus builds detect `*-nexus.N` versions and compare them with `NexusAgentX/new-api` `nexus-v*` tags; official builds keep the upstream release check. | `web/default/src/features/system-settings/maintenance/update-checker-section.tsx` |
| Attribution | Preserve New API, QuantumNous, copyright, license, notices, and upstream documentation. Publish corresponding Nexus source publicly. | `README.md`, `LICENSE`, `NOTICE`, `THIRD-PARTY-LICENSES.md` |

## External Deployment Boundary

Building or publishing a Nexus image is not a production deployment. The
production New API instance has its own backup-first update procedure in the
operations repository. Do not change remote Docker Compose files, databases, or
public ingress from this fork unless the user explicitly requested that
separate deployment task.
