-- ============================================================================
-- DATABASE COMMENTS — every table, every column, every shared enum.
-- Spec: database/diagram_base.txt (model rule 13).
--
-- WHY
--   Until this file the catalog had ZERO comments, so \d+ showed nothing. The
--   diagram already documents every rule; these comments compress that knowledge
--   into the catalog itself, where psql, GUIs and pg_catalog queries can read it.
--   Comments are part of the model, not decoration: verify_base_model.sql (S24)
--   enforces full coverage, so a new table or column without one fails the drift
--   check.
--
-- SCOPE
--   Every base table in seg and core, every column of those tables, and every
--   enum type in public. The monthly partitions of seg.tb_security_event are
--   covered by the parent's comments and are not commented individually.
--   Content rule: never secrets, tokens, real emails, passwords or internal
--   URLs — comments are visible to anyone with catalog access.
-- ============================================================================
\connect melia

-- ---------------------------------------------------------------------------
-- public enums — shared by seg and core, the only objects that live in public.
-- ---------------------------------------------------------------------------
COMMENT ON TYPE public.activatable_status IS
    'Two-state lifecycle (ACTIVE, INACTIVE) for catalogs and content that can be retired but not suspended.';
COMMENT ON TYPE public.suspendable_status IS
    'Three-state lifecycle (ACTIVE, INACTIVE, SUSPENDED) for entities an administrator can suspend: users and places.';
COMMENT ON TYPE public.review_status IS
    'Review lifecycle (ACTIVE, INACTIVE, MODERATED); its transitions are what the moderation funnel measures.';
COMMENT ON TYPE public.photo_kind IS
    'Kind of place photo: COVER (exactly one per place, application rule) or GALLERY.';
COMMENT ON TYPE public.identity_provider IS
    'Federated identity providers (GOOGLE, APPLE). The provider sub claim, never the email, resolves identity (rule 8).';
COMMENT ON TYPE public.user_token_purpose IS
    'Purpose of a one-time 6-digit code for an existing user: PASSWORD_RESET or EMAIL_CHANGE (rule 10).';
COMMENT ON TYPE public.change_operation IS
    'Operation that originated a history interval: INSERT, UPDATE or DELETE. A soft delete of the entity arrives as UPDATE; a physical delete writes a born-closed DELETE row.';

-- ===========================================================================
-- seg — identity and permissions
-- ===========================================================================

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_user IS
    'THE base user table: an app user and a backoffice user are the same entity. Capabilities come from RBAC (user -> role -> permission), never from a user-type column (rule 6).';
COMMENT ON COLUMN seg.tb_user.co_user IS
    'Primary key (uuid). Referenced cross-schema by core tables (rule 2).';
COMMENT ON COLUMN seg.tb_user.tx_name IS
    'Display name. NULL allowed: with Apple the name arrives ONLY on the first authorization.';
COMMENT ON COLUMN seg.tb_user.tx_email IS
    'Login email. NULL allowed: Apple may omit it or return a private relay. Uniqueness is partial (WHERE dt_deleted_at IS NULL), so a deleted user frees the address.';
COMMENT ON COLUMN seg.tb_user.tx_password_hash IS
    'Argon2id or bcrypt hash, never plaintext. NULL allowed: users who only signed in with Google/Apple have no password.';
COMMENT ON COLUMN seg.tb_user.dt_email_verified_at IS
    'When email ownership was proven (signup code or provider email_verified claim). With rule 9 it is always set; kept for traceability.';
COMMENT ON COLUMN seg.tb_user.nu_failed_attempts IS
    'Consecutive failed logins, reset on success. Without this counter the login is brute-forceable.';
COMMENT ON COLUMN seg.tb_user.dt_locked_until IS
    'Temporary lockout after failed attempts. NULL = not locked.';
COMMENT ON COLUMN seg.tb_user.tx_status IS
    'Lifecycle (suspendable_status): an administrator can SUSPEND the account.';
COMMENT ON COLUMN seg.tb_user.dt_created_at IS
    'Creation instant, stored in UTC: every timestamp in this database is UTC by database-level convention (rule 12).';
COMMENT ON COLUMN seg.tb_user.dt_updated_at IS
    'Last update instant (UTC). A write that changes only this column records no history (no-op suppression in the triggers).';
COMMENT ON COLUMN seg.tb_user.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted. Uniques on this table are PARTIAL (WHERE dt_deleted_at IS NULL), so deleting frees the unique values (rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_refresh_token IS
    'The only persisted session token (the access JWT is stateless and never stored). Rotates on every use; reuse of an already rotated token signals theft and revokes the whole family (rule 7).';
COMMENT ON COLUMN seg.tb_refresh_token.co_refresh_token IS
    'Primary key (uuid).';
COMMENT ON COLUMN seg.tb_refresh_token.co_user IS
    'Owner of the session (FK to seg.tb_user).';
COMMENT ON COLUMN seg.tb_refresh_token.tx_token_hash IS
    'SHA-256 of the refresh token, NEVER the raw token: if the database leaks, raw tokens are takeover of every active session. Unique.';
COMMENT ON COLUMN seg.tb_refresh_token.co_token_family IS
    'Groups one rotation chain: a reused (already rotated) token revokes the complete family.';
COMMENT ON COLUMN seg.tb_refresh_token.tx_device IS
    'User-facing device label, a name the user recognizes.';
COMMENT ON COLUMN seg.tb_refresh_token.tx_user_agent IS
    'Client user agent captured at login.';
COMMENT ON COLUMN seg.tb_refresh_token.tx_ip IS
    'IP of the session origin.';
COMMENT ON COLUMN seg.tb_refresh_token.dt_expires_at IS
    'Absolute expiry of the token (UTC).';
COMMENT ON COLUMN seg.tb_refresh_token.dt_revoked_at IS
    'Revocation instant; NULL = active. Expired and revoked are different states and are counted apart.';
COMMENT ON COLUMN seg.tb_refresh_token.dt_created_at IS
    'Creation instant (UTC). No soft delete on this table: operational state lives in columns (rule 3).';
COMMENT ON COLUMN seg.tb_refresh_token.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_user_token IS
    'One-time 6-digit codes for a user who ALREADY exists (password reset, email change). Code over link because the product is the mobile app (rule 10). Issuing a new code invalidates the previous ones of the same user and purpose (application rule).';
COMMENT ON COLUMN seg.tb_user_token.co_user_token IS
    'Primary key (uuid).';
COMMENT ON COLUMN seg.tb_user_token.co_user IS
    'User the code belongs to (FK to seg.tb_user).';
COMMENT ON COLUMN seg.tb_user_token.tx_purpose IS
    'What the code is for: PASSWORD_RESET or EMAIL_CHANGE.';
COMMENT ON COLUMN seg.tb_user_token.tx_code_hash IS
    'SHA-256 of the 6-digit code sent by email, NEVER the raw code. Unique.';
COMMENT ON COLUMN seg.tb_user_token.tx_target IS
    'Email the code was sent to: the NEW address on EMAIL_CHANGE, the current one on PASSWORD_RESET. Compared again at consumption; if the email changed meanwhile, the code is rejected.';
COMMENT ON COLUMN seg.tb_user_token.nu_attempts IS
    'Validation attempts, cut at 3-5. The control that actually protects: 6 digits are one million combinations (rule 10).';
COMMENT ON COLUMN seg.tb_user_token.dt_expires_at IS
    'Code expiry: short life, 5 to 10 minutes (UTC).';
COMMENT ON COLUMN seg.tb_user_token.dt_used_at IS
    'Single-use marker: NULL = unused, and reuse must fail.';
COMMENT ON COLUMN seg.tb_user_token.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN seg.tb_user_token.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_signup_request IS
    'Pending registration: email ownership is proven by code BEFORE any row exists in tb_user (rule 9), preventing email squatting and garbage users. No soft delete: it either becomes a user or expires and is deleted.';
COMMENT ON COLUMN seg.tb_signup_request.co_signup_request IS
    'Primary key (uuid).';
COMMENT ON COLUMN seg.tb_signup_request.tx_email IS
    'Email to verify. INMUTABLE during registration: if it changes, a new code must be issued for the new address. Unique.';
COMMENT ON COLUMN seg.tb_signup_request.tx_name IS
    'Name copied to tb_user upon verification.';
COMMENT ON COLUMN seg.tb_signup_request.tx_password_hash IS
    'Argon2id hash computed BEFORE storing; never plaintext, not even for an unverified registration.';
COMMENT ON COLUMN seg.tb_signup_request.tx_code_hash IS
    'SHA-256 of the 6-digit code sent by email, NEVER the raw code.';
COMMENT ON COLUMN seg.tb_signup_request.nu_attempts IS
    'Validation attempts, cut at 3-5; without this counter a 6-digit code falls in minutes.';
COMMENT ON COLUMN seg.tb_signup_request.dt_expires_at IS
    'Code expiry: 10 minutes at most (UTC).';
COMMENT ON COLUMN seg.tb_signup_request.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN seg.tb_signup_request.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_user_identity IS
    'One row per federated provider linked to the account (a user may have Google AND Apple). Login resolves ALWAYS by (provider, sub), never by email (rule 8).';
COMMENT ON COLUMN seg.tb_user_identity.co_user_identity IS
    'Primary key (uuid).';
COMMENT ON COLUMN seg.tb_user_identity.co_user IS
    'Account the provider is linked to (FK to seg.tb_user).';
COMMENT ON COLUMN seg.tb_user_identity.tx_provider IS
    'Provider: GOOGLE or APPLE.';
COMMENT ON COLUMN seg.tb_user_identity.tx_provider_user_id IS
    'The provider sub claim: THE federated-login resolution key, never the email.';
COMMENT ON COLUMN seg.tb_user_identity.tx_email IS
    'Email returned by the provider (may be an Apple relay). Informational only; identity is never resolved through it.';
COMMENT ON COLUMN seg.tb_user_identity.dt_linked_at IS
    'When the provider was linked to the account (UTC).';
COMMENT ON COLUMN seg.tb_user_identity.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN seg.tb_user_identity.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN seg.tb_user_identity.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted. The (provider, sub) unique is PARTIAL, so unlinking frees the identity (rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_role IS
    'Role: an assignable group of permissions (SUPERADMIN, ADMIN, EDITOR). Roles are not user types; access comes from an active role granting the permission (rule 6).';
COMMENT ON COLUMN seg.tb_role.co_role IS
    'Primary key (uuid).';
COMMENT ON COLUMN seg.tb_role.tx_name IS
    'Role name, e.g. SUPERADMIN, ADMIN, EDITOR. Partially unique: a deleted role frees the name (rule 4).';
COMMENT ON COLUMN seg.tb_role.tx_description IS
    'What the role is for.';
COMMENT ON COLUMN seg.tb_role.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN seg.tb_role.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN seg.tb_role.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN seg.tb_role.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (partial uniques apply, rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_permission IS
    'Permission: one concrete action of the backoffice. A catalog seeded by the application, not something created from the UI.';
COMMENT ON COLUMN seg.tb_permission.co_permission IS
    'Primary key (uuid).';
COMMENT ON COLUMN seg.tb_permission.tx_code IS
    'Stable key the code evaluates: place.create, review.moderate, admin.access. Partially unique (rule 4).';
COMMENT ON COLUMN seg.tb_permission.tx_module IS
    'UI grouping for the backoffice: PLACES, MENUS, REVIEWS, ROUTES, USERS, ROLES.';
COMMENT ON COLUMN seg.tb_permission.tx_name IS
    'Visible name.';
COMMENT ON COLUMN seg.tb_permission.tx_description IS
    'What the permission allows.';
COMMENT ON COLUMN seg.tb_permission.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN seg.tb_permission.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN seg.tb_permission.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN seg.tb_permission.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (partial uniques apply, rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_user_role IS
    'Bridge user <-> role. Physical delete on purpose (rule 3): with a composite key, a logically deleted row would block re-assigning the same pair forever.';
COMMENT ON COLUMN seg.tb_user_role.co_user IS
    'User (FK to seg.tb_user). Part of the composite primary key.';
COMMENT ON COLUMN seg.tb_user_role.co_role IS
    'Role (FK to seg.tb_role). Part of the composite primary key.';
COMMENT ON COLUMN seg.tb_user_role.dt_created_at IS
    'Assignment instant (UTC).';
COMMENT ON COLUMN seg.tb_user_role.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_role_permission IS
    'Bridge role <-> permission. Physical delete on purpose (rule 3).';
COMMENT ON COLUMN seg.tb_role_permission.co_role IS
    'Role (FK to seg.tb_role). Part of the composite primary key.';
COMMENT ON COLUMN seg.tb_role_permission.co_permission IS
    'Permission (FK to seg.tb_permission). Part of the composite primary key.';
COMMENT ON COLUMN seg.tb_role_permission.dt_created_at IS
    'Grant instant (UTC).';
COMMENT ON COLUMN seg.tb_role_permission.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_security_event IS
    'Append-only security audit log (rule 11), range-partitioned by month on dt_created_at; retention drops whole partitions. No FK to tb_user: the audit must survive user deletion. NEVER stores codes, tokens, passwords, or the plaintext email of a failed attempt (user-enumeration oracle). Monthly partitions hold the rows; comments live on the parent.';
COMMENT ON COLUMN seg.tb_security_event.co_security_event IS
    'Event id (uuid). The primary key also includes dt_created_at because PostgreSQL requires the partition key in the PK.';
COMMENT ON COLUMN seg.tb_security_event.tx_event IS
    'Dot-convention event catalog (auth.login.success, rbac.role.assigned, admin.access.denied, ...). varchar, not an enum: the catalog grows with every feature and enum values cannot be removed.';
COMMENT ON COLUMN seg.tb_security_event.co_user IS
    'SUBJECT of the event: who it happened to. NULL when a failed attempt resolves to no user. No FK by design.';
COMMENT ON COLUMN seg.tb_security_event.co_actor_user IS
    'WHO did it when it was not the subject (a backoffice admin). NULL = the subject themself or the system.';
COMMENT ON COLUMN seg.tb_security_event.tx_email_hash IS
    'HMAC-SHA256 of the attempted email with a server secret: distinguishes a mass attack from a targeted one without storing the plaintext email.';
COMMENT ON COLUMN seg.tb_security_event.tx_ip IS
    'Origin IP. Personal data: drives the retention policy.';
COMMENT ON COLUMN seg.tb_security_event.tx_country IS
    'ISO 3166-1 alpha-2, derived from the IP. NOT GPS.';
COMMENT ON COLUMN seg.tb_security_event.tx_city IS
    'City derived from the IP; approximate by definition (VPN, mobile data).';
COMMENT ON COLUMN seg.tb_security_event.tx_user_agent IS
    'Client user agent.';
COMMENT ON COLUMN seg.tb_security_event.js_metadata IS
    'Additional event context. NEVER codes, tokens or passwords.';
COMMENT ON COLUMN seg.tb_security_event.dt_created_at IS
    'Only timestamp: an event never changes (UTC). Partition key.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_schema_migration IS
    'Migration ledger: one row per applied database/init file. The service is the only applier, and this ledger is what makes every file run exactly once.';
COMMENT ON COLUMN seg.tb_schema_migration.tx_name IS
    'Applied file name, e.g. 03-seg-tables.sql. Unique: the applier conflicts on it to keep re-runs idempotent.';
COMMENT ON COLUMN seg.tb_schema_migration.dt_applied_at IS
    'When the file was applied (timestamptz, UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_user_role_history IS
    'Point-in-time history of seg.tb_user_role, trigger-written and append-only. The bridge is physically deleted (rule 3), so this is what makes the question who-held-role-X-on-date-D answerable.';
COMMENT ON COLUMN seg.tb_user_role_history.co_user_role_history IS
    'Surrogate id of the history row (uuid PK).';
COMMENT ON COLUMN seg.tb_user_role_history.co_user IS
    'User of the recorded assignment. No FK: history must survive the physically deleted bridge row.';
COMMENT ON COLUMN seg.tb_user_role_history.co_role IS
    'Role of the recorded assignment. No FK, same reason.';
COMMENT ON COLUMN seg.tb_user_role_history.tx_operation IS
    'INSERT or DELETE (change_operation): a bridge row is never updated in place.';
COMMENT ON COLUMN seg.tb_user_role_history.co_actor_user IS
    'Who made the change, announced per transaction via set_config(''app.current_user_id'', ...); NULL when not announced. No FK: the audit survives user deletion.';
COMMENT ON COLUMN seg.tb_user_role_history.dt_valid_from IS
    'Instant the recorded state became true (UTC).';
COMMENT ON COLUMN seg.tb_user_role_history.dt_valid_to IS
    'Instant it stopped being true; NULL = still true. Exactly one open interval per pair (partial unique); a closed interval is immutable.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE seg.tb_role_permission_history IS
    'Point-in-time history of seg.tb_role_permission, trigger-written and append-only: permission drift per role, a security question as much as an analytical one.';
COMMENT ON COLUMN seg.tb_role_permission_history.co_role_permission_history IS
    'Surrogate id of the history row (uuid PK).';
COMMENT ON COLUMN seg.tb_role_permission_history.co_role IS
    'Role of the recorded grant. No FK: history must survive the physically deleted bridge row.';
COMMENT ON COLUMN seg.tb_role_permission_history.co_permission IS
    'Permission of the recorded grant. No FK, same reason.';
COMMENT ON COLUMN seg.tb_role_permission_history.tx_operation IS
    'INSERT or DELETE (change_operation): a bridge row is never updated in place.';
COMMENT ON COLUMN seg.tb_role_permission_history.co_actor_user IS
    'Who made the change, announced via set_config(''app.current_user_id'', ...); NULL when not announced. No FK by design.';
COMMENT ON COLUMN seg.tb_role_permission_history.dt_valid_from IS
    'Instant the recorded state became true (UTC).';
COMMENT ON COLUMN seg.tb_role_permission_history.dt_valid_to IS
    'Instant it stopped being true; NULL = still true. Exactly one open interval per pair (partial unique); a closed interval is immutable.';

-- ===========================================================================
-- core — domain
-- ===========================================================================

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_place_category IS
    'Governed catalog of place categories (hotel, arepera, parque), managed from the backoffice. A table, not a PostgreSQL enum: growing it must not force a type migration.';
COMMENT ON COLUMN core.tb_place_category.co_place_category IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_place_category.tx_code IS
    'Stable key the code compares against: hotel, arepera, parque. Partially unique: a retired category frees its code (rule 4).';
COMMENT ON COLUMN core.tb_place_category.tx_name IS
    'Display name: Hotel, Arepera, Parque.';
COMMENT ON COLUMN core.tb_place_category.tx_description IS
    'What the category covers.';
COMMENT ON COLUMN core.tb_place_category.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN core.tb_place_category.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_place_category.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN core.tb_place_category.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (partial uniques apply, rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_currency IS
    'ISO 4217 currency catalog. A price without a currency is a number without meaning, and nu_decimals is what formats it (rule 12).';
COMMENT ON COLUMN core.tb_currency.co_currency IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_currency.tx_code IS
    'ISO 4217 alphabetic code (USD, VES, COP); shape enforced by CHECK. Partially unique (rule 4).';
COMMENT ON COLUMN core.tb_currency.tx_name IS
    'Display name of the currency.';
COMMENT ON COLUMN core.tb_currency.nu_decimals IS
    'Decimal digits when formatting a price in this currency (CHECK 0..4).';
COMMENT ON COLUMN core.tb_currency.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN core.tb_currency.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_currency.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN core.tb_currency.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (partial uniques apply, rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_place IS
    'A venue (e.g. MELIA). The category is the governed co_place_category; the legacy free-text tx_category was dropped in migration 12 because two columns claiming the category broke history parity.';
COMMENT ON COLUMN core.tb_place.co_place IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_place.co_owner_user IS
    'Owner user (cross-schema FK to seg.tb_user). NULL on backoffice-curated places.';
COMMENT ON COLUMN core.tb_place.tx_name IS
    'Real/legal name.';
COMMENT ON COLUMN core.tb_place.tx_title IS
    'Commercial name shown in the app.';
COMMENT ON COLUMN core.tb_place.tx_description IS
    'Free description.';
COMMENT ON COLUMN core.tb_place.nu_latitude IS
    'Latitude (CHECK -90..90).';
COMMENT ON COLUMN core.tb_place.nu_longitude IS
    'Longitude (CHECK -180..180).';
COMMENT ON COLUMN core.tb_place.tx_address IS
    'Street address.';
COMMENT ON COLUMN core.tb_place.js_social_media IS
    'Social links as JSON, e.g. {"instagram": "...", "web": "..."}.';
COMMENT ON COLUMN core.tb_place.tx_status IS
    'Lifecycle (suspendable_status): an administrator can SUSPEND the place.';
COMMENT ON COLUMN core.tb_place.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_place.dt_updated_at IS
    'Last update instant (UTC). A write that changes only this column records no history (no-op suppression).';
COMMENT ON COLUMN core.tb_place.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (partial uniques apply, rule 4).';
COMMENT ON COLUMN core.tb_place.co_place_category IS
    'Governed category (FK to core.tb_place_category). No default: every write declares it explicitly. Replaced the free-text tx_category, dropped in migration 12.';
COMMENT ON COLUMN core.tb_place.tx_country IS
    'ISO 3166-1 alpha-2 country, for grouping and reporting; shape enforced by CHECK.';
COMMENT ON COLUMN core.tb_place.tx_timezone IS
    'IANA zone name (America/Caracas): what makes dt_open_time/dt_close_time of the schedules interpretable. The CHECK enforces only the shape; pg_timezone_names validation happens at the application edge.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_place_schedule IS
    'Per-day opening hours of a place; replaces a single open/close pair and supports venues that cross midnight. Physical delete (rule 3). Interpretable only through core.tb_place.tx_timezone.';
COMMENT ON COLUMN core.tb_place_schedule.co_schedule IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_place_schedule.co_place IS
    'Place the schedule belongs to (FK).';
COMMENT ON COLUMN core.tb_place_schedule.nu_day_of_week IS
    'ISO-8601 day: 1=Monday ... 7=Sunday (CHECK 1..7).';
COMMENT ON COLUMN core.tb_place_schedule.dt_open_time IS
    'Opening wall-clock time, interpreted in the place tx_timezone (IANA).';
COMMENT ON COLUMN core.tb_place_schedule.dt_close_time IS
    'Closing wall-clock time in the place tx_timezone. If <= dt_open_time, closing is on the NEXT day (crosses midnight).';
COMMENT ON COLUMN core.tb_place_schedule.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_place_schedule.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_place_photo IS
    'Photos of a place. Exactly one COVER per place: an application rule, not a constraint.';
COMMENT ON COLUMN core.tb_place_photo.co_photo IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_place_photo.co_place IS
    'Place the photo belongs to (FK).';
COMMENT ON COLUMN core.tb_place_photo.tx_url IS
    'Image URL in S3/Cloud storage.';
COMMENT ON COLUMN core.tb_place_photo.tx_type IS
    'COVER or GALLERY (photo_kind).';
COMMENT ON COLUMN core.tb_place_photo.nu_order IS
    'Display order within the place gallery.';
COMMENT ON COLUMN core.tb_place_photo.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN core.tb_place_photo.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_place_photo.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN core.tb_place_photo.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_menu IS
    'Intermediate level between place and dish: a place may have several menus (Desayuno, Almuerzo, Cena, Vinos).';
COMMENT ON COLUMN core.tb_menu.co_menu IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_menu.co_place IS
    'Place the menu belongs to (FK).';
COMMENT ON COLUMN core.tb_menu.tx_name IS
    'Menu name, e.g. Desayuno, Almuerzo, Cena.';
COMMENT ON COLUMN core.tb_menu.tx_description IS
    'Free description.';
COMMENT ON COLUMN core.tb_menu.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN core.tb_menu.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_menu.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN core.tb_menu.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_menu_item IS
    'A dish or product; hangs from the menu, not from the place.';
COMMENT ON COLUMN core.tb_menu_item.co_menu_item IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_menu_item.co_menu IS
    'Menu the item belongs to (FK).';
COMMENT ON COLUMN core.tb_menu_item.tx_name IS
    'Item name.';
COMMENT ON COLUMN core.tb_menu_item.tx_description IS
    'Free description.';
COMMENT ON COLUMN core.tb_menu_item.nu_price IS
    'Price amount (CHECK >= 0). Meaningless without co_currency; nu_decimals of the currency formats it.';
COMMENT ON COLUMN core.tb_menu_item.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN core.tb_menu_item.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_menu_item.dt_updated_at IS
    'Last update instant (UTC). A write that changes only this column records no history (no-op suppression).';
COMMENT ON COLUMN core.tb_menu_item.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (rule 4).';
COMMENT ON COLUMN core.tb_menu_item.co_currency IS
    'Currency of nu_price (FK to core.tb_currency). No default: every write declares it explicitly.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_review IS
    'A user review of a place. One review per user and place, enforced by a partial unique that ignores soft-deleted rows, so a moderated or deleted review does not block reviewing again (rule 4).';
COMMENT ON COLUMN core.tb_review.co_review IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_review.co_user IS
    'Author (cross-schema FK to seg.tb_user). Immutable in practice: one review per user and place.';
COMMENT ON COLUMN core.tb_review.co_place IS
    'Reviewed place (FK). Immutable in practice.';
COMMENT ON COLUMN core.tb_review.nu_rating IS
    'Rating 1..5 (CHECK).';
COMMENT ON COLUMN core.tb_review.tx_comment IS
    'Review text.';
COMMENT ON COLUMN core.tb_review.tx_status IS
    'Lifecycle (review_status): ACTIVE, INACTIVE or MODERATED.';
COMMENT ON COLUMN core.tb_review.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_review.dt_updated_at IS
    'Last update instant (UTC). A write that changes only this column records no history (no-op suppression).';
COMMENT ON COLUMN core.tb_review.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (partial uniques apply, rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_route IS
    'A user-created route: an ordered visit list of places via tb_route_place.';
COMMENT ON COLUMN core.tb_route.co_route IS
    'Primary key (uuid).';
COMMENT ON COLUMN core.tb_route.co_user IS
    'Creator user (cross-schema FK to seg.tb_user).';
COMMENT ON COLUMN core.tb_route.tx_name IS
    'Route name.';
COMMENT ON COLUMN core.tb_route.tx_description IS
    'Free description.';
COMMENT ON COLUMN core.tb_route.tx_status IS
    'Lifecycle (activatable_status).';
COMMENT ON COLUMN core.tb_route.dt_created_at IS
    'Creation instant (UTC).';
COMMENT ON COLUMN core.tb_route.dt_updated_at IS
    'Last update instant (UTC).';
COMMENT ON COLUMN core.tb_route.dt_deleted_at IS
    'Soft-delete marker: non-NULL means deleted (rule 4).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_route_place IS
    'Bridge route <-> place with visit order. Physical delete on purpose (rule 3): a logically deleted row would block re-adding the place to the same route.';
COMMENT ON COLUMN core.tb_route_place.co_route IS
    'Route (FK). Part of the composite primary key.';
COMMENT ON COLUMN core.tb_route_place.co_place IS
    'Place (FK). Part of the composite primary key.';
COMMENT ON COLUMN core.tb_route_place.nu_order IS
    'Visit order (1, 2, 3...).';
COMMENT ON COLUMN core.tb_route_place.dt_created_at IS
    'Addition instant (UTC).';
COMMENT ON COLUMN core.tb_route_place.dt_updated_at IS
    'Last update instant (UTC).';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_place_history IS
    'Point-in-time history of tb_place, trigger-written and append-only: each row is a validity interval of the observable state. No FK and no business CHECKs: history must survive catalog changes and must never reject its own past.';
COMMENT ON COLUMN core.tb_place_history.co_place_history IS
    'Surrogate id of the history row (uuid PK).';
COMMENT ON COLUMN core.tb_place_history.co_place IS
    'Place this interval describes. No FK: history must survive the entity.';
COMMENT ON COLUMN core.tb_place_history.co_owner_user IS
    'Owner during this interval; NULL for curated places. No FK: the audit survives user deletion.';
COMMENT ON COLUMN core.tb_place_history.tx_name IS
    'Legal name during this interval.';
COMMENT ON COLUMN core.tb_place_history.tx_title IS
    'Commercial name during this interval.';
COMMENT ON COLUMN core.tb_place_history.tx_description IS
    'Description during this interval.';
COMMENT ON COLUMN core.tb_place_history.nu_latitude IS
    'Latitude during this interval. No range CHECK: history never rejects its own past.';
COMMENT ON COLUMN core.tb_place_history.nu_longitude IS
    'Longitude during this interval. No range CHECK, same reason.';
COMMENT ON COLUMN core.tb_place_history.tx_address IS
    'Street address during this interval.';
COMMENT ON COLUMN core.tb_place_history.js_social_media IS
    'Social links during this interval.';
COMMENT ON COLUMN core.tb_place_history.tx_status IS
    'Status during this interval.';
COMMENT ON COLUMN core.tb_place_history.dt_deleted_at IS
    'Soft-delete state during this interval; a logical delete arrives as an UPDATE on the source.';
COMMENT ON COLUMN core.tb_place_history.tx_operation IS
    'Operation that originated the interval: INSERT, UPDATE or DELETE (change_operation).';
COMMENT ON COLUMN core.tb_place_history.co_actor_user IS
    'Who made the change, announced via set_config(''app.current_user_id'', ...); NULL when not announced. No FK by design.';
COMMENT ON COLUMN core.tb_place_history.dt_valid_from IS
    'Instant the recorded state became true (UTC).';
COMMENT ON COLUMN core.tb_place_history.dt_valid_to IS
    'Instant it stopped being true; NULL = still true. Exactly one open interval per place (partial unique); a closed interval is immutable.';
COMMENT ON COLUMN core.tb_place_history.co_place_category IS
    'Category during this interval. No FK: history must survive catalog changes; a retired category must never break the past. The free-text tx_category never existed here: the governed FK is the only category worth keeping.';
COMMENT ON COLUMN core.tb_place_history.tx_country IS
    'Country during this interval (ISO 3166-1 alpha-2).';
COMMENT ON COLUMN core.tb_place_history.tx_timezone IS
    'IANA timezone during this interval: what made the schedules of that time interpretable.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_menu_item_history IS
    'Point-in-time history of tb_menu_item, trigger-written and append-only. The case that motivated the feature: the price a dish had when a review was written.';
COMMENT ON COLUMN core.tb_menu_item_history.co_menu_item_history IS
    'Surrogate id of the history row (uuid PK).';
COMMENT ON COLUMN core.tb_menu_item_history.co_menu_item IS
    'Menu item this interval describes. No FK: history must survive the entity.';
COMMENT ON COLUMN core.tb_menu_item_history.co_menu IS
    'Owning menu during this interval: an item may move between menus, so it is part of the state.';
COMMENT ON COLUMN core.tb_menu_item_history.tx_name IS
    'Item name during this interval.';
COMMENT ON COLUMN core.tb_menu_item_history.tx_description IS
    'Description during this interval.';
COMMENT ON COLUMN core.tb_menu_item_history.nu_price IS
    'Price during this interval; interpretable only together with co_currency. No CHECK: history never rejects its own past.';
COMMENT ON COLUMN core.tb_menu_item_history.tx_status IS
    'Status during this interval.';
COMMENT ON COLUMN core.tb_menu_item_history.dt_deleted_at IS
    'Soft-delete state during this interval; a logical delete arrives as an UPDATE on the source.';
COMMENT ON COLUMN core.tb_menu_item_history.tx_operation IS
    'Operation that originated the interval: INSERT, UPDATE or DELETE (change_operation).';
COMMENT ON COLUMN core.tb_menu_item_history.co_actor_user IS
    'Who made the change, announced via set_config(''app.current_user_id'', ...); NULL when not announced. No FK by design.';
COMMENT ON COLUMN core.tb_menu_item_history.dt_valid_from IS
    'Instant the recorded state became true (UTC).';
COMMENT ON COLUMN core.tb_menu_item_history.dt_valid_to IS
    'Instant it stopped being true; NULL = still true. Exactly one open interval per item (partial unique); a closed interval is immutable.';
COMMENT ON COLUMN core.tb_menu_item_history.co_currency IS
    'Currency of the price during this interval. No FK: a price history without its currency is not interpretable, and history must survive catalog changes.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_place_schedule_history IS
    'Point-in-time history of tb_place_schedule, trigger-written and append-only. The source is physically deleted (rule 3), so this table is the only trace a removed schedule leaves; the payload is complete on purpose.';
COMMENT ON COLUMN core.tb_place_schedule_history.co_schedule_history IS
    'Surrogate id of the history row (uuid PK).';
COMMENT ON COLUMN core.tb_place_schedule_history.co_schedule IS
    'Schedule this interval describes. No FK: the source row is physically deleted.';
COMMENT ON COLUMN core.tb_place_schedule_history.co_place IS
    'Place the schedule belonged to; leads the validity index because the question is the place schedule on a date.';
COMMENT ON COLUMN core.tb_place_schedule_history.nu_day_of_week IS
    'ISO-8601 day during this interval.';
COMMENT ON COLUMN core.tb_place_schedule_history.dt_open_time IS
    'Opening wall-clock time during this interval, interpreted in the place timezone of that time.';
COMMENT ON COLUMN core.tb_place_schedule_history.dt_close_time IS
    'Closing wall-clock time during this interval; <= dt_open_time means it crossed midnight.';
COMMENT ON COLUMN core.tb_place_schedule_history.tx_operation IS
    'Operation that originated the interval: INSERT, UPDATE or DELETE (change_operation). A physical delete writes a born-closed DELETE row (dt_valid_from = dt_valid_to).';
COMMENT ON COLUMN core.tb_place_schedule_history.co_actor_user IS
    'Who made the change, announced via set_config(''app.current_user_id'', ...); NULL when not announced. No FK by design.';
COMMENT ON COLUMN core.tb_place_schedule_history.dt_valid_from IS
    'Instant the recorded state became true (UTC).';
COMMENT ON COLUMN core.tb_place_schedule_history.dt_valid_to IS
    'Instant it stopped being true; NULL = still true. Exactly one open interval per schedule (partial unique); a closed interval is immutable.';

-- ---------------------------------------------------------------------------
COMMENT ON TABLE core.tb_review_history IS
    'Point-in-time history of tb_review, trigger-written and append-only: makes the moderation funnel measurable (time spent ACTIVE before MODERATED, rating drift). co_user and co_place are immutable, so they are read from the entity and not duplicated here.';
COMMENT ON COLUMN core.tb_review_history.co_review_history IS
    'Surrogate id of the history row (uuid PK).';
COMMENT ON COLUMN core.tb_review_history.co_review IS
    'Review this interval describes. No FK: history must survive the entity.';
COMMENT ON COLUMN core.tb_review_history.nu_rating IS
    'Rating during this interval. No CHECK: history never rejects its own past.';
COMMENT ON COLUMN core.tb_review_history.tx_comment IS
    'Review text during this interval.';
COMMENT ON COLUMN core.tb_review_history.tx_status IS
    'Status during this interval (review_status).';
COMMENT ON COLUMN core.tb_review_history.dt_deleted_at IS
    'Soft-delete state during this interval; a logical delete arrives as an UPDATE on the source.';
COMMENT ON COLUMN core.tb_review_history.tx_operation IS
    'Operation that originated the interval: INSERT, UPDATE or DELETE (change_operation).';
COMMENT ON COLUMN core.tb_review_history.co_actor_user IS
    'Who made the change, announced via set_config(''app.current_user_id'', ...); NULL when not announced. No FK by design.';
COMMENT ON COLUMN core.tb_review_history.dt_valid_from IS
    'Instant the recorded state became true (UTC).';
COMMENT ON COLUMN core.tb_review_history.dt_valid_to IS
    'Instant it stopped being true; NULL = still true. Exactly one open interval per review (partial unique); a closed interval is immutable.';
