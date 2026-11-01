-- ============================================================================
-- LOCALE COLUMNS — category, country and timezone on tb_place; currency on
-- tb_menu_item; the history payload mirrors the entity.
-- Spec: database/diagram_base.txt (ESQUEMA core, model rule 12).
--
-- NOT NULL WITHOUT DEFAULT, ON PURPOSE
--   tb_place and tb_menu_item are EMPTY, so the new columns arrive NOT NULL
--   and WITHOUT a default: an insert cannot silently inherit Venezuela or USD.
--   Every writer must state the category, country, timezone and currency of
--   the row it creates.
--
-- WHAT EACH COLUMN IS FOR
--   tx_country (ISO 3166-1 alpha-2) is for grouping and reporting.
--   tx_timezone (IANA name) is what makes opening hours interpretable: it is
--   the zone that "open now" buckets by, stored on the ROW because the server
--   only stores UTC (09-utc-convention.sql).
--   co_currency turns nu_price into a price: without it 10.00 is a number,
--   not an amount, and nu_decimals in the catalog is what formats it right.
--
-- THE CHECKS ASSERT SHAPE, NOT VALIDITY
--   A CHECK cannot query pg_timezone_names, so ck_tb_place_timezone only
--   enforces the IANA shape ('Area/Location'). The real validation — that the
--   name exists — happens at the application edge, which can consult
--   pg_timezone_names.
--
-- HISTORY MIRRORS THE ENTITY, WITHOUT FK
--   A price history without its currency, or a place history without its
--   timezone, is not interpretable, so the payload gains the same columns.
--   They carry no FK, for the same reason 07-history-tables.sql gives:
--   history must survive catalog changes — a retired or deleted category or
--   currency must never break the past. tx_category leaves the payload: the
--   category worth keeping in the past is the governed one.
--
-- TRIGGERS ARE NOT RECREATED
--   A trigger binds to its function by name, so CREATE OR REPLACE FUNCTION
--   swaps the body underneath; the triggers from 08 stay as they are.
-- ============================================================================
\connect melia

-- ---------------------------------------------------------------------------
-- core.tb_place — governed category, country, timezone.
-- ---------------------------------------------------------------------------
ALTER TABLE core.tb_place
    ADD COLUMN co_place_category uuid NOT NULL,
    -- ISO 3166-1 alpha-2, for grouping and reporting.
    ADD COLUMN tx_country char(2) NOT NULL
        CONSTRAINT ck_tb_place_country CHECK (tx_country ~ '^[A-Z]{2}$'),
    -- IANA name (America/Caracas). Shape only: the name-exists validation is
    -- pg_timezone_names at the application edge, because a CHECK cannot query.
    ADD COLUMN tx_timezone varchar NOT NULL
        CONSTRAINT ck_tb_place_timezone CHECK (tx_timezone ~ '^[A-Za-z]+/[A-Za-z_]+$');

-- Rule 2: every FK column is indexed.
CREATE INDEX ix_tb_place_category ON core.tb_place (co_place_category);

ALTER TABLE core.tb_place
    ADD CONSTRAINT fk_tb_place_category
    FOREIGN KEY (co_place_category) REFERENCES core.tb_place_category (co_place_category);

-- ---------------------------------------------------------------------------
-- core.tb_menu_item — the price gets its currency.
-- ---------------------------------------------------------------------------
ALTER TABLE core.tb_menu_item
    ADD COLUMN co_currency uuid NOT NULL;

CREATE INDEX ix_tb_menu_item_currency ON core.tb_menu_item (co_currency);

ALTER TABLE core.tb_menu_item
    ADD CONSTRAINT fk_tb_menu_item_currency
    FOREIGN KEY (co_currency) REFERENCES core.tb_currency (co_currency);

-- ---------------------------------------------------------------------------
-- History payload mirrors the entity, with no FK (see the header).
-- ---------------------------------------------------------------------------
ALTER TABLE core.tb_place_history
    DROP COLUMN tx_category,
    ADD COLUMN co_place_category uuid NOT NULL,
    ADD COLUMN tx_country char(2) NOT NULL,
    ADD COLUMN tx_timezone varchar NOT NULL;

ALTER TABLE core.tb_menu_item_history
    ADD COLUMN co_currency uuid NOT NULL;

-- ---------------------------------------------------------------------------
-- Bodies copied verbatim from 08-history-triggers.sql; ONLY the column lists
-- and values change (tx_category -> co_place_category/tx_country/tx_timezone;
-- + co_currency). The no-op suppression via to_jsonb(NEW) - 'dt_updated_at',
-- the close-interval UPDATE before insert, the born-closed DELETE rows,
-- v_ts timestamp := now() and seg.current_actor_user() are exactly as 08
-- wrote them.
-- ---------------------------------------------------------------------------
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
            co_place_category, tx_country, tx_timezone, nu_latitude, nu_longitude,
            tx_address, js_social_media,
            tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_place, OLD.co_owner_user, OLD.tx_name, OLD.tx_title,
            OLD.tx_description, OLD.co_place_category, OLD.tx_country, OLD.tx_timezone,
            OLD.nu_latitude, OLD.nu_longitude,
            OLD.tx_address, OLD.js_social_media, OLD.tx_status, OLD.dt_deleted_at,
            'DELETE', seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_place_history (
        co_place_history, co_place, co_owner_user, tx_name, tx_title, tx_description,
        co_place_category, tx_country, tx_timezone, nu_latitude, nu_longitude,
        tx_address, js_social_media,
        tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_place, NEW.co_owner_user, NEW.tx_name, NEW.tx_title,
        NEW.tx_description, NEW.co_place_category, NEW.tx_country, NEW.tx_timezone,
        NEW.nu_latitude, NEW.nu_longitude,
        NEW.tx_address, NEW.js_social_media, NEW.tx_status, NEW.dt_deleted_at,
        TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION core.tg_tb_menu_item_history() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_ts timestamp := now();
BEGIN
    IF TG_OP = 'UPDATE'
       AND (to_jsonb(NEW) - 'dt_updated_at') = (to_jsonb(OLD) - 'dt_updated_at') THEN
        RETURN NEW;
    END IF;

    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE core.tb_menu_item_history
           SET dt_valid_to = v_ts
         WHERE co_menu_item = OLD.co_menu_item AND dt_valid_to IS NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        INSERT INTO core.tb_menu_item_history (
            co_menu_item_history, co_menu_item, co_menu, tx_name, tx_description,
            nu_price, co_currency, tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_menu_item, OLD.co_menu, OLD.tx_name,
            OLD.tx_description, OLD.nu_price, OLD.co_currency, OLD.tx_status, OLD.dt_deleted_at,
            'DELETE', seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_menu_item_history (
        co_menu_item_history, co_menu_item, co_menu, tx_name, tx_description,
        nu_price, co_currency, tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_menu_item, NEW.co_menu, NEW.tx_name,
        NEW.tx_description, NEW.nu_price, NEW.co_currency, NEW.tx_status, NEW.dt_deleted_at,
        TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;
