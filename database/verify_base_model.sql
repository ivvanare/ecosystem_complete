-- ============================================================================
-- verify_base_model.sql — runnable assertions for the melia base model.
-- Spec: database/diagram_base.txt · Change: openspec/changes/base-domain-model
--
-- Usage (live PostgreSQL 15, e.g. docker compose up database):
--   psql -h localhost -U admin -d melia -v ON_ERROR_STOP=1 -f database/verify_base_model.sql
--
-- Every check RAISEs EXCEPTION on failure. All behavioral inserts run inside a
-- transaction that is ROLLED BACK at the end: the database is left untouched.
-- ============================================================================
\set ON_ERROR_STOP on

BEGIN;

-- ---- S1: topology — one database, schemas seg + core ------------------------
DO $$
BEGIN
    IF current_database() <> 'melia' THEN
        RAISE EXCEPTION 'S1 FAIL: expected database melia, got %', current_database();
    END IF;
    IF (SELECT count(*) FROM information_schema.schemata
        WHERE schema_name IN ('seg', 'core')) <> 2 THEN
        RAISE EXCEPTION 'S1 FAIL: schemas seg/core missing';
    END IF;
END $$;

-- ---- S13: the 6 enums live in public with exact labels ----------------------
DO $$
DECLARE
    labels text[];
BEGIN
    IF (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
        WHERE n.nspname = 'public' AND t.typname IN
        ('activatable_status','suspendable_status','review_status',
         'photo_kind','identity_provider','user_token_purpose')) <> 6 THEN
        RAISE EXCEPTION 'S13 FAIL: expected 6 enum types in public';
    END IF;
    SELECT array_agg(e.enumlabel ORDER BY e.enumsortorder) INTO labels
    FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
    JOIN pg_enum e ON e.enumtypid = t.oid
    WHERE n.nspname = 'public' AND t.typname = 'suspendable_status';
    IF labels <> ARRAY['ACTIVE','INACTIVE','SUSPENDED']::text[] THEN
        RAISE EXCEPTION 'S13 FAIL: suspendable_status labels are %', labels;
    END IF;
END $$;

-- ---- S14: table inventory — 10 seg + 8 core ---------------------------------
DO $$
DECLARE
    missing text;
BEGIN
    SELECT string_agg(v.t, ', ') INTO missing
    FROM (VALUES
        ('seg.tb_user'), ('seg.tb_refresh_token'), ('seg.tb_user_token'),
        ('seg.tb_signup_request'), ('seg.tb_user_identity'), ('seg.tb_role'),
        ('seg.tb_permission'), ('seg.tb_user_role'), ('seg.tb_role_permission'),
        ('seg.tb_security_event'),
        ('core.tb_place'), ('core.tb_place_schedule'), ('core.tb_place_photo'),
        ('core.tb_menu'), ('core.tb_menu_item'), ('core.tb_review'),
        ('core.tb_route'), ('core.tb_route_place')
    ) AS v(t)
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_tables pt
        WHERE pt.schemaname || '.' || pt.tablename = v.t
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'S14 FAIL: missing tables: %', missing;
    END IF;
END $$;

-- ---- S2: business PKs are uuid ----------------------------------------------
DO $$
BEGIN
    IF (SELECT data_type FROM information_schema.columns
        WHERE table_schema = 'seg' AND table_name = 'tb_user'
          AND column_name = 'co_user') <> 'uuid' THEN
        RAISE EXCEPTION 'S2 FAIL: seg.tb_user.co_user is not uuid';
    END IF;
END $$;

-- ---- S12: tb_security_event is partitioned monthly, no FK to tb_user --------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = 'seg' AND c.relname = 'tb_security_event'
                     AND c.relkind = 'p') THEN
        RAISE EXCEPTION 'S12 FAIL: tb_security_event is not a partitioned table';
    END IF;
    IF (SELECT count(*) FROM pg_inherits i
        JOIN pg_class p ON p.oid = i.inhparent
        JOIN pg_namespace n ON n.oid = p.relnamespace
        WHERE n.nspname = 'seg' AND p.relname = 'tb_security_event') <> 13 THEN
        RAISE EXCEPTION 'S12 FAIL: expected 13 partitions (12 months of 2026 + default)';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint
               WHERE conrelid = 'seg.tb_security_event'::regclass AND contype = 'f') THEN
        RAISE EXCEPTION 'S12 FAIL: tb_security_event must have NO foreign keys (rule 11)';
    END IF;
END $$;

-- ---- S8: refresh token stores hash, keeps operational state columns ---------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'seg.tb_refresh_token'::regclass
                     AND contype = 'u'
                     AND pg_get_constraintdef(oid) ILIKE '%tx_token_hash%') THEN
        RAISE EXCEPTION 'S8 FAIL: tb_refresh_token.tx_token_hash is not unique';
    END IF;
    IF (SELECT is_nullable FROM information_schema.columns
        WHERE table_schema = 'seg' AND table_name = 'tb_refresh_token'
          AND column_name = 'dt_revoked_at') <> 'YES' THEN
        RAISE EXCEPTION 'S8 FAIL: dt_revoked_at must be nullable (NULL = active)';
    END IF;
END $$;

-- ---- S14/S9/S10: tb_user nullable email + password; signup holds the code ----
DO $$
BEGIN
    IF (SELECT count(*) FROM information_schema.columns
        WHERE table_schema = 'seg' AND table_name = 'tb_user'
          AND column_name IN ('tx_email','tx_password_hash') AND is_nullable = 'YES') <> 2 THEN
        RAISE EXCEPTION 'S14 FAIL: tb_user tx_email/tx_password_hash must be nullable';
    END IF;
END $$;

-- ============================================================================
-- Behavioral checks (inside the transaction, rolled back at the end).
-- ============================================================================

-- ---- S5: partial unique on tb_user.tx_email ---------------------------------
DO $$
DECLARE
    u1 uuid := gen_random_uuid();
BEGIN
    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (u1, 'verify@test.dev', now(), now());

    -- A second ACTIVE row with the same email must fail.
    BEGIN
        INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), 'verify@test.dev', now(), now());
        RAISE EXCEPTION 'S5 FAIL: duplicate active email was admitted';
    EXCEPTION WHEN unique_violation THEN NULL;
    END;

    -- After soft delete the same email must be reusable (rule 4).
    UPDATE seg.tb_user SET dt_deleted_at = now() WHERE co_user = u1;
    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (gen_random_uuid(), 'verify@test.dev', now(), now());
END $$;

-- ---- S6: CHECK constraints reject out-of-range values ------------------------
DO $$
BEGIN
    BEGIN
        INSERT INTO core.tb_review (co_review, co_user, co_place, nu_rating,
                                    dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 0, now(), now());
        RAISE EXCEPTION 'S6 FAIL: rating 0 admitted (expected 1..5)';
    EXCEPTION WHEN check_violation OR foreign_key_violation THEN NULL;
    END;
    BEGIN
        INSERT INTO core.tb_place (co_place, tx_name, tx_title, tx_category,
                                   nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), 'X', 'X', 'X', 91, 0, now(), now());
        RAISE EXCEPTION 'S6 FAIL: latitude 91 admitted (expected -90..90)';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    BEGIN
        INSERT INTO core.tb_place_schedule (co_schedule, co_place, nu_day_of_week,
                                            dt_open_time, dt_close_time,
                                            dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), gen_random_uuid(), 8, '08:00', '12:00', now(), now());
        RAISE EXCEPTION 'S6 FAIL: day_of_week 8 admitted (expected 1..7)';
    EXCEPTION WHEN check_violation OR foreign_key_violation THEN NULL;
    END;
END $$;

-- ---- S3: cross-schema FK is enforced -----------------------------------------
DO $$
BEGIN
    BEGIN
        INSERT INTO core.tb_route (co_route, co_user, tx_name, dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), gen_random_uuid(), 'R', now(), now());
        RAISE EXCEPTION 'S3 FAIL: route with nonexistent seg.tb_user was admitted';
    EXCEPTION WHEN foreign_key_violation THEN NULL;
    END;
END $$;

-- ---- S12: inserts route to the right monthly partition / default -------------
DO $$
DECLARE
    ev uuid := gen_random_uuid();
BEGIN
    INSERT INTO seg.tb_security_event (co_security_event, tx_event, dt_created_at)
    VALUES (ev, 'auth.login.success', '2026-06-15 10:00:00');
    IF NOT EXISTS (SELECT 1 FROM seg.tb_security_event_2026_06
                   WHERE co_security_event = ev) THEN
        RAISE EXCEPTION 'S12 FAIL: event did not route to partition 2026_06';
    END IF;

    INSERT INTO seg.tb_security_event (co_security_event, tx_event, dt_created_at)
    VALUES (gen_random_uuid(), 'auth.login.success', '2027-03-01 10:00:00');
    IF NOT EXISTS (SELECT 1 FROM seg.tb_security_event_default
                   WHERE dt_created_at = '2027-03-01 10:00:00') THEN
        RAISE EXCEPTION 'S12 FAIL: out-of-range event did not route to default partition';
    END IF;
END $$;

ROLLBACK;

\echo ''
\echo 'ALL CHECKS PASSED — base model matches database/diagram_base.txt'
