-- ============================================================================
-- DROP LEGACY PLACE CATEGORY — core.tb_place.tx_category leaves the model.
-- Spec: database/diagram_base.txt (ESQUEMA core, tb_place).
--
-- WHY NOW
--   10-locale-catalogs.sql created the governed catalog and 11-locale-columns.sql
--   wired core.tb_place.co_place_category to it. Since then the entity carries TWO
--   columns that claim to be the category: the legacy free-text tx_category and the
--   catalog FK. The history payload already kept only the governed one, so history
--   no longer mirrors the entity: an UPDATE that touches only tx_category differs
--   from OLD in the trigger's no-op comparison (to_jsonb(NEW) - 'dt_updated_at'),
--   and records an interval whose payload is identical to the previous one — a
--   "change" that shows no change.
--   core.tb_place is EMPTY, so there is nothing to migrate and the column goes
--   without a backfill. And keeping free text next to the catalog would reintroduce
--   exactly the string-grouping problem the catalog exists to end: 'Hotel',
--   'hotel' and 'Hotel ' group as three different categories.
--
-- TRIGGER FUNCTIONS
--   The live core.tg_tb_place_history() is the body 11-locale-columns.sql installed
--   with CREATE OR REPLACE; it references co_place_category, never tx_category
--   (verified against pg_proc), so no function is replaced here.
-- ============================================================================
\connect melia

ALTER TABLE core.tb_place DROP COLUMN tx_category;
