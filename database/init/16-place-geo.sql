-- ============================================================================
-- 16 — PLACE GEO — core.tb_place.co_location, derived from the decimals.
-- Spec: database/diagram_base.txt · Change: openspec/changes/social-discovery
-- (Phase 0, D2/S16) · Feature: odd/tasks/geo-foundation.md
--
-- THE DECIMALS STAY THE CONTRACT
--   nu_latitude/nu_longitude remain NOT NULL decimal(9,6) with their shape
--   CHECK; every spatial representation is DERIVED from them and can be
--   dropped and rebuilt without data loss (D2). This column is the first of
--   those representations, and nothing writes it by hand.
--
-- GENERATED, NOT A TRIGGER
--   ST_SetSRID(ST_MakePoint(nu_longitude, nu_latitude), 4326)::geography is
--   immutable on PostGIS 3.5 / PostgreSQL 15 — verified on a throwaway
--   database before this file was written: the ALTER was accepted, and the
--   value followed an UPDATE of the decimals. So the column is GENERATED
--   ALWAYS ... STORED and no trigger machinery exists. Note the argument
--   order: MakePoint takes LONGITUDE first. geography, not geometry, because
--   the reads this index serves are distance reads in metres (ST_DWithin)
--   and viewport intersection (&&); polygon containment, when it arrives,
--   uses geometry with ST_Contains (S16).
--
-- THE INDEX
--   GiST on the geography column serves both the radius (ST_DWithin) and the
--   viewport (ST_MakeEnvelope / &&) reads.
--
-- WHY NOT IN HISTORY
--   core.tb_place_history does not carry co_location: it is derived from the
--   two decimals the payload already carries, so duplicating it would make an
--   interval claim a change that is not one. A coordinates update already
--   records itself through nu_latitude/nu_longitude.
--
-- NO H3 COLUMN
--   There is deliberately no tx_h3_cell and no h3-pg extension: cells are
--   computed in Go where they are used (D13).
-- ============================================================================
\connect melia

ALTER TABLE core.tb_place
    ADD COLUMN co_location geography(Point,4326)
    GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(nu_longitude, nu_latitude), 4326)::geography) STORED;

CREATE INDEX ix_tb_place_location ON core.tb_place USING gist (co_location);

COMMENT ON COLUMN core.tb_place.co_location IS
    'Geography derived from nu_latitude/nu_longitude (GENERATED ALWAYS STORED, MakePoint(longitude, latitude), SRID 4326). The decimals are the contract; this column is never written by hand. Serves ST_DWithin (radius) and the viewport (&&) via its GiST index. Kept out of tb_place_history on purpose: the payload already carries the decimals it derives from.';
