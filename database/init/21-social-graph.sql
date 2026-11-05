-- ============================================================================
-- 21 — SOCIAL GRAPH — asymmetric follow and append-only check-ins.
-- Spec: database/diagram_base.txt · Change: openspec/changes/social-discovery
-- (Phase 2, S5/S6/S10) · Feature: odd/tasks/social-graph.md
--
-- WHY NEITHER TABLE JOINS THE HISTORY SET
--   History answers "what was the state of this entity at instant T" for rows that change
--   in place (07-history-tables.sql). Follow and check-in are append-only facts: a follow
--   is created or removed, a check-in is never edited. Recording them would produce one
--   interval per row with no state to compare, so S15 keeps them out — deliberately, not
--   by omission. Do not "fix" this later without reading that rule.
--
-- WHY FOLLOW HAS NO SOFT DELETE
--   With a composite primary key, a logically deleted row would block ever following that
--   person again — the same reason tb_route_place has none.
-- ============================================================================

BEGIN;

-- ---------------------------------------------------------------------------
-- public.checkin_verification — how a check-in was established (resolved Q1).
--   SELF_REPORTED: the client claimed presence without a usable coordinate.
--   PROXIMITY: a coordinate was validated against the place radius and then discarded;
--              the coordinate and the distance are never stored (D11).
--   A closed set on purpose: adding SIGNED_TOKEN later is additive, a free-text verdict
--   would be a silent privilege bug.
-- ---------------------------------------------------------------------------
CREATE TYPE public.checkin_verification AS ENUM ('SELF_REPORTED', 'PROXIMITY');

-- ---------------------------------------------------------------------------
-- core.tb_follow — asymmetric graph, one direction per row.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_follow (
    co_follower  uuid NOT NULL,
    co_followed  uuid NOT NULL,
    dt_created_at timestamp NOT NULL,

    CONSTRAINT pk_tb_follow PRIMARY KEY (co_follower, co_followed),
    -- Self-follow is rejected by the database, not only by the service.
    CONSTRAINT ck_tb_follow_not_self CHECK (co_follower <> co_followed),
    CONSTRAINT fk_tb_follow_follower FOREIGN KEY (co_follower) REFERENCES seg.tb_user (co_user),
    CONSTRAINT fk_tb_follow_followed FOREIGN KEY (co_followed) REFERENCES seg.tb_user (co_user)
);

-- "Who follows me" is a first-class read, so the reverse direction is indexed too.
CREATE INDEX ix_tb_follow_followed ON core.tb_follow (co_followed);

-- ---------------------------------------------------------------------------
-- core.tb_checkin — append-only fact: no update path, no soft delete.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_checkin (
    co_checkin      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    co_user         uuid NOT NULL,
    co_place        uuid NOT NULL,
    tx_verification public.checkin_verification NOT NULL DEFAULT 'SELF_REPORTED',
    js_metadata     jsonb,
    dt_created_at   timestamp NOT NULL,
    -- The day bucket is generated so the uniqueness rule can be indexed: a check-in is
    -- once per person, place and day, and that lives in the schema (S10), not only in
    -- the service. date truncation is immutable for a timestamp without time zone.
    dt_day          date GENERATED ALWAYS AS (dt_created_at::date) STORED,

    CONSTRAINT fk_tb_checkin_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user),
    CONSTRAINT fk_tb_checkin_place FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place)
);

CREATE UNIQUE INDEX ux_tb_checkin_user_place_day ON core.tb_checkin (co_user, co_place, dt_day);
CREATE INDEX ix_tb_checkin_place ON core.tb_checkin (co_place);
CREATE INDEX ix_tb_checkin_user ON core.tb_checkin (co_user);

COMMENT ON TABLE core.tb_follow IS
    'Asymmetric follow graph (D6). "Friend" is a mutual follow computed at read time and never stored as a flag: the graph changes daily and a denormalized flag rots within a day. Composite primary key, no soft delete (a deleted row would block following that person again), self-follow rejected by ck_tb_follow_not_self. Migration 21.';
COMMENT ON TABLE core.tb_checkin IS
    'Append-only presence fact (S10). No update path and no soft delete: a check-in is history, not state, and it is therefore not in the point-in-time set. tx_verification records HOW presence was established (resolved Q1); the coordinate that proved it is validated at the edge and discarded, so no coordinate and no distance is ever stored (D11). ux_tb_checkin_user_place_day enforces one check-in per person, place and day in the schema. Migration 21.';

COMMIT;
