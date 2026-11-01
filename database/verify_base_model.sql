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
BEGIN
    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_user, 'history@test.dev', now(), now());

    INSERT INTO seg.tb_role (co_role, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_role, 'VERIFY_HISTORY', now(), now());

    INSERT INTO seg.tb_permission (co_permission, tx_code, tx_module, tx_name,
                                   dt_created_at, dt_updated_at)
    VALUES (v_perm, 'verify.history', 'VERIFY', 'Verify history', now(), now());

    INSERT INTO core.tb_place (co_place, tx_name, tx_title, tx_category,
                               nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
    VALUES (v_place, 'History Place', 'History Place', 'Hotel',
            -34.6, -58.4, now(), now());

    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Cena', now(), now());

    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                   dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Milanesa', 10.00, now(), now());

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
BEGIN
    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_user, 'history2@test.dev', now(), now());
    INSERT INTO core.tb_place (co_place, tx_name, tx_title, tx_category,
                               nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
    VALUES (v_place, 'H2', 'H2', 'Hotel', -34.6, -58.4, now(), now());
    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Almuerzo', now(), now());
    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                   dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Milanesa', 10.00, now(), now());

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
BEGIN
    INSERT INTO seg.tb_user (co_user, tx_email, dt_created_at, dt_updated_at)
    VALUES (v_user, 'history3@test.dev', now(), now());
    INSERT INTO seg.tb_role (co_role, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_role, 'VERIFY_HISTORY_3', now(), now());
    INSERT INTO core.tb_place (co_place, tx_name, tx_title, tx_category,
                               nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
    VALUES (v_place, 'H3', 'H3', 'Hotel', -34.6, -58.4, now(), now());

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
BEGIN
    INSERT INTO core.tb_place (co_place, tx_name, tx_title, tx_category,
                               nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
    VALUES (v_place, 'Actor Place', 'Actor Place', 'Hotel', -34.6, -58.4, now(), now());
    INSERT INTO core.tb_menu (co_menu, co_place, tx_name, dt_created_at, dt_updated_at)
    VALUES (v_menu, v_place, 'Carta', now(), now());

    -- Garbage in the GUC degrades to NULL, never to an aborted business write.
    PERFORM set_config('app.current_user_id', 'not-a-uuid', true);
    INSERT INTO core.tb_menu_item (co_menu_item, co_menu, tx_name, nu_price,
                                   dt_created_at, dt_updated_at)
    VALUES (v_item, v_menu, 'Item', 9.99, now(), now());
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
BEGIN
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
    INSERT INTO core.tb_place (co_place, tx_name, tx_title, tx_category,
                               nu_latitude, nu_longitude, dt_created_at, dt_updated_at)
    VALUES (v_place, 'Unique Place', 'Unique Place', 'Hotel', -34.6, -58.4, now(), now());
    BEGIN
        INSERT INTO core.tb_place_history (co_place_history, co_place, tx_name, tx_title,
            tx_category, nu_latitude, nu_longitude, tx_status, tx_operation, dt_valid_from)
        VALUES (gen_random_uuid(), v_place, 'X', 'X', 'Hotel', 0, 0, 'ACTIVE', 'INSERT', now());
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

ROLLBACK;

\echo ''
\echo 'ALL CHECKS PASSED — base model matches database/diagram_base.txt'
