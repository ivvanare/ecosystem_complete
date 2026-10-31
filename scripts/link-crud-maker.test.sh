#!/usr/bin/env bash
# Behavioural test for scripts/link-crud-maker.sh: it must create the path the panel's
# `link:` dependency resolves to, reuse the main clone's sibling by default, stay quiet and
# idempotent when there is nothing to do, and refuse to point at a package that is not built.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/link-crud-maker.sh"

failures=0
passes=0
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

pass() { passes=$((passes + 1)); printf 'ok   %s\n' "$1"; }
fail() { failures=$((failures + 1)); printf 'FAIL %s\n     %s\n' "$1" "$2"; }

# Geometry matters: the panel's `link:` resolves to <worktree-root>/../crud-maker, so the
# worktree must live somewhere other than beside the main clone for this to exercise the
# real case. That is exactly the layout dev broke.
setup() {
  local name="$1"
  local base="$tmp_root/$name"
  mkdir -p "$base/crud-maker/dist/crud-maker" "$base/melia" "$base/elsewhere"
  printf '{"name":"crud-maker"}\n' >"$base/crud-maker/dist/crud-maker/package.json"
  git -C "$base/melia" init -q
  git -C "$base/melia" config user.email t@t.t
  git -C "$base/melia" config user.name t
  printf 'seed\n' >"$base/melia/README.md"
  git -C "$base/melia" add README.md
  git -C "$base/melia" commit -qm init
  git -C "$base/melia" worktree add -q "$base/elsewhere/wt" -b wt 2>/dev/null
  printf '%s\n' "$base"
}

base="$(setup default-source)"
out="$(cd "$base/elsewhere/wt" && bash "$script" 2>&1)"; rc=$?
if [ "$rc" -ne 0 ]; then
  fail "creates the path the dependency expects" "exit $rc: $out"
elif [ ! -L "$base/elsewhere/crud-maker" ]; then
  fail "creates the path the dependency expects" "no symlink at the expected sibling: $out"
elif [ "$(readlink -f "$base/elsewhere/crud-maker")" != "$(readlink -f "$base/crud-maker")" ]; then
  fail "creates the path the dependency expects" "wrong target: $(readlink -f "$base/elsewhere/crud-maker")"
else
  pass "creates the path the dependency expects"
fi

out2="$(cd "$base/elsewhere/wt" && bash "$script" 2>&1)"; rc2=$?
if [ "$rc2" -ne 0 ] || [ "$(readlink -f "$base/elsewhere/crud-maker")" != "$(readlink -f "$base/crud-maker")" ]; then
  fail "is idempotent" "exit $rc2: $out2"
else
  pass "is idempotent"
fi

base="$(setup explicit-source)"
other="$base/elsewhere/crud-maker"
mkdir -p "$other/dist/crud-maker" && printf '{}\n' >"$other/dist/crud-maker/package.json"
out="$(cd "$base/elsewhere/wt" && CRUD_MAKER_SRC="$other" bash "$script" 2>&1)"; rc=$?
if [ "$rc" -ne 0 ] || [ "$(readlink -f "$base/elsewhere/crud-maker")" != "$(readlink -f "$other")" ]; then
  fail "honours CRUD_MAKER_SRC" "exit $rc: $out"
else
  pass "honours CRUD_MAKER_SRC"
fi

base="$(setup unbuilt-package)"
rm -rf "$base/crud-maker/dist"
out="$(cd "$base/elsewhere/wt" && bash "$script" 2>&1)"; rc=$?
if [ "$rc" -eq 0 ]; then
  fail "refuses a package that is not built" "expected failure, got exit 0: $out"
elif [ -L "$base/elsewhere/crud-maker" ]; then
  fail "refuses a package that is not built" "left something behind: $(ls -ld "$base/elsewhere/crud-maker" 2>&1)"
else
  pass "refuses a package that is not built"
fi

printf '\n%s passed, %s failed\n' "$passes" "$failures"
[ "$failures" -eq 0 ]
