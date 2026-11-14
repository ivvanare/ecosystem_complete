# Archive Report: base-domain-model

## Change Summary

**What was done**: One PostgreSQL database (`melia`) with two schemas (`seg` for identity and
security, `core` for the catalogue and the product), replacing the two-database design, expressed
as ordered init SQL instead of framework migrations.
**Why**: the diagram the product is written against names a single database, and two appliers of
the same DDL was the failure mode to avoid.
**How**: ledger files under `database/init/`, applied by the service at startup and recorded in
`seg.tb_schema_migration`; the canonical DBML lives in `database/diagram_base.txt`.

## Delivered

- `database/init/02`…`14`: schemas, enums, the 18 base tables, foreign keys, the six
  point-in-time history tables and their triggers, the UTC convention, locale catalogs and
  timezone handling.
- `database/verify_base_model.sql`: runnable assertions, every one inside a transaction that
  rolls back, so the check never writes.
- `database/diagram_base.txt` as the canonical model, with locale/timezone columns and the
  history set documented in it.

## Verification

`ALL CHECKS PASSED` against the live database, last re-run on 2026-11-14 after later slices
extended the file. The drift check is the same file the later changes appended their own sections
to — it never stopped covering the base model, it grew.

## Notes

This change is the origin of the file, not its boundary: `social-discovery` and the phases after
it appended their own sections (see their archive reports). The living specification today is
`database/diagram_base.txt` for the model, `api/openapi.yaml` for the HTTP contract and
`api/API.md` for the flows.
