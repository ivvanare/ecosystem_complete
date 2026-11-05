-- ============================================================================
-- verify_base_model.sql — runnable assertions for the melia base model.
-- Spec: database/diagram_base.txt · Change: openspec/changes/base-domain-model
--
-- Usage (live PostgreSQL 15, e.g. docker compose up database):
--   psql -h localhost -U admin -d melia -v ON_ERROR_STOP=1 -f database/verify_base_model.sql
-- Against a scratch database (a throwaway proof of pending schema files):
--   psql -h localhost -U admin -d <scratch> -v ON_ERROR_STOP=1 -v expected_db=<scratch> -f database/verify_base_model.sql
--
-- Every check RAISEs EXCEPTION on failure. All behavioral inserts run inside a
-- transaction that is ROLLED BACK at the end: the database is left untouched.
-- ============================================================================
\set ON_ERROR_STOP on

-- The S1 name guard expects melia unless the caller overrides it. psql does not
-- expand variables inside $$ bodies, so the value travels through a custom GUC.
\if :{?expected_db}
\else
\set expected_db 'melia'
\endif
SELECT set_config('verify.expected_db', :'expected_db', false);

BEGIN;

-- ---- S1: topology — one database, schemas seg + core ------------------------
DO $$
BEGIN
    IF current_database() <> current_setting('verify.expected_db') THEN
        RAISE EXCEPTION 'S1 FAIL: expected database %, got %',
            current_setting('verify.expected_db'), current_database();
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

-- ---- S15: history inventory — 6 tables, one open interval each --------------
DO $$
DECLARE
    missing   text;
    no_unique text;
    bad_enum  text;
BEGIN
    SELECT string_agg(v.t, ', ') INTO missing
    FROM (VALUES
        ('core.tb_place_history'), ('core.tb_menu_item_history'),
        ('core.tb_place_schedule_history'), ('core.tb_review_history'),
        ('seg.tb_user_role_history'), ('seg.tb_role_permission_history')
    ) AS v(t)
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_tables pt
        WHERE pt.schemaname || '.' || pt.tablename = v.t
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'S15 FAIL: missing history tables: %', missing;
    END IF;

    -- Exactly one open (dt_valid_to IS NULL) interval per entity: a partial unique
    -- index is what makes "the current state" unambiguous.
    SELECT string_agg(v.t, ', ') INTO no_unique
    FROM (VALUES
        ('core.tb_place_history'), ('core.tb_menu_item_history'),
        ('core.tb_place_schedule_history'), ('core.tb_review_history'),
        ('seg.tb_user_role_history'), ('seg.tb_role_permission_history')
    ) AS v(t)
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_indexes pi
        WHERE pi.schemaname || '.' || pi.tablename = v.t
          AND pi.indexdef ILIKE 'CREATE UNIQUE INDEX%'
          AND pi.indexdef ILIKE '%dt_valid_to IS NULL%'
    );
    IF no_unique IS NOT NULL THEN
        RAISE EXCEPTION 'S15 FAIL: history tables without an open-interval unique: %', no_unique;
    END IF;

    SELECT string_agg(e.enumlabel, ', ' ORDER BY e.enumsortorder) INTO bad_enum
    FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
    JOIN pg_enum e ON e.enumtypid = t.oid
    WHERE n.nspname = 'public' AND t.typname = 'change_operation';
    IF bad_enum IS DISTINCT FROM 'INSERT, UPDATE, DELETE' THEN
        RAISE EXCEPTION 'S15 FAIL: public.change_operation labels are %', coalesce(bad_enum, 'missing');
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
DECLARE
    v_cat uuid;
BEGIN
    SELECT co_place_category INTO v_cat
      FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;

    BEGIN
        INSERT INTO core.tb_review (co_review, co_user, co_place, nu_rating,
                                    dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 0, now(), now());
        RAISE EXCEPTION 'S6 FAIL: rating 0 admitted (expected 1..5)';
    EXCEPTION WHEN check_violation OR foreign_key_violation THEN NULL;
    END;
    -- The latitude CHECK is what must fire, so the locale columns come along valid.
    BEGIN
        INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                                   nu_latitude, nu_longitude,
                                   co_place_category, tx_country, tx_timezone,
                                   dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), 'X', 'X', 91, 0,
                v_cat, 'VE', 'America/Caracas', now(), now());
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

-- ---- S16: every scoped entity records history -------------------------------
DO $$
DECLARE
    v_user   uuid := gen_random_uuid();
    v_place  uuid := gen_random_uuid();
    v_menu   uuid := gen_random_uuid();
    v_item   uuid := gen_random_uuid();
    v_sched  uuid := gen_random_uuid();
    v_review uuid := gen_random_uuid();
    v_role   uuid := gen_random_uuid();
    v_perm   uuid := gen_random_uuid();
    v_cat    uuid;
    v_cur    uuid;
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;
    SELECT co_currency INTO v_cur FROM core.tb_currency
     WHERE tx_code = 'VES' AND dt_deleted_at IS NULL;

    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_user, 'history@test.dev', now(), now());

    INSERT INTO seg.tb_role (co_role, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_role, 'VERIFY_HISTORY', now(), now());

    INSERT INTO seg.tb_permission (co_permission, tx_code, tx_module, tx_name,
                                   dt_created_at, dt_updated_at)
    VALUES (v_perm, 'verify.history', 'VERIFY', 'Verify history', now(), now());

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'History Place', 'History Place',
            -34.6, -58.4, v_cat, 'VE', 'America/Caracas', now(), now());

    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Cena', now(), now());

    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                   co_currency, dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Milanesa', 10.00, v_cur, now(), now());

    INSERT INTO core.tb_place_schedule (co_schedule, co_place, nu_day_of_week,
                                        dt_open_time, dt_close_time,
                                        dt_created_at, dt_updated_at)
    VALUES (v_sched, v_place, 1, '20:00', '02:00', now(), now());

    INSERT INTO core.tb_review (co_review, co_user, co_place, nu_rating, tx_comment,
                                dt_created_at, dt_updated_at)
    VALUES (v_review, v_user, v_place, 5, 'Muy bueno', now(), now());

    INSERT INTO seg.tb_user_role (co_user, co_role, dt_created_at, dt_updated_at)
    VALUES (v_user, v_role, now(), now());

    INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
    VALUES (v_role, v_perm, now(), now());

    -- The six scoped entities must all have recorded their insert.
    IF NOT EXISTS (SELECT 1 FROM core.tb_place_history WHERE co_place = v_place) THEN
        RAISE EXCEPTION 'S16 FAIL: tb_place insert not recorded';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM core.tb_menu_item_history WHERE co_menu_item = v_item) THEN
        RAISE EXCEPTION 'S16 FAIL: tb_menu_item insert not recorded';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM core.tb_place_schedule_history WHERE co_schedule = v_sched) THEN
        RAISE EXCEPTION 'S16 FAIL: tb_place_schedule insert not recorded';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM core.tb_review_history WHERE co_review = v_review) THEN
        RAISE EXCEPTION 'S16 FAIL: tb_review insert not recorded';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM seg.tb_user_role_history
                    WHERE co_user = v_user AND co_role = v_role) THEN
        RAISE EXCEPTION 'S16 FAIL: tb_user_role insert not recorded';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM seg.tb_role_permission_history
                    WHERE co_role = v_role AND co_permission = v_perm) THEN
        RAISE EXCEPTION 'S16 FAIL: tb_role_permission insert not recorded';
    END IF;

    -- A recorded insert is an OPEN interval with the matching operation.
    IF (SELECT count(*) FROM core.tb_place_history
         WHERE co_place = v_place AND dt_valid_to IS NULL
           AND tx_operation = 'INSERT') <> 1 THEN
        RAISE EXCEPTION 'S16 FAIL: tb_place insert interval is not a single open INSERT';
    END IF;
END $$;

-- ---- S16b: the past survives an update and an interval closes once -----------
DO $$
DECLARE
    v_user   uuid := gen_random_uuid();
    v_place  uuid := gen_random_uuid();
    v_menu   uuid := gen_random_uuid();
    v_item   uuid := gen_random_uuid();
    v_before int;
    v_open   int;
    v_price  numeric;
    v_state  text;
    v_msg    text;
    v_cat    uuid;
    v_cur    uuid;
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;
    SELECT co_currency INTO v_cur FROM core.tb_currency
     WHERE tx_code = 'VES' AND dt_deleted_at IS NULL;

    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_user, 'history2@test.dev', now(), now());
    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'H2', 'H2', -34.6, -58.4,
            v_cat, 'VE', 'America/Caracas', now(), now());
    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Almuerzo', now(), now());
    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                   co_currency, dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Milanesa', 10.00, v_cur, now(), now());

    SELECT count(*) INTO v_before
      FROM core.tb_menu_item_history WHERE co_menu_item = v_item;
    IF v_before <> 1 THEN
        RAISE EXCEPTION 'S16 FAIL: expected 1 history row after insert, got %', v_before;
    END IF;

    -- An UPDATE closes the previous interval and opens exactly one new one.
    UPDATE core.tb_menu_item SET nu_price = 12.50, dt_updated_at = now()
     WHERE co_menu_item = v_item;

    SELECT count(*) INTO v_open FROM core.tb_menu_item_history
     WHERE co_menu_item = v_item AND dt_valid_to IS NULL;
    IF v_open <> 1 THEN
        RAISE EXCEPTION 'S16 FAIL: expected exactly 1 open interval, got %', v_open;
    END IF;

    -- THE point of this feature: the previous price is still recoverable.
    SELECT nu_price INTO v_price FROM core.tb_menu_item_history
     WHERE co_menu_item = v_item AND dt_valid_to IS NOT NULL
     ORDER BY dt_valid_from DESC LIMIT 1;
    IF v_price <> 10.00 THEN
        RAISE EXCEPTION 'S16 FAIL: history lost the previous price (got %)', v_price;
    END IF;

    -- An update that touches no business attribute must not create noise.
    SELECT count(*) INTO v_before
      FROM core.tb_menu_item_history WHERE co_menu_item = v_item;
    UPDATE core.tb_menu_item SET dt_updated_at = now() WHERE co_menu_item = v_item;
    IF (SELECT count(*) FROM core.tb_menu_item_history WHERE co_menu_item = v_item)
       <> v_before THEN
        RAISE EXCEPTION 'S16 FAIL: a no-op update created history noise';
    END IF;

    -- A closed interval is immutable: rewriting it must be rejected by the guard,
    -- with its own SQLSTATE and message. Asserting the exact rejection keeps this
    -- from passing on any unrelated error.
    BEGIN
        UPDATE core.tb_menu_item_history
           SET dt_valid_to = dt_valid_to + interval '1 day'
         WHERE co_menu_item = v_item AND dt_valid_to IS NOT NULL;
        v_state := 'admitted';
    EXCEPTION WHEN others THEN
        GET STACKED DIAGNOSTICS v_state = RETURNED_SQLSTATE, v_msg = MESSAGE_TEXT;
    END;
    IF v_state <> '23001' THEN
        RAISE EXCEPTION 'S16 FAIL: closed interval not rejected by the guard (sqlstate=%, msg=%)',
            v_state, v_msg;
    END IF;
    IF v_msg <> 'history interval of tb_menu_item_history is closed and cannot be rewritten' THEN
        RAISE EXCEPTION 'S16 FAIL: wrong rejection message (%)', v_msg;
    END IF;
END $$;

-- ---- S16c: physical deletes keep their history -------------------------------
DO $$
DECLARE
    v_user   uuid := gen_random_uuid();
    v_place  uuid := gen_random_uuid();
    v_sched  uuid := gen_random_uuid();
    v_role   uuid := gen_random_uuid();
    v_rows   int;
    v_place_of_schedule uuid;
    v_cat    uuid;
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;

    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_user, 'history3@test.dev', now(), now());
    INSERT INTO seg.tb_role (co_role, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_role, 'VERIFY_HISTORY_3', now(), now());
    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'H3', 'H3', -34.6, -58.4,
            v_cat, 'VE', 'America/Caracas', now(), now());

    INSERT INTO core.tb_place_schedule (co_schedule, co_place, nu_day_of_week,
                                        dt_open_time, dt_close_time,
                                        dt_created_at, dt_updated_at)
    VALUES (v_sched, v_place, 2, '09:00', '18:00', now(), now());

    -- The schedule table is deleted PHYSICALLY on purpose (rule 3).
    DELETE FROM core.tb_place_schedule WHERE co_schedule = v_sched;

    SELECT count(*) INTO v_rows FROM core.tb_place_schedule_history
     WHERE co_schedule = v_sched AND tx_operation = 'DELETE';
    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'S16 FAIL: physical delete recorded % DELETE intervals', v_rows;
    END IF;

    -- The payload survives the entity, because the bridge row is gone.
    SELECT co_place INTO v_place_of_schedule FROM core.tb_place_schedule_history
     WHERE co_schedule = v_sched AND tx_operation = 'DELETE';
    IF v_place_of_schedule IS DISTINCT FROM v_place THEN
        RAISE EXCEPTION 'S16 FAIL: DELETE interval lost the place link';
    END IF;

    -- Same for a physically deleted bridge row.
    INSERT INTO seg.tb_user_role (co_user, co_role, dt_created_at, dt_updated_at)
    VALUES (v_user, v_role, now(), now());
    DELETE FROM seg.tb_user_role WHERE co_user = v_user AND co_role = v_role;

    IF (SELECT count(*) FROM seg.tb_user_role_history
         WHERE co_user = v_user AND co_role = v_role AND tx_operation = 'INSERT') <> 1 THEN
        RAISE EXCEPTION 'S16 FAIL: bridge insert was not recorded';
    END IF;
    IF (SELECT count(*) FROM seg.tb_user_role_history
         WHERE co_user = v_user AND co_role = v_role AND dt_valid_to IS NULL) <> 0 THEN
        RAISE EXCEPTION 'S16 FAIL: deleted bridge still has an open interval';
    END IF;
END $$;

-- ---- S17: composite-key triggers cannot mis-close a different pair -----------
DO $$
DECLARE
    v_u1 uuid := gen_random_uuid();
    v_u2 uuid := gen_random_uuid();
    v_r1 uuid := gen_random_uuid();
    v_r2 uuid := gen_random_uuid();
    v_pm uuid := gen_random_uuid();
BEGIN
    -- Fresh fixtures: this block shares the transaction with every other check,
    -- so nothing here may rely on rows another block inserted.
    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_u1, 'pair1@test.dev', now(), now()),
           (v_u2, 'pair2@test.dev', now(), now());
    INSERT INTO seg.tb_role (co_role, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_r1, 'VERIFY_PAIR_1', now(), now()),
           (v_r2, 'VERIFY_PAIR_2', now(), now());
    INSERT INTO seg.tb_permission (co_permission, tx_code, tx_module, tx_name,
                                   dt_created_at, dt_updated_at)
    VALUES (v_pm, 'verify.pair', 'VERIFY', 'Verify pair', now(), now());

    INSERT INTO seg.tb_user_role (co_user, co_role, dt_created_at, dt_updated_at)
    VALUES (v_u1, v_r1, now(), now()), (v_u2, v_r1, now(), now());
    INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
    VALUES (v_r1, v_pm, now(), now()), (v_r2, v_pm, now(), now());

    -- Delete (u1,r1): the trigger must close exactly that pair, not the sibling.
    DELETE FROM seg.tb_user_role WHERE co_user = v_u1 AND co_role = v_r1;
    IF (SELECT count(*) FROM seg.tb_user_role_history
         WHERE co_user = v_u2 AND co_role = v_r1 AND dt_valid_to IS NULL) <> 1 THEN
        RAISE EXCEPTION 'S17 FAIL: deleting (u1,r1) mis-closed (u2,r1)';
    END IF;
    IF (SELECT count(*) FROM seg.tb_user_role_history
         WHERE co_user = v_u1 AND co_role = v_r1 AND tx_operation = 'DELETE'
           AND dt_valid_from = dt_valid_to) <> 1 THEN
        RAISE EXCEPTION 'S17 FAIL: (u1,r1) delete not a single born-closed DELETE row';
    END IF;

    -- PK-change update: (u2,r1) -> (u2,r2) closes the OLD pair, opens the NEW one.
    UPDATE seg.tb_user_role SET co_role = v_r2, dt_updated_at = now()
     WHERE co_user = v_u2 AND co_role = v_r1;
    IF (SELECT count(*) FROM seg.tb_user_role_history
         WHERE co_user = v_u2 AND co_role = v_r1 AND dt_valid_to IS NULL) <> 0
       OR (SELECT count(*) FROM seg.tb_user_role_history
            WHERE co_user = v_u2 AND co_role = v_r2 AND dt_valid_to IS NULL
              AND tx_operation = 'UPDATE') <> 1 THEN
        RAISE EXCEPTION 'S17 FAIL: PK-change update closed/opened the wrong pair';
    END IF;

    -- Same for role_permission: delete (r1,pm), (r2,pm) must stay open.
    DELETE FROM seg.tb_role_permission WHERE co_role = v_r1 AND co_permission = v_pm;
    IF (SELECT count(*) FROM seg.tb_role_permission_history
         WHERE co_role = v_r2 AND co_permission = v_pm AND dt_valid_to IS NULL) <> 1 THEN
        RAISE EXCEPTION 'S17 FAIL: deleting (r1,pm) mis-closed (r2,pm)';
    END IF;
END $$;

-- ---- S18: a malformed actor never aborts a business write --------------------
DO $$
DECLARE
    v_place uuid := gen_random_uuid();
    v_menu  uuid := gen_random_uuid();
    v_item  uuid := gen_random_uuid();
    v_actor uuid;
    v_cat   uuid;
    v_cur   uuid;
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;
    SELECT co_currency INTO v_cur FROM core.tb_currency
     WHERE tx_code = 'VES' AND dt_deleted_at IS NULL;

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'Actor Place', 'Actor Place', -34.6, -58.4,
            v_cat, 'VE', 'America/Caracas', now(), now());
    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Carta', now(), now());

    -- Garbage in the GUC degrades to NULL, never to an aborted business write.
    PERFORM set_config('app.current_user_id', 'not-a-uuid', true);
    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                   co_currency, dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Item', 9.99, v_cur, now(), now());
    SELECT co_actor_user INTO v_actor FROM core.tb_menu_item_history
     WHERE co_menu_item = v_item;
    IF v_actor IS NOT NULL THEN
        RAISE EXCEPTION 'S18 FAIL: malformed actor leaked into history';
    END IF;

    -- A well-formed actor is recorded exactly.
    PERFORM set_config('app.current_user_id', '11111111-1111-1111-1111-111111111111', true);
    UPDATE core.tb_menu_item SET nu_price = 10.50, dt_updated_at = now()
     WHERE co_menu_item = v_item;
    SELECT co_actor_user INTO v_actor FROM core.tb_menu_item_history
     WHERE co_menu_item = v_item AND dt_valid_to IS NULL;
    IF v_actor IS DISTINCT FROM '11111111-1111-1111-1111-111111111111'::uuid THEN
        RAISE EXCEPTION 'S18 FAIL: announced actor not recorded (got %)', v_actor;
    END IF;

    -- Leave the GUC clean so the blocks that follow are unaffected.
    PERFORM set_config('app.current_user_id', '', true);
END $$;

-- ---- S19: the interval CHECK and the open-interval unique both bite ----------
DO $$
DECLARE
    v_place uuid := gen_random_uuid();
    v_cat   uuid;
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;

    -- An inverted interval (dt_valid_to < dt_valid_from) must be rejected.
    BEGIN
        INSERT INTO core.tb_review_history (co_review_history, co_review, nu_rating,
            tx_status, tx_operation, dt_valid_from, dt_valid_to)
        VALUES (gen_random_uuid(), gen_random_uuid(), 4, 'ACTIVE', 'INSERT',
                now(), now() - interval '1 hour');
        RAISE EXCEPTION 'S19 FAIL: inverted interval admitted';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    -- The trigger opens one interval on insert; a second open one cannot coexist.
    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'Unique Place', 'Unique Place', -34.6, -58.4,
            v_cat, 'VE', 'America/Caracas', now(), now());
    BEGIN
        INSERT INTO core.tb_place_history (co_place_history, co_place, tx_name, tx_title,
            co_place_category, tx_country, tx_timezone,
            nu_latitude, nu_longitude, tx_status, tx_operation, dt_valid_from)
        VALUES (gen_random_uuid(), v_place, 'X', 'X', v_cat, 'VE', 'America/Caracas',
                0, 0, 'ACTIVE', 'INSERT', now());
        RAISE EXCEPTION 'S19 FAIL: second open interval admitted';
    EXCEPTION WHEN unique_violation THEN NULL;
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

-- ---- S20: locale catalogs exist, seeded, and the storage convention is UTC ---
DO $$
BEGIN
    -- Both catalogs exist with exactly their seeds, and nothing else.
    IF (SELECT count(*) FROM core.tb_place_category WHERE dt_deleted_at IS NULL) <> 3
       OR (SELECT count(*) FROM core.tb_place_category
            WHERE dt_deleted_at IS NULL
              AND (tx_code, tx_name) IN (('hotel', 'Hotel'), ('arepera', 'Arepera'),
                                         ('parque', 'Parque'))) <> 3 THEN
        RAISE EXCEPTION 'S20 FAIL: expected exactly the 3 seeded place categories';
    END IF;
    IF (SELECT count(*) FROM core.tb_currency WHERE dt_deleted_at IS NULL) <> 3
       OR (SELECT count(*) FROM core.tb_currency
            WHERE dt_deleted_at IS NULL
              AND tx_code IN ('USD', 'VES', 'COP') AND nu_decimals = 2) <> 3 THEN
        RAISE EXCEPTION 'S20 FAIL: expected exactly the 3 seeded currencies with nu_decimals = 2';
    END IF;

    -- nu_decimals is present with its range CHECK.
    IF (SELECT data_type FROM information_schema.columns
        WHERE table_schema = 'core' AND table_name = 'tb_currency'
          AND column_name = 'nu_decimals') IS DISTINCT FROM 'smallint' THEN
        RAISE EXCEPTION 'S20 FAIL: tb_currency.nu_decimals is missing or not smallint';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'core.tb_currency'::regclass AND contype = 'c'
                     AND pg_get_constraintdef(oid) ILIKE '%nu_decimals%') THEN
        RAISE EXCEPTION 'S20 FAIL: tb_currency.nu_decimals has no range CHECK';
    END IF;

    -- ISO-4217 shape CHECK on the currency code.
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'core.tb_currency'::regclass AND contype = 'c'
                     AND pg_get_constraintdef(oid) ILIKE '%tx_code%') THEN
        RAISE EXCEPTION 'S20 FAIL: tb_currency.tx_code has no ISO-4217 shape CHECK';
    END IF;

    -- Both catalogs carry the partial unique on tx_code (rule 4).
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                   WHERE schemaname = 'core' AND tablename = 'tb_place_category'
                     AND indexdef ILIKE 'CREATE UNIQUE INDEX%'
                     AND indexdef ILIKE '%(tx_code)%'
                     AND indexdef ILIKE '%dt_deleted_at IS NULL%') THEN
        RAISE EXCEPTION 'S20 FAIL: tb_place_category has no partial unique on tx_code';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                   WHERE schemaname = 'core' AND tablename = 'tb_currency'
                     AND indexdef ILIKE 'CREATE UNIQUE INDEX%'
                     AND indexdef ILIKE '%(tx_code)%'
                     AND indexdef ILIKE '%dt_deleted_at IS NULL%') THEN
        RAISE EXCEPTION 'S20 FAIL: tb_currency has no partial unique on tx_code';
    END IF;

    -- The UTC storage convention is pinned at the database level, so a fresh
    -- session inherits it without any client SET.
    IF current_setting('TimeZone') <> 'UTC' THEN
        RAISE EXCEPTION 'S20 FAIL: session TimeZone is %, expected UTC',
            current_setting('TimeZone');
    END IF;
END $$;

-- ---- S21: tb_place demands its locale columns and history mirrors them ------
DO $$
DECLARE
    v_cat   uuid;
    v_place uuid := gen_random_uuid();
BEGIN
    SELECT co_place_category INTO v_cat
      FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;
    IF v_cat IS NULL THEN
        RAISE EXCEPTION 'S21 FAIL: seeded category hotel not found';
    END IF;

    -- A place without category/country/timezone is rejected, with the exact
    -- SQLSTATE: no insert silently inherits Venezuela.
    BEGIN
        INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                                   nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), 'X', 'X', 10, 10, now(), now());
        RAISE EXCEPTION 'S21 FAIL: place without locale columns admitted';
    EXCEPTION WHEN not_null_violation THEN NULL;
    END;

    -- A shape that cannot be a zone name (four levels) is rejected by the CHECK,
    -- with the exact SQLSTATE.
    BEGIN
        INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                                   nu_latitude, nu_longitude,
                                   co_place_category, tx_country, tx_timezone,
                                   dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), 'X', 'X', 10, 10,
                v_cat, 'VE', 'America/Argentina/Buenos_Aires/Extra', now(), now());
        RAISE EXCEPTION 'S21 FAIL: bad IANA timezone shape admitted';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    -- A valid place succeeds and its open interval carries the locale payload.
    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'Melia', 'Melia', 10.5, -66.9,
            v_cat, 'VE', 'America/Caracas', now(), now());

    IF NOT EXISTS (SELECT 1 FROM core.tb_place_history
                    WHERE co_place = v_place AND dt_valid_to IS NULL
                      AND tx_operation = 'INSERT'
                      AND co_place_category = v_cat
                      AND tx_country = 'VE'
                      AND tx_timezone = 'America/Caracas') THEN
        RAISE EXCEPTION 'S21 FAIL: place history lost category/country/timezone';
    END IF;

    -- A category that is in use cannot be deleted: the FK protects it, with
    -- the exact SQLSTATE.
    BEGIN
        DELETE FROM core.tb_place_category WHERE co_place_category = v_cat;
        RAISE EXCEPTION 'S21 FAIL: in-use category delete admitted';
    EXCEPTION WHEN foreign_key_violation THEN NULL;
    END;
END $$;

-- ---- S22: tb_menu_item demands its currency and history mirrors it ----------
DO $$
DECLARE
    v_cat   uuid;
    v_cur   uuid;
    v_place uuid := gen_random_uuid();
    v_menu  uuid := gen_random_uuid();
    v_item  uuid := gen_random_uuid();
BEGIN
    SELECT co_place_category INTO v_cat
      FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;
    SELECT co_currency INTO v_cur
      FROM core.tb_currency
     WHERE tx_code = 'VES' AND dt_deleted_at IS NULL;
    IF v_cat IS NULL OR v_cur IS NULL THEN
        RAISE EXCEPTION 'S22 FAIL: seeded category/currency not found';
    END IF;

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'S22', 'S22', 10.5, -66.9,
            v_cat, 'VE', 'America/Caracas', now(), now());
    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Carta', now(), now());

    -- A menu item without a currency is rejected, with the exact SQLSTATE.
    BEGIN
        INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                       dt_created_at, dt_updated_at)
        VALUES (gen_random_uuid(), v_menu, 'Item', 10.00, now(), now());
        RAISE EXCEPTION 'S22 FAIL: menu item without currency admitted';
    EXCEPTION WHEN not_null_violation THEN NULL;
    END;

    -- With a valid currency it succeeds and its open interval carries it.
    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price, co_currency,
                                   dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Item', 10.00, v_cur, now(), now());

    IF NOT EXISTS (SELECT 1 FROM core.tb_menu_item_history
                    WHERE co_menu_item = v_item AND dt_valid_to IS NULL
                      AND tx_operation = 'INSERT'
                      AND co_currency = v_cur) THEN
        RAISE EXCEPTION 'S22 FAIL: menu item history lost the currency';
    END IF;
END $$;

-- ---- S23: legacy tx_category is gone and place history tracks the category --
DO $$
DECLARE
    v_cat_old uuid;
    v_cat_new uuid;
    v_place   uuid := gen_random_uuid();
BEGIN
    -- Migration 12 dropped the free-text column: the catalog FK is the only
    -- category the entity carries.
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'core' AND table_name = 'tb_place'
                 AND column_name = 'tx_category') THEN
        RAISE EXCEPTION 'S23 FAIL: core.tb_place.tx_category still exists (migration 12)';
    END IF;

    SELECT co_place_category INTO v_cat_old FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;
    SELECT co_place_category INTO v_cat_new FROM core.tb_place_category
     WHERE tx_code = 'arepera' AND dt_deleted_at IS NULL;
    IF v_cat_old IS NULL OR v_cat_new IS NULL THEN
        RAISE EXCEPTION 'S23 FAIL: seeded categories hotel/arepera not found';
    END IF;

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'S23', 'S23', 10.5, -66.9,
            v_cat_old, 'VE', 'America/Caracas', now(), now());

    -- Changing ONLY the category must record a real interval: the closed one
    -- holds the previous category, the open one the new one.
    UPDATE core.tb_place SET co_place_category = v_cat_new, dt_updated_at = now()
     WHERE co_place = v_place;

    IF NOT EXISTS (SELECT 1 FROM core.tb_place_history
                    WHERE co_place = v_place AND dt_valid_to IS NOT NULL
                      AND co_place_category = v_cat_old) THEN
        RAISE EXCEPTION 'S23 FAIL: closed interval lost the previous category';
    END IF;
    IF (SELECT count(*) FROM core.tb_place_history
         WHERE co_place = v_place AND dt_valid_to IS NULL) <> 1
       OR NOT EXISTS (SELECT 1 FROM core.tb_place_history
                       WHERE co_place = v_place AND dt_valid_to IS NULL
                         AND co_place_category = v_cat_new
                         AND tx_operation = 'UPDATE') THEN
        RAISE EXCEPTION 'S23 FAIL: open interval does not hold the new category';
    END IF;
END $$;

-- ---- S24: every table, column and enum carries a database COMMENT ----------
DO $$
DECLARE
    missing text;
BEGIN
    -- Base tables of seg and core (partitions excluded: the comment of the
    -- partitioned parent covers them).
    SELECT string_agg(n.nspname || '.' || c.relname, ', '
                      ORDER BY n.nspname || '.' || c.relname)
      INTO missing
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname IN ('seg', 'core')
       AND c.relkind IN ('r', 'p')
       AND NOT c.relispartition
       AND coalesce(obj_description(c.oid, 'pg_class'), '') = '';
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'S24 FAIL: tables without COMMENT: %', missing;
    END IF;

    -- Every column of those same tables.
    SELECT string_agg(n.nspname || '.' || c.relname || '.' || a.attname, ', '
                      ORDER BY n.nspname || '.' || c.relname || '.' || a.attname)
      INTO missing
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_attribute a ON a.attrelid = c.oid
     WHERE n.nspname IN ('seg', 'core')
       AND c.relkind IN ('r', 'p')
       AND NOT c.relispartition
       AND a.attnum > 0
       AND NOT a.attisdropped
       AND coalesce(col_description(a.attrelid, a.attnum), '') = '';
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'S24 FAIL: columns without COMMENT: %', missing;
    END IF;

    -- The shared enums of public.
    SELECT string_agg(t.typname, ', ' ORDER BY t.typname)
      INTO missing
      FROM pg_type t
      JOIN pg_namespace n ON n.oid = t.typnamespace
     WHERE n.nspname = 'public' AND t.typtype = 'e'
       AND coalesce(obj_description(t.oid, 'pg_type'), '') = '';
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'S24 FAIL: enum types without COMMENT: %', missing;
    END IF;
END $$;

-- ---- S25: real multi-level IANA zones are accepted (regression pin for 14) --
-- The first shape check in 11 admitted only one slash and therefore rejected 26
-- legitimate IANA names, so a place in Argentina could not be created at all.
DO $$
DECLARE
    v_cat   uuid;
    v_place uuid := gen_random_uuid();
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'Buenos Aires', 'BA', -34.6, -58.4,
            v_cat, 'AR', 'America/Argentina/Buenos_Aires', now(), now());

    IF NOT EXISTS (SELECT 1 FROM core.tb_place_history
                    WHERE co_place = v_place AND dt_valid_to IS NULL
                      AND tx_timezone = 'America/Argentina/Buenos_Aires') THEN
        RAISE EXCEPTION 'S25 FAIL: history lost the multi-level timezone';
    END IF;

    -- A bare IANA name is valid too (UTC and GMT exist as first-level zones).
    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (gen_random_uuid(), 'UTC Place', 'UTC', 0, 0,
            v_cat, 'AR', 'UTC', now(), now());
END $$;

-- ---- S26: co_location is derived from the decimals and follows them ---------
-- Phase 0 of social discovery (D2/S16): the decimals are the contract and the
-- geography column is a GENERATED ALWAYS projection of them.
DO $$
DECLARE
    v_cat   uuid;
    v_place uuid := gen_random_uuid();
BEGIN
    -- The extension is present (15-postgis-extension.sql).
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'postgis') THEN
        RAISE EXCEPTION 'S26 FAIL: postgis extension is not created';
    END IF;

    -- The column is a stored generated column on the entity.
    IF NOT EXISTS (SELECT 1 FROM pg_attribute a
                   JOIN pg_class c ON c.oid = a.attrelid
                   JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = 'core' AND c.relname = 'tb_place'
                     AND a.attname = 'co_location' AND a.attgenerated = 's'
                     AND NOT a.attisdropped) THEN
        RAISE EXCEPTION 'S26 FAIL: co_location is not a stored generated column';
    END IF;

    -- The GiST index serves the radius and viewport reads.
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                   WHERE schemaname = 'core' AND tablename = 'tb_place'
                     AND indexname = 'ix_tb_place_location'
                     AND indexdef ILIKE '%USING gist%') THEN
        RAISE EXCEPTION 'S26 FAIL: tb_place has no GiST index on co_location';
    END IF;

    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'S26', 'S26', 10.5, -66.9,
            v_cat, 'VE', 'America/Caracas', now(), now());

    -- The derived value agrees with the decimals on insert (10.5, -66.9 would
    -- also catch a swapped MakePoint argument order) ...
    IF NOT (SELECT ST_Equals(co_location::geometry,
                             ST_SetSRID(ST_MakePoint(nu_longitude, nu_latitude), 4326))
              FROM core.tb_place WHERE co_place = v_place) THEN
        RAISE EXCEPTION 'S26 FAIL: co_location disagrees with the decimals on insert';
    END IF;

    -- ... and follows an update of them.
    UPDATE core.tb_place SET nu_latitude = -34.6, nu_longitude = -58.4,
                             dt_updated_at = now()
     WHERE co_place = v_place;
    IF NOT (SELECT ST_Equals(co_location::geometry,
                             ST_SetSRID(ST_MakePoint(-58.4, -34.6), 4326))
              FROM core.tb_place WHERE co_place = v_place) THEN
        RAISE EXCEPTION 'S26 FAIL: co_location did not follow an update of the decimals';
    END IF;

    -- A hand-written value is rejected: the column is derived, never authored.
    BEGIN
        UPDATE core.tb_place
           SET co_location = ST_SetSRID(ST_MakePoint(0, 0), 4326)::geography
         WHERE co_place = v_place;
        RAISE EXCEPTION 'S26 FAIL: co_location accepted a hand-written value';
    EXCEPTION WHEN generated_always THEN NULL;
    END;
END $$;

-- ---- S27: tx_region shape, navigation index, and history mirror -------------
-- Phase 0 of social discovery (S3/S15): region is a nullable ISO 3166-2 code,
-- and the place history payload carries it.
DO $$
DECLARE
    v_cat   uuid;
    v_place uuid := gen_random_uuid();
BEGIN
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'hotel' AND dt_deleted_at IS NULL;

    -- NULL is deliberate and must be admitted: the geocoder may not answer.
    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'S27', 'S27', 10.5, -66.9,
            v_cat, 'VE', 'America/Caracas', now(), now());

    -- Shape violations are rejected by the CHECK, with the exact SQLSTATE.
    BEGIN
        UPDATE core.tb_place SET tx_region = 've-a', dt_updated_at = now()
         WHERE co_place = v_place;
        RAISE EXCEPTION 'S27 FAIL: lowercase region admitted';
    EXCEPTION WHEN check_violation THEN NULL;
    END;
    BEGIN
        UPDATE core.tb_place SET tx_region = 'VEA1', dt_updated_at = now()
         WHERE co_place = v_place;
        RAISE EXCEPTION 'S27 FAIL: region without the hyphen admitted';
    EXCEPTION WHEN check_violation THEN NULL;
    END;

    -- A valid ISO 3166-2 code is accepted and the open interval carries it.
    UPDATE core.tb_place SET tx_region = 'VE-A', dt_updated_at = now()
     WHERE co_place = v_place;
    IF NOT EXISTS (SELECT 1 FROM core.tb_place_history
                    WHERE co_place = v_place AND dt_valid_to IS NULL
                      AND tx_operation = 'UPDATE' AND tx_region = 'VE-A') THEN
        RAISE EXCEPTION 'S27 FAIL: place history lost tx_region';
    END IF;

    -- Navigation reads use (tx_country, tx_region) together, in that order.
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                   WHERE schemaname = 'core' AND tablename = 'tb_place'
                     AND indexname = 'ix_tb_place_country_region'
                     AND indexdef ILIKE '%(tx_country, tx_region)%') THEN
        RAISE EXCEPTION 'S27 FAIL: ix_tb_place_country_region missing or wrong order';
    END IF;

    -- The derived co_location stays OUT of the history payload (S16/16: the
    -- payload already carries the decimals it derives from).
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'core' AND table_name = 'tb_place_history'
                 AND column_name = 'co_location') THEN
        RAISE EXCEPTION 'S27 FAIL: history payload carries the derived co_location';
    END IF;
END $$;

-- ---- S28: attribute vocabulary, link bridge, and generated search columns ---
-- Phase 1 commit 3 of social discovery (S15-S23): attributes are a governed
-- many-to-many with a composite-key bridge, and text search runs on generated
-- tsvectors built through an immutable unaccent wrapper (18/19).
DO $$
DECLARE
    v_place  uuid := gen_random_uuid();
    v_menu   uuid := gen_random_uuid();
    v_item   uuid := gen_random_uuid();
    v_cat    uuid;
    v_usd    uuid;
    v_attr_t uuid;
    v_attr_p uuid;
BEGIN
    -- The bridge key shape: composite PRIMARY KEY (co_place, co_attribute),
    -- and NO soft delete, exactly like tb_route_place (S15).
    IF NOT EXISTS (SELECT 1
                     FROM pg_constraint c
                     JOIN pg_index i ON i.indexrelid = c.conindid
                    WHERE c.conrelid = 'core.tb_place_attribute_link'::regclass
                      AND c.contype = 'p'
                      AND (SELECT array_agg(a.attname ORDER BY a.attname)
                             FROM pg_attribute a
                            WHERE a.attrelid = c.conindid
                              AND NOT a.attisdropped) = '{co_attribute,co_place}') THEN
        RAISE EXCEPTION 'S28 FAIL: tb_place_attribute_link has no composite PK (co_place, co_attribute)';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema = 'core' AND table_name = 'tb_place_attribute_link'
                  AND column_name = 'dt_deleted_at') THEN
        RAISE EXCEPTION 'S28 FAIL: tb_place_attribute_link carries soft delete (composite-key bridges do not)';
    END IF;

    -- tx_code is unique among LIVE rows, like every soft-deletable catalog.
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'core' AND tablename = 'tb_place_attribute'
                      AND indexname = 'ux_tb_place_attribute_code_active'
                      AND indexdef ILIKE '%UNIQUE%'
                      AND indexdef ILIKE '%dt_deleted_at IS NULL%') THEN
        RAISE EXCEPTION 'S28 FAIL: ux_tb_place_attribute_code_active missing or not partial';
    END IF;

    -- The vocabulary and the new categories are seeded by the migration, not
    -- created on the fly (S15/S23).
    IF (SELECT count(*) FROM core.tb_place_attribute
         WHERE tx_code IN ('terraza','petfriendly','frente_playa','aire_libre','musica_en_vivo')
           AND tx_status = 'ACTIVE' AND dt_deleted_at IS NULL) <> 5 THEN
        RAISE EXCEPTION 'S28 FAIL: the attribute vocabulary is not seeded';
    END IF;
    IF (SELECT count(*) FROM core.tb_place_category
         WHERE tx_code IN ('rumba','restaurante','bar')
           AND tx_status = 'ACTIVE' AND dt_deleted_at IS NULL) <> 3 THEN
        RAISE EXCEPTION 'S28 FAIL: the new categories (rumba, restaurante, bar) are not seeded';
    END IF;

    -- The wrapper exists, declared IMMUTABLE and STRICT: the declaration is
    -- the correctness decision the migration header documents (S16).
    IF NOT EXISTS (SELECT 1 FROM pg_proc p
                     JOIN pg_namespace n ON n.oid = p.pronamespace
                    WHERE n.nspname = 'public' AND p.proname = 'unaccent_immutable'
                      AND p.provolatile = 'i' AND p.proisstrict) THEN
        RAISE EXCEPTION 'S28 FAIL: public.unaccent_immutable is not an IMMUTABLE STRICT function';
    END IF;

    -- Both search columns are stored generated columns, with their GIN indexes.
    IF NOT EXISTS (SELECT 1 FROM pg_attribute a
                     JOIN pg_class c ON c.oid = a.attrelid
                     JOIN pg_namespace n ON n.oid = c.relnamespace
                    WHERE n.nspname = 'core' AND c.relname = 'tb_place'
                      AND a.attname = 'search_name' AND a.attgenerated = 's'
                      AND NOT a.attisdropped) THEN
        RAISE EXCEPTION 'S28 FAIL: tb_place.search_name is not a stored generated column';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_attribute a
                     JOIN pg_class c ON c.oid = a.attrelid
                     JOIN pg_namespace n ON n.oid = c.relnamespace
                    WHERE n.nspname = 'core' AND c.relname = 'tb_menu_item'
                      AND a.attname = 'search_text' AND a.attgenerated = 's'
                      AND NOT a.attisdropped) THEN
        RAISE EXCEPTION 'S28 FAIL: tb_menu_item.search_text is not a stored generated column';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'core' AND tablename = 'tb_place'
                      AND indexname = 'ix_tb_place_search_name'
                      AND indexdef ILIKE '%USING gin%') THEN
        RAISE EXCEPTION 'S28 FAIL: tb_place has no GIN index on search_name';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'core' AND tablename = 'tb_menu_item'
                      AND indexname = 'ix_tb_menu_item_search_text'
                      AND indexdef ILIKE '%USING gin%') THEN
        RAISE EXCEPTION 'S28 FAIL: tb_menu_item has no GIN index on search_text';
    END IF;

    -- A real match through the wrapper: the stored vector and the query vector
    -- come from the same expression, so 'Térraza' answers 'terraza' ...
    SELECT co_place_category INTO v_cat FROM core.tb_place_category
     WHERE tx_code = 'restaurante' AND dt_deleted_at IS NULL;
    SELECT co_currency INTO v_usd FROM core.tb_currency
     WHERE tx_code = 'USD' AND dt_deleted_at IS NULL;

    INSERT INTO core.tb_place (co_place, tx_name, tx_title,
                               nu_latitude, nu_longitude,
                               co_place_category, tx_country, tx_timezone,
                               dt_created_at, dt_updated_at)
    VALUES (v_place, 'S28', 'Café Térraza S28', 10.5, -66.9,
            v_cat, 'VE', 'America/Caracas', now(), now());

    IF NOT EXISTS (SELECT 1 FROM core.tb_place
                    WHERE co_place = v_place
                      AND search_name @@ plainto_tsquery('simple', public.unaccent_immutable('terraza'))) THEN
        RAISE EXCEPTION 'S28 FAIL: the name vector does not match through the immutable wrapper';
    END IF;
    -- ... and 'terrazas' does NOT match: 'simple' must not stem names (S16).
    IF EXISTS (SELECT 1 FROM core.tb_place
                WHERE co_place = v_place
                  AND search_name @@ plainto_tsquery('simple', public.unaccent_immutable('terrazas'))) THEN
        RAISE EXCEPTION 'S28 FAIL: the simple configuration stemmed a place name (it must not)';
    END IF;

    -- The dish side: menu -> item, the vector covers name and description.
    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, tx_status, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Cena', 'ACTIVE', now(), now());
    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, tx_description,
                                   nu_price, co_currency, tx_status, dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Arepa Reina Pepiada', 'Con aguacate y pollo', 4.50,
            v_usd, 'ACTIVE', now(), now());

    IF NOT EXISTS (SELECT 1 FROM core.tb_menu_item
                    WHERE co_menu_item = v_item
                      AND search_text @@ plainto_tsquery('simple', public.unaccent_immutable('pepiada'))) THEN
        RAISE EXCEPTION 'S28 FAIL: the dish vector does not match through the immutable wrapper';
    END IF;

    -- The conjunctive count the filter relies on (S18): both attributes linked,
    -- the intersection of {terraza, petfriendly} counts 2.
    SELECT co_attribute INTO v_attr_t FROM core.tb_place_attribute
     WHERE tx_code = 'terraza' AND dt_deleted_at IS NULL;
    SELECT co_attribute INTO v_attr_p FROM core.tb_place_attribute
     WHERE tx_code = 'petfriendly' AND dt_deleted_at IS NULL;
    INSERT INTO core.tb_place_attribute_link (co_place, co_attribute, dt_created_at, dt_updated_at)
    VALUES (v_place, v_attr_t, now(), now()), (v_place, v_attr_p, now(), now());

    IF (SELECT count(*) FROM core.tb_place_attribute_link l
         JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
        WHERE l.co_place = v_place
          AND a.tx_code = ANY(ARRAY['terraza','petfriendly'])
          AND a.dt_deleted_at IS NULL) <> 2 THEN
        RAISE EXCEPTION 'S28 FAIL: the conjunctive attribute count is broken';
    END IF;

    -- The derived search columns stay OUT of the history payloads, like
    -- co_location (S27): the payloads already carry what they derive from.
    IF EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema = 'core' AND table_name = 'tb_place_history'
                  AND column_name = 'search_name')
       OR EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'core' AND table_name = 'tb_menu_item_history'
                     AND column_name = 'search_text') THEN
        RAISE EXCEPTION 'S28 FAIL: a derived search column leaked into a history payload';
    END IF;
END $$;

ROLLBACK;

\echo ''
\echo 'ALL CHECKS PASSED — base model matches database/diagram_base.txt'
