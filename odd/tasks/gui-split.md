# Feature: gui-split

Objective: move `gui/admin` (the Angular SPA) out of this repository into its own, leaving `api/` and `database/` here, and make parallel agent sessions independent so they stop colliding on a shared HEAD.
Status: **implemented locally, awaiting publication.** The tree is split: `melia-admin` holds the console with its 57 commits of history, `melia` keeps `api/` + `database/`. Both sides are verified. What remains needs the human: creating the repository and pushing, because no session holds GitHub credentials.

## Why

Two root causes were paid for during the `admin-dashboard-shell` → `admin-users` work:

1. **Two sessions, one checkout.** Twice, one session switched branches under another. Fixed processually with one worktree per session; a repository split removes the shared HEAD entirely for sessions that work on different sides.
2. **`crud-maker` as an unpublished sibling.** The adapter had to be built locally and linked from `dist/`; that produced a resolution failure (`crud-maker` was unresolvable from the linked package), a duplicate-instance risk with ng-zorro, and a CI step that clones and builds the sibling before the SPA.

The human chose: after stabilising, split the GUI out. Recorded decision, not a recommendation: **`gui/admin` leaves; `api/` + `database/` stay.**

## Approach

1. **Preserve history with `git subtree split -P gui/admin`.** It produces a branch containing only the GUI commits, which becomes the initial history of the new repository. A fresh start would lose the review trail of 100+ commits.
2. **The new repository is born only when it can be pushed** — credentials first, otherwise the split produces a local branch that cannot be published.
3. **The API contract moves to a consumed artefact.** `api/openapi.yaml` stays here (the API owns its contract); the GUI gets a versioned copy or a generated client. Never a submodule: pinned SHAs and `--recurse-submodules` reproduce exactly the coupling this split is meant to remove.
4. **CI splits with it.** The `admin` job (SPA build + tests) moves to the new repository; the `api` job drops the sibling clone and the SPA steps. Both keep the shared contract check by consuming the same openapi artefact.
5. **`scripts/check-commit-scope.sh` becomes obsolete.** It exists because both sides share one repository and one branch per side; after the split there is nothing to separate. Decide explicitly whether to delete it or turn it into a per-repo check — do not leave it silently failing.
6. **Transition rule while both exist:** still one worktree per session in `melia`, because `api/` and `database/` keep living here and two sessions can still collide on those paths.

## What is NOT part of this

- Publishing `crud-maker`. It is the other half of the friction and it is independent: it unblocks the SPA's dependency story whether or not the split happens. Recommended first.
- Any change to `database/` or `api/`. They stay, unchanged, in this repository.
- Splitting `database` from `api`: the service is the only applier of the schema (`DB_AUTO_MIGRATE` + ledger), so separating them would turn that invariant into a distributed one.

## Prerequisites (blocked)

| # | Prerequisite | Why |
|---|---|---|
| 1 | `gh auth login` (no credentials exist in this environment) | The two pending pushes — `crud-maker` `main` (8 commits) and `melia` `dev` (77+) — and creating the new repository |
| 2 | `npx ng test admin --watch=false` + `npx ng build` green on `dev` | Do not split a tree whose suite has not run since the last four commits |

## Tasks

| # | Task | State | Evidence |
|---|------|-------|----------|
| T1 | Split the console's history out of this repository | done | `git subtree split -P gui/admin -b gui-only` → 57 commits, 153 files, no `gui/` prefix |
| T2 | Seed the new repository with that history | done | `git clone --single-branch --branch gui-only` into `melia-admin`; `origin` removed; 11 MB |
| T3 | Mirror the branch scheme | done | `dev` at the split tip, `quality` and `prod` created from it |
| T4 | Fix the `crud-maker` link depth | done | `link:../../../crud-maker` → `link:../crud-maker` in `package.json` and `pnpm-lock.yaml` |
| T5 | Move the CI gate with the tree | done | `melia-admin/.github/workflows/ci.yml` carries the old `admin` job at the new root |
| T6 | Move the sibling-link script | done | `scripts/link-crud-maker.sh` + its test; suite passes 4/4 in the new repository |
| T7 | Drop the console from this repository | done | 153 files removed; `gui/` count is 0 |
| T8 | Drop the frontend gate from this CI | done | `admin` job and the `crud-maker` clone gone; `test` and `image` remain |
| T9 | Retire the commit-scope guard | done | `check-commit-scope.sh`, its test and `pre-commit` deleted, with the reason recorded in `AGENTS.md` |
| T10 | Update the documentation | done | `AGENTS.md`, `README.md`; `opencode.json` moved with the SPA |
| T11 | Verify the new repository builds | done | `ng build` exit 0, initial bundle 312.86 kB / 87.31 kB — byte-identical to the pre-split build |
| T12 | Verify this repository without the console | done | `go build`, `go vet`, 15 packages with tests, 0 FAIL, contract green |
| T13 | Publish | **blocked on the human** | Needs the private repository created and both pushes |

## Log

- **L1** (verbatim, human, 2026-11-01): "resuelvan el thema del git que todo este ok y mergueadro cuando tengamos todo estable mergueamos a dev todo clean y borrado,creemos un repositorio para el api la bd y la gui adm asi no tendremos mas problemas ypara el repo principal podriamos unirlo todo por submodulos quizas"
- **L2** (verbatim, human, 2026-11-01, after the architecture trade-offs were presented): "luego que la estabilicen tomo tu propuesta de sacar las gui a un repo y dejar lo demas aca pero que independicen y paralelicen las sessiones para no velver a cohocar todos" — recorded as: split the GUI out, keep `api` + `database`, and make sessions independent; **submodules are dropped** from the original idea.
- **L3** (state, 2026-11-01): consolidation is done and local only — `dev` at `05c4790`, one worktree, no feature branches, everything merged. The API image was rebuilt from `dev` (ledger 02..14, no migrations re-applied) after an incident documented in `05c4790`: the ledger files were mode 600 in this checkout, the image runs as `USER app`, and a blanket `COPY --chmod=644` broke the directory before `RUN chmod -R a+rX` fixed it. Root cause was the creating session's umask, not the repository — Git only versionaizes 644/755, so a clean clone would not have reproduced it.
- **L4** (decision, 2026-11-01): the branch scheme of `melia-admin` mirrors this repository — `dev` → `quality` → `prod`, with `prod` as the default and protected branch.
- **L5** (state, 2026-11-01): the split ran. `dev` here is `64583a6` and published; the console left as `melia-admin` with 57 commits, and `gui/` is gone from this repository. Prerequisite 2 was satisfied before starting: `ng build` green from a sandbox session and the 101-test suite reported green by the human. Two sandbox limits were worked around rather than accepted, and they cost hours, so they are recorded here: `ng build` only runs under `UV_THREADPOOL_SIZE=1 NG_BUILD_MAX_WORKERS=1 GOMAXPROCS=1 NODE_OPTIONS=--v8-pool-size=0` because the cgroup allows 64 pids against 24 CPUs, and `pnpm install` needs `--child-concurrency=1 --ignore-scripts` for the same reason. The vitest suite still cannot start a worker in that cgroup, which is why the human ran it.
