-- ============================================================================
-- 14 — relax the IANA timezone shape check on core.tb_place.
-- Feature: Phase 0 locale hardening · Follow-up to 11-locale-columns.sql
--
-- WHAT WAS WRONG
--   11 shipped CHECK (tx_timezone ~ '^[A-Za-z]+/[A-Za-z_]+$'), which accepts only
--   ONE slash. A large share of real IANA zone names carry two:
--     America/Argentina/Buenos_Aires, America/Indiana/Indianapolis,
--     America/North_Dakota/Center, America/Argentina/ComodRivadavia
--   The live database has 26 such names. Consequence: a place in Argentina could
--   not be created at all — the insert died on ck_tb_place_timezone. Expanding to
--   a second country was blocked by a validation that was supposed to help.
--   The same pattern also rejected the bare IANA names that are valid on their own,
--   such as UTC and GMT.
--
-- WHAT THIS DOES
--   Accepts one to three slash-separated components, which is what IANA actually
--   uses, and keeps rejecting anything else. The constraint stays a SHAPE guard
--   only: the real validation is membership in pg_timezone_names, which a CHECK
--   cannot express because it would need a query, so it belongs at the application
--   edge.
--
-- WHY A NEW FILE INSTEAD OF EDITING 11
--   11 is applied and recorded in seg.tb_schema_migration. Editing an applied
--   migration would make a fresh database and an existing one diverge.
-- ============================================================================
\connect melia

ALTER TABLE core.tb_place DROP CONSTRAINT ck_tb_place_timezone;

ALTER TABLE core.tb_place
    ADD CONSTRAINT ck_tb_place_timezone
    CHECK (tx_timezone ~ '^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$');
