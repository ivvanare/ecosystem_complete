#!/usr/bin/env bash
# Reject a commit that stages both the backend and the frontend.
#
# This repository holds one Go service (api/) and one Angular SPA (gui/). They are separate
# sides with separate gates: a commit that touches both is almost always two changes that
# belong in two reviewable commits, and untangling it afterwards costs a reviewer more than
# splitting it now.
#
# The check reads the index, never the working tree, so it judges exactly what the commit
# will contain while leaving unrelated dirty files alone.
#
# Escape hatch, for the rare change that genuinely belongs to both sides:
#   MELIA_ALLOW_MIXED=1 git commit ...
set -uo pipefail

# Backend paths: the service, its schema, and the load tests that exercise its contract.
backend_re='^(api|database|tests)/'
# Frontend path: the admin SPA and its local library.
frontend_re='^gui/'

# Paths outside both groups (odd/, openspec/, .github/, scripts/, root docs, compose file)
# belong to whichever side needs them and never decide the verdict.
backend=()
frontend=()
while IFS= read -r path; do
  [ -n "$path" ] || continue
  # --no-renames keeps both ends of a move visible, so a file moved out of gui/ into api/
  # reports the side it left as well as the side it joined.
  if [[ $path =~ $backend_re ]]; then
    backend+=("$path")
  elif [[ $path =~ $frontend_re ]]; then
    frontend+=("$path")
  fi
done < <(git diff --cached --name-only --no-renames)

if [ "${#backend[@]}" -eq 0 ] || [ "${#frontend[@]}" -eq 0 ]; then
  exit 0
fi

list_side() {
  local label="$1"
  shift
  local shown=0 path
  printf '  %s:\n' "$label"
  for path in "$@"; do
    if [ "$shown" -lt 10 ]; then
      printf '    %s\n' "$path"
    fi
    shown=$((shown + 1))
  done
  if [ "$shown" -gt 10 ]; then
    printf '    ... and %s more\n' "$((shown - 10))"
  fi
}

if [ -n "${MELIA_ALLOW_MIXED:-}" ]; then
  {
    printf 'melia: this commit stages both sides; allowed because MELIA_ALLOW_MIXED is set.\n'
    list_side "backend (api/ database/ tests/)" "${backend[@]}"
    list_side "frontend (gui/)" "${frontend[@]}"
  } >&2
  exit 0
fi

{
  printf 'melia: refusing a commit that stages both the backend and the frontend.\n\n'
  list_side "backend (api/ database/ tests/)" "${backend[@]}"
  list_side "frontend (gui/)" "${frontend[@]}"
  cat <<'HINT'

Split it into one commit per side, staging by path:

  git restore --staged gui/
  git commit -m "feat(api): ..." -- api database tests
  git add gui/
  git commit -m "feat(admin): ..." -- gui
  git add odd/ openspec/   # the feature notes go with whichever side they describe

If this change genuinely belongs to both sides:

  MELIA_ALLOW_MIXED=1 git commit ...
HINT
} >&2
exit 1
