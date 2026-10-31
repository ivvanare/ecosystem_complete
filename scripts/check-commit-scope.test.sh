#!/usr/bin/env bash
# Behavioural test for scripts/check-commit-scope.sh.
#
# Each case builds a throwaway git repository, stages a set of paths, and asserts what the
# guardrail does: the exit code, that a rejection names the offending paths on both sides,
# that it advertises the override, and that every allowed case stays silent so it never
# adds noise to a normal commit.
#
# Run: bash scripts/check-commit-scope.test.sh
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/check-commit-scope.sh"
wrapper="$here/pre-commit"

failures=0
passes=0
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

pass() { passes=$((passes + 1)); printf 'ok   %s\n' "$1"; }
fail() { failures=$((failures + 1)); printf 'FAIL %s\n     %s\n' "$1" "$2"; }

new_repo() {
  local dir="$tmp_root/$1"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.com
  git -C "$dir" config user.name test
  printf 'seed\n' >"$dir/README.md"
  git -C "$dir" add README.md
  git -C "$dir" commit -qm init
  printf '%s\n' "$dir"
}

stage() {
  local dir="$1"
  shift
  local path
  for path in "$@"; do
    mkdir -p "$dir/$(dirname "$path")"
    printf 'content\n' >"$dir/$path"
  done
  git -C "$dir" add -- "$@"
}

# run <dir> [env assignments...] -> sets out and rc, runs the guardrail from the repo root
run() {
  local dir="$1"
  shift
  out="$(cd "$dir" && env "$@" bash "$script" 2>&1)"
  rc=$?
}

expect_allowed() {
  local name="$1" dir="$2"
  shift 2
  run "$dir" "$@"
  if [ "$rc" -ne 0 ]; then
    fail "$name" "expected exit 0, got $rc. output: $out"
  elif [ -n "$out" ]; then
    fail "$name" "expected silence on an allowed commit, got: $out"
  else
    pass "$name"
  fi
}

contains() {
  printf '%s\n' "$out" | grep -qF -- "$1"
}

expect_rejected() {
  local name="$1" dir="$2" want_a="$3" want_b="$4"
  run "$dir"
  if [ "$rc" -ne 1 ]; then
    fail "$name" "expected exit 1, got $rc. output: $out"
    return
  fi
  if ! contains "$want_a" || ! contains "$want_b"; then
    fail "$name" "message does not name both offending paths ($want_a, $want_b): $out"
  elif ! contains "MELIA_ALLOW_MIXED=1"; then
    fail "$name" "message does not advertise the override: $out"
  else
    pass "$name"
  fi
}

# --- a commit that spans both sides is refused -------------------------------------
dir="$(new_repo mixed)"
stage "$dir" api/internal/auth.go gui/admin/src/app/app.ts
expect_rejected "mixed backend+frontend is rejected" "$dir" "api/internal/auth.go" "gui/admin/src/app/app.ts"

# every backend root counts, not just api/
dir="$(new_repo mixed-database)"
stage "$dir" database/init/05-users.sql gui/admin/src/app/app.ts
expect_rejected "database+frontend is rejected" "$dir" "database/init/05-users.sql" "gui/admin/src/app/app.ts"

dir="$(new_repo mixed-tests)"
stage "$dir" tests/k6/signup.js gui/admin/src/app/app.ts
expect_rejected "tests+frontend is rejected" "$dir" "tests/k6/signup.js" "gui/admin/src/app/app.ts"

# a rename out of one side into the other must not look like a single-side change
dir="$(new_repo mixed-rename)"
stage "$dir" gui/admin/src/moved.go
git -C "$dir" commit -qm "add frontend file"
mkdir -p "$dir/api"
git -C "$dir" mv gui/admin/src/moved.go api/moved.go
expect_rejected "a rename across sides is rejected" "$dir" "gui/admin/src/moved.go" "api/moved.go"

# removing a frontend file while touching the backend is still a mixed commit
dir="$(new_repo mixed-delete)"
stage "$dir" gui/admin/src/dead.ts
git -C "$dir" commit -qm "add dead file"
stage "$dir" api/internal/auth.go
git -C "$dir" rm -q gui/admin/src/dead.ts
expect_rejected "a frontend deletion with a backend change is rejected" "$dir" "gui/admin/src/dead.ts" "api/internal/auth.go"

# --- single-side and neutral commits pass silently ---------------------------------
dir="$(new_repo backend-only)"
stage "$dir" api/internal/auth.go database/init/05-users.sql tests/k6/signup.js
expect_allowed "backend-only commit passes" "$dir"

dir="$(new_repo frontend-only)"
stage "$dir" gui/admin/src/app/app.ts gui/admin/projects/crud-maker/README.md
expect_allowed "frontend-only commit passes" "$dir"

dir="$(new_repo neutral-only)"
stage "$dir" odd/tasks/crud-maker.md openspec/specs/flow-tutorial/spec.md .github/workflows/ci.yml
expect_allowed "neutral paths pass" "$dir"

dir="$(new_repo backend-neutral)"
stage "$dir" api/internal/auth.go odd/tasks/crud-maker.md docker-compose.yml
expect_allowed "backend plus neutral paths pass" "$dir"

dir="$(new_repo nothing-staged)"
expect_allowed "an empty index passes" "$dir"

# --- the documented escape hatch ---------------------------------------------------
dir="$(new_repo override)"
stage "$dir" api/internal/auth.go gui/admin/src/app/app.ts
out="$(cd "$dir" && env MELIA_ALLOW_MIXED=1 bash "$script" 2>&1)"
rc=$?
if [ "$rc" -ne 0 ]; then
  fail "MELIA_ALLOW_MIXED=1 allows a mixed commit" "expected exit 0, got $rc. output: $out"
elif ! printf '%s\n' "$out" | grep -qF "api/internal/auth.go"; then
  fail "MELIA_ALLOW_MIXED=1 allows a mixed commit" "allowed but silent about which sides were mixed: $out"
else
  pass "MELIA_ALLOW_MIXED=1 allows a mixed commit"
fi

# --- git actually invokes it, and a refused commit leaves no commit behind -------------
install_hook() {
  local dir="$1"
  mkdir -p "$dir/scripts"
  cp "$script" "$dir/scripts/check-commit-scope.sh"
  cp "$wrapper" "$dir/.git/hooks/pre-commit"
  chmod +x "$dir/.git/hooks/pre-commit"
}

dir="$(new_repo hook-integration)"
install_hook "$dir"
before="$(git -C "$dir" rev-parse HEAD)"
stage "$dir" api/internal/auth.go gui/admin/src/app/app.ts
out="$(cd "$dir" && git commit -m mixed 2>&1)"
rc=$?
after="$(git -C "$dir" rev-parse HEAD)"
if [ "$rc" -eq 0 ]; then
  fail "git commit refuses a mixed index" "commit succeeded (exit 0): $out"
elif [ "$before" != "$after" ]; then
  fail "git commit refuses a mixed index" "the commit was created despite the failure: $out"
elif ! printf '%s\n' "$out" | grep -qF "refusing a commit"; then
  fail "git commit refuses a mixed index" "the hook's message never reached the user: $out"
else
  pass "git commit refuses a mixed index"
fi

git -C "$dir" restore --staged gui/
out="$(cd "$dir" && git commit -m 'backend only' 2>&1)"
rc=$?
after="$(git -C "$dir" rev-parse HEAD)"
if [ "$rc" -ne 0 ] || [ "$before" = "$after" ]; then
  fail "the same index commits once the frontend is unstaged" "exit $rc. output: $out"
else
  pass "the same index commits once the frontend is unstaged"
fi

# a worktree on a branch that predates the check must still be able to commit
dir="$(new_repo hook-missing-script)"
mkdir -p "$dir/.git/hooks"
cp "$wrapper" "$dir/.git/hooks/pre-commit"
chmod +x "$dir/.git/hooks/pre-commit"
stage "$dir" api/internal/auth.go gui/admin/src/app/app.ts
out="$(cd "$dir" && git commit -m mixed 2>&1)"
rc=$?
if [ "$rc" -ne 0 ]; then
  fail "a checkout without the check still commits" "exit $rc. output: $out"
else
  pass "a checkout without the check still commits"
fi

printf '\n%s passed, %s failed\n' "$passes" "$failures"
[ "$failures" -eq 0 ]