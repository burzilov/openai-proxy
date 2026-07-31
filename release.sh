#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 vX.Y.Z" >&2
  exit 1
}

[[ $# -eq 1 ]] || usage

release_tag="$1"
if [[ ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Version must use the vX.Y.Z format, for example v0.1.7." >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" ||
  { echo "The script must be run from a Git repository." >&2; exit 1; }
cd "$repo_root"

[[ "$(git rev-parse --abbrev-ref HEAD)" == "main" ]] ||
  { echo "The release must be created from the main branch." >&2; exit 1; }

[[ -z "$(git status --porcelain)" ]] ||
  { echo "Working tree is not clean. Commit or discard the changes." >&2; exit 1; }

git fetch origin --tags

git diff --quiet HEAD origin/main ||
  { echo "Local main differs from origin/main. Synchronize the branch first." >&2; exit 1; }

git rev-parse --verify --quiet "refs/tags/$release_tag" >/dev/null &&
  { echo "Tag $release_tag already exists." >&2; exit 1; }

if git ls-remote --exit-code --tags origin "refs/tags/$release_tag" >/dev/null 2>&1; then
  echo "Tag $release_tag already exists on origin." >&2
  exit 1
fi

version="${release_tag#v}"
version_file="internal/version/version.go"

go test ./...

python3 - "$version_file" "$version" <<'PY'
import re
import sys
from pathlib import Path

path = Path(sys.argv[1])
version = sys.argv[2]
source = path.read_text()
updated, count = re.subn(
    r'const Version = "[^"]+"',
    f'const Version = "{version}"',
    source,
    count=1,
)
if count != 1:
    raise SystemExit(f"Failed to update the version in {path}.")
path.write_text(updated)
PY

git add "$version_file"
git commit -m "Release $release_tag"
git tag -a "$release_tag" -m "Release $release_tag"
git push origin main
git push origin "$release_tag"

echo "Release $release_tag has been published. GitHub Actions has started publishing the Docker image."
