-- ============================================================================
-- ESQUEMA seg — IDENTITY AND PERMISSIONS
-- Canonical spec: database/diagram_base.txt (sections "ESQUEMA seg").
-- Column prefixes per diagram rule 1: co_ uuid, tx_ text, nu_ numeric,
-- dt_ timestamp, js_ json. Lifecycle buckets per rule 3 (see spec S4).
-- ============================================================================
\connect melia

-- ---------------------------------------------------------------------------
-- tb_user: THE base user table. App and backoffice users are the same entity;
-- capabilities derive from RBAC (user -> role -> permission), never from a
-- user "type" column (diagram rule 6).
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_user (
    co_user               uuid PRIMARY KEY,
    -- NULL allowed: with Apple the name arrives ONLY on the first authorization.
    tx_name               varchar,
    -- NULL allowed: Apple may not send it or may send a @privaterelay.appleid.com
    -- relay. PostgreSQL admits multiple NULLs in a unique index.
    tx_email              varchar,
    -- NULL allowed: users who only signed in with Google/Apple have no password.
    -- Argon2id or bcrypt, never plaintext.
    tx_password_hash      varchar,
    -- When the email was proven (signup code or provider email_verified claim).
    dt_email_verified_at  timestamp,
    -- Consecutive failed logins; reset on success. Without this login is
    -- brute-forceable.
    nu_failed_attempts    int NOT NULL DEFAULT 0,
    -- Temporary lockout from failed attempts. NULL = not locked.
    dt_locked_until       timestamp,
    tx_status             public.suspendable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at         timestamp NOT NULL,
    dt_updated_at         timestamp NOT NULL,
    dt_deleted_at         timestamp
);

-- [PARTIAL UNIQUE] A plain unique would keep a deleted user's email occupied
-- forever (diagram rule 4).
CREATE UNIQUE INDEX ux_tb_user_email_active
    ON seg.tb_user (tx_email)
    WHERE dt_deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- tb_refresh_token: only the refresh token is persisted (access tokens are
-- stateless, diagram rule 7). Rotation + family: a reused (already rotated)
-- token revokes the whole family. Operational state lives in columns, no soft
-- delete (rule 3).
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_refresh_token (
    co_refresh_token  uuid PRIMARY KEY,
    co_user           uuid NOT NULL,
    -- SHA-256 of the token. NEVER the raw token: if the DB leaks, raw tokens are
    -- account takeover of every active session.
    tx_token_hash     varchar NOT NULL UNIQUE,
    -- Groups the rotation chain; reuse of a rotated token (theft signal) revokes
    -- the complete family.
    co_token_family   uuid NOT NULL,
    -- User-facing label: "iPhone de Ana".
    tx_device         varchar,
    tx_user_agent     varchar,
    tx_ip             varchar,
    dt_expires_at     timestamp NOT NULL,
    -- NULL = active. Expired and revoked are different states, counted apart.
    dt_revoked_at     timestamp,
    dt_created_at     timestamp NOT NULL,
    dt_updated_at     timestamp NOT NULL
);

-- Every FK column gets an index: PostgreSQL does not index FKs automatically
-- (diagram rule 2). FK constraints live in 05-foreign-keys.sql.
CREATE INDEX ix_tb_refresh_token_user ON seg.tb_refresh_token (co_user);
CREATE INDEX ix_tb_refresh_token_family ON seg.tb_refresh_token (co_token_family);

-- ---------------------------------------------------------------------------
-- tb_user_token: one-time 6-digit code for an EXISTING user (password reset,
-- email change). Application rule (not expressible as a partial unique, the
-- predicate would depend on now()): issuing a new code invalidates previous
-- ones of the same user + purpose by marking dt_used_at.
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_user_token (
    co_user_token  uuid PRIMARY KEY,
    co_user        uuid NOT NULL,
    tx_purpose     public.user_token_purpose NOT NULL,
    -- SHA-256 of the 6-digit code sent by email. NEVER the raw code.
    tx_code_hash   varchar NOT NULL UNIQUE,
    -- Email the code was sent to. On EMAIL_CHANGE it is the NEW address; on
    -- PASSWORD_RESET the address current at request time. Compared at
    -- consumption: if the email changed meanwhile, the code is rejected.
    tx_target      varchar NOT NULL,
    -- Validation attempts, cut at 3-5. The control that actually protects:
    -- 6 digits are 1.000.000 combinations (diagram rule 10).
    nu_attempts    int NOT NULL DEFAULT 0,
    -- Short life: 5 to 10 minutes.
    dt_expires_at  timestamp NOT NULL,
    -- Single use. NULL = unused. Reuse must fail.
    dt_used_at     timestamp,
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL
);

CREATE INDEX ix_tb_user_token_user ON seg.tb_user_token (co_user);
CREATE INDEX ix_tb_user_token_purpose ON seg.tb_user_token (tx_purpose);

-- ---------------------------------------------------------------------------
-- tb_signup_request: pending registration; the user does NOT exist in tb_user
-- yet (diagram rule 9). No soft delete: it either becomes a user or expires
-- and is deleted.
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_signup_request (
    co_signup_request  uuid PRIMARY KEY,
    -- Email to verify. IMMUTABLE during registration: if it changes, a new code
    -- must be issued for the new address.
    tx_email           varchar NOT NULL UNIQUE,
    -- Copied to tb_user on verification.
    tx_name            varchar NOT NULL,
    -- Argon2id, hashed BEFORE storing. Never plaintext, not even for an
    -- unverified registration.
    tx_password_hash   varchar NOT NULL,
    -- SHA-256 of the 6-digit code sent by email. NEVER the raw code.
    tx_code_hash       varchar NOT NULL,
    nu_attempts        int NOT NULL DEFAULT 0,
    -- 10 minutes at most.
    dt_expires_at      timestamp NOT NULL,
    dt_created_at      timestamp NOT NULL,
    dt_updated_at      timestamp NOT NULL
);

-- ---------------------------------------------------------------------------
-- tb_user_identity: one row per linked provider. The same user may have Google
-- AND Apple; resolution is ALWAYS by (provider, sub), never by email (rule 8).
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_user_identity (
    co_user_identity      uuid PRIMARY KEY,
    co_user               uuid NOT NULL,
    tx_provider           public.identity_provider NOT NULL,
    -- The provider's `sub` claim. THE federated login resolution key.
    tx_provider_user_id   varchar NOT NULL,
    -- Email returned by the provider (may be an Apple relay). Informational only;
    -- identity is never resolved through it.
    tx_email              varchar,
    dt_linked_at          timestamp NOT NULL,
    dt_created_at         timestamp NOT NULL,
    dt_updated_at         timestamp NOT NULL,
    dt_deleted_at         timestamp
);

-- [PARTIAL UNIQUE] Prevents the same person ending up with two users depending
-- on which provider they signed in with.
CREATE UNIQUE INDEX ux_tb_user_identity_provider_sub
    ON seg.tb_user_identity (tx_provider, tx_provider_user_id)
    WHERE dt_deleted_at IS NULL;

CREATE INDEX ix_tb_user_identity_user ON seg.tb_user_identity (co_user);

-- ---------------------------------------------------------------------------
-- RBAC: role groups permissions; permission is an application-seeded catalog,
-- not something created from the UI (rule 6).
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_role (
    co_role        uuid PRIMARY KEY,
    -- E.g. SUPERADMIN, ADMIN, EDITOR.
    tx_name        varchar NOT NULL,
    tx_description text,
    tx_status      public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL,
    dt_deleted_at  timestamp
);

CREATE UNIQUE INDEX ux_tb_role_name_active
    ON seg.tb_role (tx_name)
    WHERE dt_deleted_at IS NULL;

CREATE TABLE seg.tb_permission (
    co_permission  uuid PRIMARY KEY,
    -- Stable key evaluated by code: place.create, place.update, review.moderate,
    -- admin.access.
    tx_code        varchar NOT NULL,
    -- UI grouping for the backoffice: PLACES, MENUS, REVIEWS, ROUTES, USERS, ROLES.
    tx_module      varchar NOT NULL,
    -- Visible name: "Crear lugar".
    tx_name        varchar NOT NULL,
    tx_description text,
    tx_status      public.activatable_status NOT NULL DEFAULT 'ACTIVE',
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL,
    dt_deleted_at  timestamp
);

CREATE UNIQUE INDEX ux_tb_permission_code_active
    ON seg.tb_permission (tx_code)
    WHERE dt_deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Bridges: no soft delete (rule 3) — a logically deleted row with a composite
-- key would block re-inserting the same pair forever.
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_user_role (
    co_user        uuid NOT NULL,
    co_role        uuid NOT NULL,
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL,
    CONSTRAINT pk_tb_user_role PRIMARY KEY (co_user, co_role)
);

CREATE INDEX ix_tb_user_role_role ON seg.tb_user_role (co_role);

CREATE TABLE seg.tb_role_permission (
    co_role        uuid NOT NULL,
    co_permission  uuid NOT NULL,
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL,
    CONSTRAINT pk_tb_role_permission PRIMARY KEY (co_role, co_permission)
);

CREATE INDEX ix_tb_role_permission_permission ON seg.tb_role_permission (co_permission);

-- ---------------------------------------------------------------------------
-- tb_security_event: append-only security audit log (rule 11). A row is never
-- updated nor individually deleted; retention is applied per monthly partition
-- (DROP PARTITION) or by batch.
--
-- Hard rules: NEVER store codes, tokens or passwords; NEVER store the plaintext
-- email of a failed attempt (user-enumeration oracle) — store an HMAC; NO FK to
-- tb_user, because the audit must survive user deletion.
--
-- tx_event is varchar with dot convention, NOT an enum: an event catalog grows
-- with every feature and a PostgreSQL enum forces a type migration each time
-- (values cannot be removed).
-- Catalog: auth.login.success / auth.login.failed / auth.logout /
-- auth.password.changed / auth.password.set / auth.password.reset_requested /
-- auth.password.reset_completed / auth.email.verify_requested /
-- auth.email.verified / auth.email.changed / auth.identity.linked /
-- auth.identity.unlinked / auth.token.refresh_reused / auth.sessions.revoked /
-- auth.account.locked / auth.account.unlocked / rbac.role.assigned /
-- rbac.role.revoked / rbac.permission.changed / admin.access.granted /
-- admin.access.denied / admin.user.suspended / admin.user.reactivated
--
-- Partitioning (implementation note; DBML cannot express it): RANGE by
-- dt_created_at, monthly partitions for 2026 created here, plus a DEFAULT
-- safety net. PostgreSQL requires the partition key in the PK, hence
-- PRIMARY KEY (co_security_event, dt_created_at). Creating a future monthly
-- partition requires the DEFAULT partition to hold no overlapping rows.
-- Retention: DROP TABLE seg.tb_security_event_YYYY_MM.
-- ---------------------------------------------------------------------------
CREATE TABLE seg.tb_security_event (
    co_security_event  uuid,
    -- Dot-convention catalog (see header). Never secrets, codes or tokens here.
    tx_event           varchar NOT NULL,
    -- SUBJECT of the event: who it happened to. NULL when a failed attempt does
    -- not resolve to a user. No FK by design (see header).
    co_user            uuid,
    -- WHO did it when it was not the subject: a backoffice admin. NULL = the
    -- user themself or the system.
    co_actor_user      uuid,
    -- HMAC-SHA256 of the attempted email, with a server secret. Distinguishes a
    -- mass attack from a targeted one without storing the plaintext email.
    tx_email_hash      varchar,
    -- Origin IP. Personal data: drives the retention policy.
    tx_ip              varchar,
    -- ISO 3166-1 alpha-2, derived from the IP. NOT GPS.
    tx_country         varchar(2),
    -- Derived from the IP. Approximate by definition (VPN, mobile data).
    tx_city            varchar,
    tx_user_agent      varchar,
    -- Additional event context. NEVER codes, tokens or passwords.
    js_metadata        jsonb,
    -- Only timestamp: an event never changes.
    dt_created_at      timestamp NOT NULL,
    CONSTRAINT pk_tb_security_event PRIMARY KEY (co_security_event, dt_created_at)
) PARTITION BY RANGE (dt_created_at);

CREATE TABLE seg.tb_security_event_2026_01 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE seg.tb_security_event_2026_02 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE seg.tb_security_event_2026_03 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE seg.tb_security_event_2026_04 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE seg.tb_security_event_2026_05 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE seg.tb_security_event_2026_06 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE seg.tb_security_event_2026_07 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE seg.tb_security_event_2026_08 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE seg.tb_security_event_2026_09 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE seg.tb_security_event_2026_10 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE seg.tb_security_event_2026_11 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE seg.tb_security_event_2026_12 PARTITION OF seg.tb_security_event
    FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');
CREATE TABLE seg.tb_security_event_default PARTITION OF seg.tb_security_event DEFAULT;

CREATE INDEX ix_tb_security_event_user
    ON seg.tb_security_event (co_user, dt_created_at);
CREATE INDEX ix_tb_security_event_event
    ON seg.tb_security_event (tx_event, dt_created_at);
CREATE INDEX ix_tb_security_event_ip
    ON seg.tb_security_event (tx_ip);
