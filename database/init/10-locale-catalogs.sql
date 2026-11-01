-- ============================================================================
-- LOCALE CATALOGS — place categories and currencies as business data.
-- Spec: database/diagram_base.txt (ESQUEMA core, model rule 12).
--
-- WHY TABLES AND NOT ENUMS
--   Categories are business data managed through the admin panel — the same
--   pattern as seg.tb_permission: the backoffice creates, edits and retires
--   them without a deploy, while a PostgreSQL enum forces a type migration for
--   every new value and a value can never be removed. The currency catalog
--   records the ISO 4217 codes the application actually trades in, with
--   nu_decimals so a price can be formatted correctly.
--
-- RULE 4
--   Both tables have soft delete, so the unique on tx_code is PARTIAL
--   (WHERE dt_deleted_at IS NULL): a retired category or currency frees its
--   code. The seeds are idempotent precisely because they conflict against
--   that partial unique.
-- ============================================================================
\connect melia

-- ---------------------------------------------------------------------------
-- core.tb_place_category — what kind of place this is (Hotel, Arepera,
-- Parque...). The governed catalog behind core.tb_place.co_place_category
-- (11-locale-columns.sql), replacing the free-text tx_category.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_place_category (
    co_place_category uuid PRIMARY KEY,
    -- Stable key the code compares against: hotel, arepera, parque, ...
    tx_code           varchar NOT NULL,
    -- Display name: Hotel, Arepera, Parque.
    tx_name           varchar NOT NULL,
    tx_description    text,
    tx_status         public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at     timestamp NOT NULL,
    dt_updated_at     timestamp NOT NULL,
    dt_deleted_at     timestamp
);

-- [PARTIAL UNIQUE] rule 4: a retired category frees its code.
CREATE UNIQUE INDEX ux_tb_place_category_code_active
    ON core.tb_place_category (tx_code)
    WHERE dt_deleted_at IS NULL;

-- Seed: exactly the examples the diagram names, no more. Idempotent via the
-- partial unique above.
INSERT INTO core.tb_place_category
    (co_place_category, tx_code, tx_name, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'hotel',   'Hotel',   'ACTIVE', now(), now()),
    (gen_random_uuid(), 'arepera', 'Arepera', 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'parque',  'Parque',  'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;

-- ---------------------------------------------------------------------------
-- core.tb_currency — ISO 4217. A price without a currency is a number without
-- meaning; nu_decimals is what makes formatting it correct.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_currency (
    co_currency   uuid PRIMARY KEY,
    -- ISO 4217 alphabetic code: USD, VES, COP.
    tx_code       char(3) NOT NULL,
    tx_name       varchar NOT NULL,
    -- Decimal digits when formatting a price in this currency.
    nu_decimals   smallint NOT NULL CHECK (nu_decimals BETWEEN 0 AND 4),
    tx_status     public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp,
    CONSTRAINT ck_tb_currency_code CHECK (tx_code ~ '^[A-Z]{3}$')
);

-- [PARTIAL UNIQUE] rule 4.
CREATE UNIQUE INDEX ux_tb_currency_code_active
    ON core.tb_currency (tx_code)
    WHERE dt_deleted_at IS NULL;

INSERT INTO core.tb_currency
    (co_currency, tx_code, tx_name, nu_decimals, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'USD', 'Dólar estadounidense', 2, 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'VES', 'Bolívar',              2, 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'COP', 'Peso colombiano',      2, 'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;
