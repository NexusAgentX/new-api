# Nexus Release Runbook

Read this reference before preparing, triggering, retrying, or verifying a
Nexus Docker image release.

## Preflight

1. Confirm the user explicitly authorized a release tag or workflow dispatch.
2. Confirm the worktree is clean and `nexus` points at the intended commit.
3. Confirm the base tag:

   ```bash
   git describe --tags --match 'v[0-9]*' --abbrev=0 nexus
   ```

4. Confirm validation has passed for the commit.
5. Confirm the GHCR package is publicly pullable when the release is intended
   for anonymous deployment. GitHub Packages defaults new packages to private,
   and visibility changes require the package settings web UI. Verify with an
   unauthenticated client after the first publish.
6. List existing Nexus tags and choose the next build number:

   ```bash
   git tag --list 'nexus-v*' --sort=-version:refname
   ```

Use `nexus-<official-release-tag>-nexus.N`. For example, the first build based
on `v1.0.0-rc.21` is `nexus-v1.0.0-rc.21-nexus.1`. Increment `N` for another
Nexus build on the same official base and reset it to `1` after rebasing to a
new official base.

## Tag And Publish

Create an annotated tag at the intended `nexus` commit:

```bash
git tag -a nexus-v1.0.0-rc.21-nexus.1 \
  -m "Nexus New API v1.0.0-rc.21-nexus.1"
git push origin nexus-v1.0.0-rc.21-nexus.1
```

Only `nexus-v*` tags start the tagged `Nexus Docker image` workflow. The
workflow builds Linux amd64 and arm64 images, pushes per-architecture tags, and
creates the multi-arch manifest at:

```text
ghcr.io/nexusagentx/new-api:<nexus-tag>
```

## Verify

1. Watch the workflow to completion.
2. Verify both architecture jobs and the manifest job succeeded.
3. Record the manifest digest and the source commit from the workflow summary.
4. Inspect the manifest before handing it to deployment:

   ```bash
   docker buildx imagetools inspect \
     ghcr.io/nexusagentx/new-api:<nexus-tag>
   ```

5. Confirm the image labels include the Nexus source URL, the intended version,
   and the expected Git revision.

## Deployment Boundary

A published image is only a release artifact. Production deployment is a
separate operations task that must back up PostgreSQL first, pin the manifest
digest, run Compose validation, wait for New API/PostgreSQL/Redis health, and
verify the two public entrances and representative API calls. Do not describe
an image build as a completed deployment.

## Recovery

If a tag was pushed by mistake, stop and report before deleting it. Deleting a
published tag or GHCR image is an external state change and requires explicit
user authorization. If a workflow fails, preserve the run URL and failed job
logs, fix the cause on a normal commit, and use a new build number rather than
reusing a tag.
