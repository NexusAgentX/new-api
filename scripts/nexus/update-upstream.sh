#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/nexus/update-upstream.sh [options]

Rebase the NexusAgentX patch stack onto a stable QuantumNous New API release tag.

Options:
  --branch <name>       Branch to update (default: nexus)
  --target <v-tag>      Release tag to use (default: latest GitHub release)
  --dry-run             Print the planned rebase without changing refs
  -h, --help            Show this help

Environment overrides:
  NEXUS_BRANCH          Same as --branch
  NEXUS_TARGET_TAG      Same as --target
EOF
}

branch="${NEXUS_BRANCH:-nexus}"
target_tag="${NEXUS_TARGET_TAG:-}"
dry_run="false"
tag_re='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'

github_slug() {
  local url="$1"
  url="${url%.git}"

  case "$url" in
    https://github.com/*)
      printf '%s\n' "${url#https://github.com/}"
      ;;
    git@github.com:*)
      printf '%s\n' "${url#git@github.com:}"
      ;;
    ssh://git@github.com/*)
      printf '%s\n' "${url#ssh://git@github.com/}"
      ;;
    *)
      return 1
      ;;
  esac
}

require_github_remote() {
  local remote="$1"
  local expected_slug="$2"
  local url
  local actual_slug
  local normalized_actual_slug
  local normalized_expected_slug

  if ! url="$(git remote get-url "$remote" 2>/dev/null)"; then
    echo "Missing '${remote}' remote; expected ${expected_slug}." >&2
    exit 1
  fi
  if ! actual_slug="$(github_slug "$url")"; then
    echo "Remote '${remote}' must point to ${expected_slug}, got '${url}'." >&2
    exit 1
  fi
  normalized_actual_slug="$(printf '%s' "$actual_slug" | tr '[:upper:]' '[:lower:]')"
  normalized_expected_slug="$(printf '%s' "$expected_slug" | tr '[:upper:]' '[:lower:]')"
  if [[ "$normalized_actual_slug" != "$normalized_expected_slug" ]]; then
    echo "Remote '${remote}' must point to ${expected_slug}, got '${url}'." >&2
    exit 1
  fi
}

latest_local_release_tag() {
  git tag --list 'v[0-9]*' --sort=-version:refname |
    while IFS= read -r tag; do
      if [[ "$tag" =~ $tag_re ]]; then
        printf '%s\n' "$tag"
        break
      fi
    done
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --branch)
      branch="${2:?--branch requires a value}"
      shift 2
      ;;
    --target)
      target_tag="${2:?--target requires a value}"
      shift 2
      ;;
    --dry-run)
      dry_run="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unexpected argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "Run this command from inside the New API repository." >&2
  exit 1
}
cd "$repo_root"

if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo "The worktree must be clean before updating the patch stack." >&2
  exit 1
fi

require_github_remote origin NexusAgentX/new-api
require_github_remote upstream QuantumNous/new-api

if ! git show-ref --verify --quiet "refs/heads/${branch}"; then
  echo "Local branch '${branch}' does not exist." >&2
  exit 1
fi

echo "+ git fetch upstream main and v* tags"
git fetch --no-tags upstream \
  '+refs/heads/main:refs/remotes/upstream/main' \
  '+refs/tags/v*:refs/tags/v*'

old_tag="$(
  git describe --tags --match 'v[0-9]*' --abbrev=0 "$branch" 2>/dev/null || true
)"
if [[ ! "$old_tag" =~ $tag_re ]]; then
  echo "Could not determine the upstream release tag beneath '${branch}'." >&2
  exit 1
fi

if [[ -z "$target_tag" ]]; then
  if command -v gh >/dev/null 2>&1; then
    target_tag="$(gh api repos/QuantumNous/new-api/releases/latest --jq .tag_name)"
  else
    target_tag="$(latest_local_release_tag)"
  fi
fi

if [[ ! "$target_tag" =~ $tag_re ]]; then
  echo "Target '${target_tag}' is not a New API release tag." >&2
  exit 1
fi

if ! git show-ref --verify --quiet "refs/tags/${target_tag}"; then
  echo "Tag '${target_tag}' was not fetched from upstream." >&2
  exit 1
fi

if [[ "$old_tag" == "$target_tag" ]]; then
  echo "${branch} is already based on ${target_tag}."
  exit 0
fi

patch_count="$(git rev-list --count "${old_tag}..${branch}")"
backup_branch="backup/${branch}-before-${target_tag}-$(date -u +%Y%m%dT%H%M%SZ)"

cat <<EOF
Branch:       ${branch}
Current base: ${old_tag}
Target base:  ${target_tag}
Patch count:  ${patch_count}
Backup ref:   ${backup_branch}
EOF

if [[ "$dry_run" == "true" ]]; then
  echo "+ git rebase --onto ${target_tag} ${old_tag} ${branch}"
  exit 0
fi

git branch "$backup_branch" "$branch"
git switch "$branch"

if ! git rebase --onto "$target_tag" "$old_tag" "$branch"; then
  cat >&2 <<EOF
The rebase stopped on a conflict. Resolve it, then run:
  git add <resolved-files>
  git rebase --continue

To return to the pre-update state, run:
  git rebase --abort
  git reset --hard ${backup_branch}
EOF
  exit 1
fi

cat <<EOF
Updated ${branch} from ${old_tag} to ${target_tag}.

Next steps:
  1. Run focused backend and frontend validation for the changed surface.
  2. Review: git range-diff ${old_tag}..${backup_branch} ${target_tag}..${branch}
  3. Publish: git push --force-with-lease origin ${branch}
EOF
