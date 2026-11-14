-- ============================================================================
-- 17 — PLACE REGION — tx_region on core.tb_place, mirrored into history.
-- Spec: database/diagram_base.txt · Change: openspec/changes/social-discovery
-- (Phase 0, S3/S15) · Feature: odd/tasks/geo-foundation.md
--
-- WHAT THIS DOES
--   core.tb_place gains tx_region varchar(6) NULL holding an ISO 3166-2
--   subdivision code (VE-A, US-CA): country code, hyphen, subdivision. The
--   CHECK asserts the shape, exactly like tx_country asserts its own. NULL is
--   deliberate: many places legitimately have no subdivision data, and
--   requiring it would make a place uninsertable wherever the geocoder cannot
--   answer (S3). Assignment is by reverse geocoding at creation, never typed
--   as free text — and nothing in the service creates places yet, so the
--   column ships with no writer and the hook arrives with the places write
--   path. ix_tb_place_country_region (tx_country, tx_region) in that order,
--   because "list regions of a country" and "list places of a region" are the
--   two navigation reads.
--
-- HISTORY MIRRORS THE ENTITY
--   tb_place is in the point-in-time set, so its history payload gains
--   tx_region too (S15). The history ALTER carries no CHECK: history records
--   what the entity already validated (07-history-tables.sql). The function
--   is swapped with CREATE OR REPLACE, never DROP+CREATE, because a trigger
--   binds by name — the triggers from 08 stay as they are.
--   Body copied verbatim from 11-locale-columns.sql; ONLY the column lists
--   and values change (+ tx_region). co_location does NOT join the payload:
--   it is derived from the decimals the payload already carries (16).
-- ============================================================================
\connect melia

ALTER TABLE core.tb_place
    ADD COLUMN tx_region varchar(6) NULL
        CONSTRAINT ck_tb_place_region CHECK (tx_region ~ '^[A-Z]{2}-[A-Z0-9]{1,3}$');

CREATE INDEX ix_tb_place_country_region ON core.tb_place (tx_country, tx_region);

ALTER TABLE core.tb_place_history
    ADD COLUMN tx_region varchar(6) NULL;

CREATE OR REPLACE FUNCTION core.tg_tb_place_history() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_ts timestamp := now();
BEGIN
    IF TG_OP = 'UPDATE'
       AND (to_jsonb(NEW) - 'dt_updated_at') = (to_jsonb(OLD) - 'dt_updated_at') THEN
        RETURN NEW;
    END IF;

    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE core.tb_place_history
           SET dt_valid_to = v_ts
         WHERE co_place = OLD.co_place AND dt_valid_to IS NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        INSERT INTO core.tb_place_history (
            co_place_history, co_place, co_owner_user, tx_name, tx_title, tx_description,
            co_place_category, tx_country, tx_timezone, tx_region, nu_latitude, nu_longitude,
            tx_address, js_social_media,
            tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_place, OLD.co_owner_user, OLD.tx_name, OLD.tx_title,
            OLD.tx_description, OLD.co_place_category, OLD.tx_country, OLD.tx_timezone,
            OLD.tx_region, OLD.nu_latitude, OLD.nu_longitude,
            OLD.tx_address, OLD.js_social_media, OLD.tx_status, OLD.dt_deleted_at,
            'DELETE', seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_place_history (
        co_place_history, co_place, co_owner_user, tx_name, tx_title, tx_description,
        co_place_category, tx_country, tx_timezone, tx_region, nu_latitude, nu_longitude,
        tx_address, js_social_media,
        tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_place, NEW.co_owner_user, NEW.tx_name, NEW.tx_title,
        NEW.tx_description, NEW.co_place_category, NEW.tx_country, NEW.tx_timezone,
        NEW.tx_region, NEW.nu_latitude, NEW.nu_longitude,
        NEW.tx_address, NEW.js_social_media, NEW.tx_status, NEW.dt_deleted_at,
        TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

COMMENT ON COLUMN core.tb_place.tx_region IS
    'ISO 3166-2 subdivision code (VE-A, US-CA); the CHECK asserts the shape only. NULL on purpose: requiring it would make a place uninsertable where the geocoder cannot answer. Assigned by reverse geocoding at creation; indexed with tx_country for the navigation reads.';

COMMENT ON COLUMN core.tb_place_history.tx_region IS
    'Region of the place during the interval, mirrored from the entity with no FK and no CHECK: history records what the entity already validated.';
