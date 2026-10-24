-- ============================================================================
-- ESQUEMA core — DOMAIN
-- Canonical spec: database/diagram_base.txt (sections "ESQUEMA core").
-- Cross-schema FK columns (co_user / co_owner_user -> seg.tb_user) are indexed
-- here (rule 2); the constraints live in 05-foreign-keys.sql.
-- ============================================================================
\connect melzone

-- ---------------------------------------------------------------------------
-- tb_place: a venue (e.g. MELIA). co_owner_user is NULL for backoffice-curated
-- places (optional FK, DBML `>?`).
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_place (
    co_place        uuid PRIMARY KEY,
    -- Place owner. NULL on backoffice-curated places.
    co_owner_user   uuid,
    -- Real/legal name.
    tx_name         varchar NOT NULL,
    -- Commercial name for the app.
    tx_title        varchar NOT NULL,
    tx_description  text,
    -- Hotel, Arepera, Parque, etc.
    tx_category     varchar NOT NULL,
    nu_latitude     decimal(9,6) NOT NULL CHECK (nu_latitude BETWEEN -90 AND 90),
    nu_longitude    decimal(9,6) NOT NULL CHECK (nu_longitude BETWEEN -180 AND 180),
    tx_address      varchar,
    -- {"instagram": "...", "web": "..."}
    js_social_media jsonb,
    tx_status       public.suspendable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at   timestamp NOT NULL,
    dt_updated_at   timestamp NOT NULL,
    dt_deleted_at   timestamp
);

CREATE INDEX ix_tb_place_owner ON core.tb_place (co_owner_user);

-- ---------------------------------------------------------------------------
-- tb_place_schedule: per-day schedules; replaces a single open/close pair and
-- supports venues that cross midnight. Physical delete (rule 3, no soft delete).
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_place_schedule (
    co_schedule    uuid PRIMARY KEY,
    co_place       uuid NOT NULL,
    -- ISO-8601: 1=Monday ... 7=Sunday.
    nu_day_of_week int NOT NULL CHECK (nu_day_of_week BETWEEN 1 AND 7),
    dt_open_time   time NOT NULL,
    -- If <= dt_open_time, closing is on the NEXT day (crosses midnight).
    -- E.g. opens 20:00, closes 02:00.
    dt_close_time  time NOT NULL,
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL
);

CREATE UNIQUE INDEX ux_tb_place_schedule_slot
    ON core.tb_place_schedule (co_place, nu_day_of_week, dt_open_time);

CREATE INDEX ix_tb_place_schedule_place ON core.tb_place_schedule (co_place);

-- ---------------------------------------------------------------------------
-- tb_place_photo: COVER or GALLERY. Only one COVER per place: application rule.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_place_photo (
    co_photo      uuid PRIMARY KEY,
    co_place      uuid NOT NULL,
    -- Image URL in S3/Cloud.
    tx_url        varchar NOT NULL,
    tx_type       public.photo_kind NOT NULL,
    nu_order      int NOT NULL,
    tx_status     public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp
);

CREATE INDEX ix_tb_place_photo_place ON core.tb_place_photo (co_place);

-- ---------------------------------------------------------------------------
-- tb_menu: intermediate level between place and dish — a place may have several
-- menus (Desayuno, Almuerzo, Cena, Vinos).
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_menu (
    co_menu       uuid PRIMARY KEY,
    co_place      uuid NOT NULL,
    -- E.g. Desayuno, Almuerzo, Cena.
    tx_name       varchar NOT NULL,
    tx_description text,
    tx_status     public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp
);

CREATE INDEX ix_tb_menu_place ON core.tb_menu (co_place);

-- ---------------------------------------------------------------------------
-- tb_menu_item: hangs from the menu, not from the place.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_menu_item (
    co_menu_item  uuid PRIMARY KEY,
    co_menu       uuid NOT NULL,
    tx_name       varchar NOT NULL,
    tx_description text,
    nu_price      decimal(10,2) NOT NULL CHECK (nu_price >= 0),
    tx_status     public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp
);

CREATE INDEX ix_tb_menu_item_menu ON core.tb_menu_item (co_menu);

-- ---------------------------------------------------------------------------
-- tb_review: one review per user and place. The partial unique keeps a moderated
-- or deleted review from blocking the user from reviewing that place again.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_review (
    co_review     uuid PRIMARY KEY,
    -- Review author. Real cross-schema FK to seg.tb_user.
    co_user       uuid NOT NULL,
    co_place      uuid NOT NULL,
    nu_rating     int NOT NULL CHECK (nu_rating BETWEEN 1 AND 5),
    tx_comment    text,
    tx_status     public.review_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp
);

CREATE INDEX ix_tb_review_place ON core.tb_review (co_place);
CREATE INDEX ix_tb_review_user ON core.tb_review (co_user);

-- [PARTIAL UNIQUE] One review per user and place (diagram rule 4).
CREATE UNIQUE INDEX ux_tb_review_user_place_active
    ON core.tb_review (co_user, co_place)
    WHERE dt_deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- tb_route: user-created route (ordered visit list via tb_route_place).
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_route (
    co_route      uuid PRIMARY KEY,
    -- Creator user. Real cross-schema FK to seg.tb_user.
    co_user       uuid NOT NULL,
    tx_name       varchar NOT NULL,
    tx_description text,
    tx_status     public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp
);

CREATE INDEX ix_tb_route_user ON core.tb_route (co_user);

-- ---------------------------------------------------------------------------
-- tb_route_place: route <-> place bridge with visit order. No soft delete on
-- purpose (rule 3): a logically deleted row would block re-adding that place
-- to the same route.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_route_place (
    co_route      uuid NOT NULL,
    co_place      uuid NOT NULL,
    -- Visit order (1, 2, 3...).
    nu_order      int NOT NULL,
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    CONSTRAINT pk_tb_route_place PRIMARY KEY (co_route, co_place)
);

CREATE INDEX ix_tb_route_place_place ON core.tb_route_place (co_place);
