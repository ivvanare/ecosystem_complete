#!/usr/bin/env bash
# Point the panel's `link:` dependency at the crud-maker checkout it expects.
#
# gui/admin declares `crud-maker: link:../../../crud-maker/dist/crud-maker`, a path resolved
# relative to its own directory: <repo-root>/../crud-maker. That is right for a clone sitting
# in projects/melia and wrong for every git worktree, whose root lives somewhere else, so a
# worktree cannot install the panel until the expected path exists.
#
# Run once per worktree, before `pnpm install`.
#
#   scripts/link-crud-maker.sh
#   CRUD_MAKER_SRC=/somewhere/crud-maker scripts/link-crud-maker.sh
#
# The source defaults to the sibling of the MAIN clone, so a worktree reuses the package
# checkout that already exists instead of needing its own.
set -euo pipefail

common_dir="$(git rev-parse --path-format=absolute --git-common-dir)"
main_root="$(dirname "$common_dir")"
src="${CRUD_MAKER_SRC:-$main_root/../crud-maker}"
dest="$(git rev-parse --show-toplevel)/../crud-maker"

if [ ! -d "$src/dist/crud-maker" ]; then
  printf 'link-crud-maker: %s has no dist/crud-maker — build the package first.\n' "$src" >&2
  exit 1
fi

src="$(cd "$src" && pwd -P)"
dest="$(cd "$dest/.." 2>/dev/null && pwd -P)/crud-maker" || dest="$(git rev-parse --show-toplevel)/../crud-maker"

if [ -d "$dest" ] && [ "$src" = "$(cd "$dest" && pwd -P)" ]; then
  echo 'link-crud-maker: nothing to do, the expected path is already the package itself.'
  exit 0
fi

ln -sfn "$src" "$dest"
printf 'link-crud-maker: %s -> %s\n' "$dest" "$src"
