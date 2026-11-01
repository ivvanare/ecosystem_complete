-- ============================================================================
-- HISTORY — point-in-time record for the six entities whose past has
-- analytical value. Spec: database/diagram_base.txt (section "HISTORY").
-- Feature: odd/tasks/db-history.md
--
-- WHY HISTORY TABLES AND NOT COLUMNS ON THE ENTITY
--   Adding dt_valid_from/dt_valid_to to the entity would put history inside the
--   operational table: every query would need `WHERE dt_valid_to IS NULL`, and
--   every Go model would have to grow. History lives beside the entity instead,
--   written by triggers (08-history-triggers.sql), so no existing column, index
--   or constraint changes and the API models stay as they are.
--
-- WHY NO FOREIGN KEY TO THE ENTITY
--   Two of the sources are deleted physically on purpose (tb_place_schedule,
--   tb_user_role, tb_role_permission — rule 3: a logically deleted bridge would
--   block re-inserting the same pair). An FK would either forbid that delete or
--   cascade and destroy the very history it is supposed to keep. The same reason
--   tb_security_event carries no FK to tb_user (rule 11). co_actor_user has no
--   FK either: an audit trail must survive user deletion.
--
-- WHY NO BUSINESS CHECK CONSTRAINTS HERE
--   A history row records what already happened and was already validated by the
--   entity. Duplicating rating 1..5 or the latitude range here would make the
--   history reject its own past the day a business rule changes or is relaxed.
--   Only the interval sanity check is enforced.
--
-- RELATIONSHIP TO tb_security_event (rule 11)
--   They answer different questions and neither replaces the other:
--   tb_security_event says WHO did WHAT and WHEN; these tables say WHAT THE STATE
--   WAS at an instant. Do not duplicate event rows here.
--
-- SHAPE OF ONE ROW
--   co_<entity>_history  surrogate id of the history row
--   co_<entity>          the entity this interval describes
--   <payload>            the attributes needed to interpret the interval
--   tx_operation         INSERT | UPDATE | DELETE (DELETE also covers the soft
--                        delete of an entity, which is an UPDATE on the source)
--   co_actor_user        who made the change, when the service announces it via
--                        set_config('app.current_user_id', ...); NULL otherwise
--   dt_valid_from        instant the state became true
--   dt_valid_to          instant it stopped being true; NULL = still true
--
--   Closed intervals are immutable: an interval is closed exactly once and never
--   reopened (enforced in 08). `CHECK (dt_valid_to >= dt_valid_from)` accepts a
--   zero-length interval so that two changes inside one transaction can close and
--   open at the same instant without leaving a gap.
-- ============================================================================
\connect melia

-- New enum introduced by an already-applied model. It lives in public like the
-- other shared enums, because seg and core history tables both use it.
CREATE TYPE public.change_operation AS ENUM ('INSERT', 'UPDATE', 'DELETE');

-- ---------------------------------------------------------------------------
-- core.tb_place_history — owner, identity, category, coordinates, status.
-- Answers "what did this place look like then" and "how long was it suspended".
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_place_history (
    co_place_history uuid PRIMARY KEY,
    co_place         uuid NOT NULL,
    co_owner_user    uuid,
    tx_name          varchar NOT NULL,
    tx_title         varchar NOT NULL,
    tx_description   text,
    tx_category      varchar NOT NULL,
    nu_latitude      decimal(9,6) NOT NULL,
    nu_longitude     decimal(9,6) NOT NULL,
    tx_address       varchar,
    js_social_media  jsonb,
    tx_status        public.suspendable_status NOT NULL,
    dt_deleted_at    timestamp,
    tx_operation     public.change_operation NOT NULL,
    co_actor_user    uuid,
    dt_valid_from    timestamp NOT NULL,
    dt_valid_to      timestamp,
    CONSTRAINT ck_tb_place_history_interval
        CHECK (dt_valid_to IS NULL OR dt_valid_to >= dt_valid_from)
);

-- Exactly one open interval per entity: this is what makes "the current state"
-- unambiguous, and it is the partial-unique equivalent of rule 4.
CREATE UNIQUE INDEX ux_tb_place_history_open
    ON core.tb_place_history (co_place)
    WHERE dt_valid_to IS NULL;

-- Point-in-time lookup: the latest interval starting at or before T.
CREATE INDEX ix_tb_place_history_validity
    ON core.tb_place_history (co_place, dt_valid_from DESC);

-- ---------------------------------------------------------------------------
-- core.tb_menu_item_history — price, name, description, status.
-- The case that motivated this feature: "the price this dish had when the
-- review was written". The price is mutable and had no past at all.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_menu_item_history (
    co_menu_item_history uuid PRIMARY KEY,
    co_menu_item         uuid NOT NULL,
    -- The item may move between menus, so the owning menu is part of the state.
    co_menu              uuid NOT NULL,
    tx_name              varchar NOT NULL,
    tx_description       text,
    nu_price             decimal(10,2) NOT NULL,
    tx_status            public.activatable_status NOT NULL,
    dt_deleted_at        timestamp,
    tx_operation         public.change_operation NOT NULL,
    co_actor_user        uuid,
    dt_valid_from        timestamp NOT NULL,
    dt_valid_to          timestamp,
    CONSTRAINT ck_tb_menu_item_history_interval
        CHECK (dt_valid_to IS NULL OR dt_valid_to >= dt_valid_from)
);

CREATE UNIQUE INDEX ux_tb_menu_item_history_open
    ON core.tb_menu_item_history (co_menu_item)
    WHERE dt_valid_to IS NULL;

CREATE INDEX ix_tb_menu_item_history_validity
    ON core.tb_menu_item_history (co_menu_item, dt_valid_from DESC);

-- ---------------------------------------------------------------------------
-- core.tb_place_schedule_history — day, open, close.
-- This table is deleted PHYSICALLY (rule 3), so until now a removed schedule
-- left no trace whatsoever. The payload is complete on purpose: the entity row
-- may no longer exist when someone asks what the schedule used to be.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_place_schedule_history (
    co_schedule_history uuid PRIMARY KEY,
    co_schedule         uuid NOT NULL,
    co_place            uuid NOT NULL,
    nu_day_of_week      int NOT NULL,
    dt_open_time        time NOT NULL,
    dt_close_time       time NOT NULL,
    tx_operation        public.change_operation NOT NULL,
    co_actor_user       uuid,
    dt_valid_from       timestamp NOT NULL,
    dt_valid_to         timestamp,
    CONSTRAINT ck_tb_place_schedule_history_interval
        CHECK (dt_valid_to IS NULL OR dt_valid_to >= dt_valid_from)
);

CREATE UNIQUE INDEX ux_tb_place_schedule_history_open
    ON core.tb_place_schedule_history (co_schedule)
    WHERE dt_valid_to IS NULL;

-- "What were this place's schedules on date X" is the question that matters here,
-- so the validity index is led by the place.
CREATE INDEX ix_tb_place_schedule_history_validity
    ON core.tb_place_schedule_history (co_place, dt_valid_from DESC);

-- ---------------------------------------------------------------------------
-- core.tb_review_history — rating, comment, status.
-- Makes the moderation funnel measurable: how long a review spent ACTIVE before
-- MODERATED, and how the rating moved. One review per user and place means
-- co_user and co_place are immutable, so they are not duplicated here; join the
-- entity (reviews are soft deleted, never purged).
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_review_history (
    co_review_history uuid PRIMARY KEY,
    co_review         uuid NOT NULL,
    nu_rating         int NOT NULL,
    tx_comment        text,
    tx_status         public.review_status NOT NULL,
    dt_deleted_at     timestamp,
    tx_operation      public.change_operation NOT NULL,
    co_actor_user     uuid,
    dt_valid_from     timestamp NOT NULL,
    dt_valid_to       timestamp,
    CONSTRAINT ck_tb_review_history_interval
        CHECK (dt_valid_to IS NULL OR dt_valid_to >= dt_valid_from)
);

CREATE UNIQUE INDEX ux_tb_review_history_open
    ON core.tb_review_history (co_review)
    WHERE dt_valid_to IS NULL;

CREATE INDEX ix_tb_review_history_validity
    ON core.tb_review_history (co_review, dt_valid_from DESC);

-- ---------------------------------------------------------------------------
-- seg.tb_user_role_history — user/role link.
-- Physically deleted today, so "who was ADMIN on date X" was unanswerable.
-- The full payload is stored because the bridge row itself disappears.
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_user_role_history (
    co_user_role_history uuid PRIMARY KEY,
    co_user              uuid NOT NULL,
    co_role              uuid NOT NULL,
    tx_operation         public.change_operation NOT NULL,
    co_actor_user        uuid,
    dt_valid_from        timestamp NOT NULL,
    dt_valid_to          timestamp,
    CONSTRAINT ck_tb_user_role_history_interval
        CHECK (dt_valid_to IS NULL OR dt_valid_to >= dt_valid_from)
);

CREATE UNIQUE INDEX ux_tb_user_role_history_open
    ON seg.tb_user_role_history (co_user, co_role)
    WHERE dt_valid_to IS NULL;

CREATE INDEX ix_tb_user_role_history_validity
    ON seg.tb_user_role_history (co_user, dt_valid_from DESC);

-- The reverse question: every user who held a role over a period.
CREATE INDEX ix_tb_user_role_history_role
    ON seg.tb_user_role_history (co_role, dt_valid_from DESC);

-- ---------------------------------------------------------------------------
-- seg.tb_role_permission_history — role/permission link.
-- Answers "what could a role do on date X", i.e. permission drift, which is a
-- security question as much as an analytical one.
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_role_permission_history (
    co_role_permission_history uuid PRIMARY KEY,
    co_role                    uuid NOT NULL,
    co_permission              uuid NOT NULL,
    tx_operation               public.change_operation NOT NULL,
    co_actor_user              uuid,
    dt_valid_from              timestamp NOT NULL,
    dt_valid_to                timestamp,
    CONSTRAINT ck_tb_role_permission_history_interval
        CHECK (dt_valid_to IS NULL OR dt_valid_to >= dt_valid_from)
);

CREATE UNIQUE INDEX ux_tb_role_permission_history_open
    ON seg.tb_role_permission_history (co_role, co_permission)
    WHERE dt_valid_to IS NULL;

CREATE INDEX ix_tb_role_permission_history_validity
    ON seg.tb_role_permission_history (co_role, dt_valid_from DESC);

-- The reverse question: every role that granted a permission over a period.
CREATE INDEX ix_tb_role_permission_history_permission
    ON seg.tb_role_permission_history (co_permission, dt_valid_from DESC);
