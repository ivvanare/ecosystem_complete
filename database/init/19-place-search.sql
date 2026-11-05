-- ============================================================================
-- 19 — PLACE SEARCH — immutable unaccent, generated tsvectors, GIN indexes.
-- Spec: database/diagram_base.txt · Change: openspec/changes/social-discovery
-- (Phase 1, S16/S17/S21) · Feature: odd/tasks/place-discovery.md (commit 3)
--
-- WHY THE EXTENSIONS LIVE HERE
--   Extensions ship in their own ledger file with CREATE EXTENSION IF NOT
--   EXISTS (the rule 15-postgis-extension.sql set). unaccent and pg_trgm are
--   TRUSTED extensions in PostgreSQL 15, so any role with CREATE on the
--   database can install them — no superuser check before release, unlike
--   postgis. Both land in public, the first schema of the service's
--   search_path. pg_trgm is not used by any query yet: it ships now so the
--   typo-tolerant RANKING signal S21 allows is a future query change, not a
--   future migration.
--
-- THE IMMUTABLE WRAPPER — A CORRECTNESS DECISION, NOT SYNTAX
--   unaccent() is STABLE: PostgreSQL reserves the right to change the
--   unaccent dictionary between releases, and a STABLE function cannot
--   appear in a GENERATED ALWAYS AS ... STORED column nor in an index
--   expression. The two-argument form with a FIXED dictionary
--   ('public.unaccent') does not change at runtime, so PostgreSQL's own
--   documentation (F.45, "unaccent and indexes") blesses declaring an SQL
--   wrapper IMMUTABLE. That declaration is ours to keep true: the wrapper
--   must never call anything volatile or stable again, which is why it is a
--   one-line frozen expression, schema-qualified so search_path changes
--   cannot rebind it. It lives in public because that is a schema every
--   search_path the service uses reaches.
--
-- 'simple', NOT 'spanish'
--   Both generated columns and the query vectors use the 'simple'
--   configuration: a stemming analyzer would fold place and dish names into
--   each other ('terrazas' must NOT match 'terraza'), and accent folding
--   already happens through the wrapper. The stored vector and the query
--   vector are built by the SAME expression, so they always agree (S16).
--
-- WHAT THIS DOES
--   core.tb_place.search_name: tsvector over tx_title — the commercial name,
--   the only name the public read path exposes.
--   core.tb_menu_item.search_text: tsvector over
--   tx_name || ' ' || coalesce(tx_description, ''). The dish question is
--   answered by EXISTS over tb_menu -> tb_menu_item and returns PLACES (S17).
--   GIN index on each. Like co_location (16), both columns are derived,
--   never written by hand, and stay OUT of the history payloads: they derive
--   from columns the payloads already carry.
-- ============================================================================
\connect melia

CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- IMMUTABLE by declaration (see the header): wraps the STABLE two-argument
-- unaccent with the dictionary pinned, so generated columns and index
-- expressions may call it. STRICT: NULL in, NULL out.
CREATE OR REPLACE FUNCTION public.unaccent_immutable(text)
RETURNS text
LANGUAGE sql
IMMUTABLE
STRICT
PARALLEL SAFE
AS $$ SELECT public.unaccent('public.unaccent', $1) $$;

ALTER TABLE core.tb_place
    ADD COLUMN search_name tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', public.unaccent_immutable(tx_title))) STORED;

CREATE INDEX ix_tb_place_search_name ON core.tb_place USING gin (search_name);

ALTER TABLE core.tb_menu_item
    ADD COLUMN search_text tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', public.unaccent_immutable(tx_name || ' ' || coalesce(tx_description, '')))) STORED;

CREATE INDEX ix_tb_menu_item_search_text ON core.tb_menu_item USING gin (search_text);

COMMENT ON FUNCTION public.unaccent_immutable(text) IS
    'IMMUTABLE wrapper over public.unaccent with the dictionary pinned (''public.unaccent''): the base function is STABLE and cannot appear in a generated column or index expression. The declaration is a correctness decision (migration 19 header): the wrapper must stay a one-line call to the fixed-dictionary form. Used by tb_place.search_name and tb_menu_item.search_text, and by the query side so both vectors come from the same expression.';

COMMENT ON COLUMN core.tb_place.search_name IS
    'Search vector of the commercial name (tx_title), GENERATED ALWAYS AS to_tsvector(''simple'', public.unaccent_immutable(tx_title)) STORED. Accent-folded through the immutable wrapper; ''simple'' on purpose, so names are never stemmed into each other. Derived, never written by hand; kept out of tb_place_history like co_location. Migration 19.';

COMMENT ON COLUMN core.tb_menu_item.search_text IS
    'Search vector of the dish (tx_name + tx_description), GENERATED ALWAYS AS to_tsvector(''simple'', public.unaccent_immutable(tx_name || '' '' || coalesce(tx_description, ''''))) STORED. The dish question returns PLACES via tb_menu. Derived, never written by hand; kept out of tb_menu_item_history. Migration 19.';
