# Feature: gui-split

Objective: move `gui/admin` (the Angular SPA) out of this repository into its own, leaving `api/` and `database/` here, and make parallel agent sessions independent so they stop colliding on a shared HEAD.
Status: **planned, not started.** Blocked on two things only the human can do: git credentials (`gh auth login`) and one green SPA suite run from a terminal without the sandbox pid cap. Nothing here has been executed.

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

## Log

- **L1** (verbatim, human, 2026-11-01): "resuelvan el thema del git que todo este ok y mergueadro cuando tengamos todo estable mergueamos a dev todo clean y borrado,creemos un repositorio para el api la bd y la gui adm asi no tendremos mas problemas ypara el repo principal podriamos unirlo todo por submodulos quizas"
- **L2** (verbatim, human, 2026-11-01, after the architecture trade-offs were presented): "luego que la estabilicen tomo tu propuesta de sacar las gui a un repo y dejar lo demas aca pero que independicen y paralelicen las sessiones para no velver a cohocar todos" — recorded as: split the GUI out, keep `api` + `database`, and make sessions independent; **submodules are dropped** from the original idea.
- **L3** (state, 2026-11-01): consolidation is done and local only — `dev` at `05c4790`, one worktree, no feature branches, everything merged. The API image was rebuilt from `dev` (ledger 02..14, no migrations re-applied) after an incident documented in `05c4790`: the ledger files were mode 600 in this checkout, the image runs as `USER app`, and a blanket `COPY --chmod=644` broke the directory before `RUN chmod -R a+rX` fixed it. Root cause was the creating session's umask, not the repository — Git only versionaizes 644/755, so a clean clone would not have reproduced it.
