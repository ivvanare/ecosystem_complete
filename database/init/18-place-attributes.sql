-- ============================================================================
-- 18 — PLACE ATTRIBUTES — the governed many-to-many vocabulary; new categories.
-- Spec: database/diagram_base.txt · Change: openspec/changes/social-discovery
-- (Phase 1, S15/S23) · Feature: odd/tasks/place-discovery.md (commit 3)
--
-- WHAT THIS DOES
--   core.tb_place_attribute: the governed vocabulary of what is PRESENT ON a
--   place (terraza, petfriendly, frente_playa, ...). Same pattern as
--   core.tb_place_category and seg.tb_permission: business data, never a
--   PostgreSQL enum, so the backoffice retires or adds vocabulary without a
--   type migration (model rule 12). tx_code is unique among LIVE rows: the
--   table has soft delete (rule 4), so the unique is partial and a retired
--   attribute frees its code.
--   core.tb_place_attribute_link: the many-to-many bridge, with a COMPOSITE
--   primary key and NO soft delete — the same reason tb_route_place has none:
--   a logically deleted link would block re-adding the same attribute to the
--   same place. No history table either: bridges are not in the point-in-time
--   set (07-history-tables.sql).
--   Categories stay ONE per place: "is this a rumba or a restaurant" is a
--   category question (S23), so rumba / restaurante / bar are seeded into
--   core.tb_place_category HERE — 06-seeds.sql and 10-locale-catalogs.sql are
--   recorded in seg.tb_schema_migration and an applied migration is never
--   edited.
--
-- SEEDS
--   The five attributes the product names: terraza, petfriendly, frente_playa,
--   aire_libre, musica_en_vivo. Idempotent through the partial unique, like
--   every other catalog seed. The API's edge vocabulary mirrors exactly these
--   codes until attribute CRUD exists: an attribute the catalogue does not
--   know is rejected, never created on the fly (S23).
-- ============================================================================
\connect melia

CREATE TABLE core.tb_place_attribute (
    co_attribute    uuid PRIMARY KEY,
    -- Stable key the code compares against: terraza, petfriendly, ...
    tx_code         varchar NOT NULL,
    tx_name         varchar NOT NULL,
    tx_description  text,
    tx_status       public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at   timestamp NOT NULL,
    dt_updated_at   timestamp NOT NULL,
    dt_deleted_at   timestamp
);

-- [PARTIAL UNIQUE] rule 4: a retired attribute frees its code.
CREATE UNIQUE INDEX ux_tb_place_attribute_code_active
    ON core.tb_place_attribute (tx_code)
    WHERE dt_deleted_at IS NULL;

CREATE TABLE core.tb_place_attribute_link (
    co_place      uuid NOT NULL,
    co_attribute  uuid NOT NULL,
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    CONSTRAINT pk_tb_place_attribute_link PRIMARY KEY (co_place, co_attribute)
);

ALTER TABLE core.tb_place_attribute_link
    ADD CONSTRAINT fk_tb_place_attribute_link_place
    FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place);

ALTER TABLE core.tb_place_attribute_link
    ADD CONSTRAINT fk_tb_place_attribute_link_attribute
    FOREIGN KEY (co_attribute) REFERENCES core.tb_place_attribute (co_attribute);

-- Reverse navigation: "which places carry this attribute".
CREATE INDEX ix_tb_place_attribute_link_attribute
    ON core.tb_place_attribute_link (co_attribute);

-- The vocabulary the product names (S23). Idempotent via the partial unique.
INSERT INTO core.tb_place_attribute
    (co_attribute, tx_code, tx_name, tx_description, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'terraza',       'Terraza',          'Mesa o espacio al aire libre, techado o no', 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'petfriendly',   'Pet friendly',     'Admite mascotas',                            'ACTIVE', now(), now()),
    (gen_random_uuid(), 'frente_playa',  'Frente a la playa','Ubicación directa sobre la playa',           'ACTIVE', now(), now()),
    (gen_random_uuid(), 'aire_libre',    'Aire libre',       'Espacio abierto, sin techo',                 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'musica_en_vivo','Música en vivo',   'Presentaciones musicales en directo',        'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;

-- The categories the product names (S23). Same governed catalog as
-- hotel/arepera/parque, seeded here because 06 and 10 are applied.
INSERT INTO core.tb_place_category
    (co_place_category, tx_code, tx_name, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'rumba',       'Rumba',       'ACTIVE', now(), now()),
    (gen_random_uuid(), 'restaurante', 'Restaurante', 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'bar',         'Bar',         'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;

COMMENT ON TABLE core.tb_place_attribute IS
    'Governed vocabulary of what is present on a place (terraza, petfriendly, frente_playa, aire_libre, musica_en_vivo). Business data like tb_place_category, never an enum; tx_code is unique among live rows so a retired attribute frees its code. Migration 18.';
COMMENT ON COLUMN core.tb_place_attribute.co_attribute IS 'UUID of the attribute.';
COMMENT ON COLUMN core.tb_place_attribute.tx_code IS 'Stable key the code compares against: terraza, petfriendly, frente_playa, aire_libre, musica_en_vivo.';
COMMENT ON COLUMN core.tb_place_attribute.tx_name IS 'Display name: Terraza, Pet friendly, Frente a la playa.';
COMMENT ON COLUMN core.tb_place_attribute.tx_description IS 'What the attribute means, for the admin panel.';
COMMENT ON COLUMN core.tb_place_attribute.tx_status IS 'Lifecycle of the catalog row: ACTIVE or INACTIVE.';
COMMENT ON COLUMN core.tb_place_attribute.dt_created_at IS 'Creation timestamp.';
COMMENT ON COLUMN core.tb_place_attribute.dt_updated_at IS 'Last update timestamp.';
COMMENT ON COLUMN core.tb_place_attribute.dt_deleted_at IS 'Logical delete; frees tx_code through the partial unique.';

COMMENT ON TABLE core.tb_place_attribute_link IS
    'Bridge place <-> attribute. Composite primary key and no soft delete, like tb_route_place: a logically deleted link would block re-adding the same attribute to the same place. Not in the point-in-time set. Migration 18.';
COMMENT ON COLUMN core.tb_place_attribute_link.co_place IS 'Place carrying the attribute.';
COMMENT ON COLUMN core.tb_place_attribute_link.co_attribute IS 'Attribute carried by the place.';
COMMENT ON COLUMN core.tb_place_attribute_link.dt_created_at IS 'Creation timestamp.';
COMMENT ON COLUMN core.tb_place_attribute_link.dt_updated_at IS 'Last update timestamp.';
