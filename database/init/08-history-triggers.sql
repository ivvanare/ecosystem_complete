-- ============================================================================
-- HISTORY TRIGGERS — keep the six history tables in 07 up to date.
-- Spec: database/diagram_base.txt (section "HISTORIA"). Feature: odd/tasks/db-history.md
--
-- WHY TRIGGERS AND NOT THE SERVICE
--   The service is the only writer today, and it still could forget. A trigger
--   cannot be bypassed by a new repository, a migration script, a psql session
--   or a future second service, and it is the only way to record the PHYSICAL
--   deletes that tb_place_schedule, tb_user_role and tb_role_permission perform
--   on purpose (rule 3). It also keeps the Go models untouched: nothing in the
--   API layer changes.
--
-- WHAT ONE CHANGE COSTS
--   Every business write now costs one extra UPDATE (to close the previous
--   interval) plus one INSERT. That is the price of the past; it is bounded and
--   paid only on the six scoped tables.
--
-- THE ACTOR
--   A trigger cannot know who the human was. The service may announce it per
--   transaction:
--       SELECT set_config('app.current_user_id', '<uuid>', true);
--   and co_actor_user is filled from there. When it is not announced the column
--   stays NULL. This needs no Go change today and no schema change tomorrow.
--
-- NOISE SUPPRESSION
--   An UPDATE that touches no business attribute (for example a bare
--   dt_updated_at bump) records nothing: the row is compared with dt_updated_at
--   stripped, so the history cannot be inflated by writes that changed nothing.
--
-- SOFT DELETE
--   A soft delete is an UPDATE on the source, so it lands as an UPDATE interval
--   whose payload carries dt_deleted_at. tx_operation stays INSERT/UPDATE/DELETE
--   exactly as PostgreSQL reports it, with no inferred interpretation.
--
-- PHYSICAL DELETE
--   A physical delete closes the current interval and appends one DELETE row that
--   is BORN CLOSED (dt_valid_from = dt_valid_to). An open DELETE row would leave
--   a deleted entity looking alive in "the current state", which is the opposite
--   of what history is for.
-- ============================================================================
\connect melia

-- ---------------------------------------------------------------------------
-- Who performed the change, when the service announces it. Any malformed value
-- yields NULL instead of breaking a business write: history must never be the
-- reason a user-facing operation fails.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION seg.current_actor_user() RETURNS uuid
LANGUAGE plpgsql STABLE AS $$
DECLARE
    raw text;
BEGIN
    raw := nullif(current_setting('app.current_user_id', true), '');
    IF raw IS NULL THEN
        RETURN NULL;
    END IF;
    RETURN raw::uuid;
EXCEPTION
    WHEN others THEN
        RETURN NULL;
END $$;

-- ---------------------------------------------------------------------------
-- A closed interval is written once and never reopened or rewritten: that is
-- what makes a history row trustworthy. Retention (dropping old rows in bulk)
-- is still possible through a DELETE issued by the owner of the data.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION seg.tg_history_closed_interval_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.dt_valid_to IS NOT NULL THEN
        RAISE EXCEPTION 'history interval of % is closed and cannot be rewritten', TG_TABLE_NAME
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END $$;

-- ===========================================================================
-- core.tb_place
-- ===========================================================================
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
            tx_category, nu_latitude, nu_longitude, tx_address, js_social_media,
            tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_place, OLD.co_owner_user, OLD.tx_name, OLD.tx_title,
            OLD.tx_description, OLD.tx_category, OLD.nu_latitude, OLD.nu_longitude,
            OLD.tx_address, OLD.js_social_media, OLD.tx_status, OLD.dt_deleted_at,
            'DELETE', seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_place_history (
        co_place_history, co_place, co_owner_user, tx_name, tx_title, tx_description,
        tx_category, nu_latitude, nu_longitude, tx_address, js_social_media,
        tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_place, NEW.co_owner_user, NEW.tx_name, NEW.tx_title,
        NEW.tx_description, NEW.tx_category, NEW.nu_latitude, NEW.nu_longitude,
        NEW.tx_address, NEW.js_social_media, NEW.tx_status, NEW.dt_deleted_at,
        TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE TRIGGER trg_tb_place_history
    AFTER INSERT OR UPDATE OR DELETE ON core.tb_place
    FOR EACH ROW EXECUTE FUNCTION core.tg_tb_place_history();

CREATE TRIGGER trg_tb_place_history_immutable
    BEFORE UPDATE ON core.tb_place_history
    FOR EACH ROW EXECUTE FUNCTION seg.tg_history_closed_interval_immutable();

-- ===========================================================================
-- core.tb_menu_item
-- ===========================================================================
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
            nu_price, tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_menu_item, OLD.co_menu, OLD.tx_name,
            OLD.tx_description, OLD.nu_price, OLD.tx_status, OLD.dt_deleted_at,
            'DELETE', seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_menu_item_history (
        co_menu_item_history, co_menu_item, co_menu, tx_name, tx_description,
        nu_price, tx_status, dt_deleted_at, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_menu_item, NEW.co_menu, NEW.tx_name,
        NEW.tx_description, NEW.nu_price, NEW.tx_status, NEW.dt_deleted_at,
        TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE TRIGGER trg_tb_menu_item_history
    AFTER INSERT OR UPDATE OR DELETE ON core.tb_menu_item
    FOR EACH ROW EXECUTE FUNCTION core.tg_tb_menu_item_history();

CREATE TRIGGER trg_tb_menu_item_history_immutable
    BEFORE UPDATE ON core.tb_menu_item_history
    FOR EACH ROW EXECUTE FUNCTION seg.tg_history_closed_interval_immutable();

-- ===========================================================================
-- core.tb_place_schedule — the source is deleted physically, so this trigger
-- is the only record a removed schedule ever had.
-- ===========================================================================
CREATE OR REPLACE FUNCTION core.tg_tb_place_schedule_history() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_ts timestamp := now();
BEGIN
    IF TG_OP = 'UPDATE'
       AND (to_jsonb(NEW) - 'dt_updated_at') = (to_jsonb(OLD) - 'dt_updated_at') THEN
        RETURN NEW;
    END IF;

    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE core.tb_place_schedule_history
           SET dt_valid_to = v_ts
         WHERE co_schedule = OLD.co_schedule AND dt_valid_to IS NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        INSERT INTO core.tb_place_schedule_history (
            co_schedule_history, co_schedule, co_place, nu_day_of_week,
            dt_open_time, dt_close_time, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_schedule, OLD.co_place, OLD.nu_day_of_week,
            OLD.dt_open_time, OLD.dt_close_time, 'DELETE',
            seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_place_schedule_history (
        co_schedule_history, co_schedule, co_place, nu_day_of_week,
        dt_open_time, dt_close_time, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_schedule, NEW.co_place, NEW.nu_day_of_week,
        NEW.dt_open_time, NEW.dt_close_time, TG_OP::public.change_operation,
        seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE TRIGGER trg_tb_place_schedule_history
    AFTER INSERT OR UPDATE OR DELETE ON core.tb_place_schedule
    FOR EACH ROW EXECUTE FUNCTION core.tg_tb_place_schedule_history();

CREATE TRIGGER trg_tb_place_schedule_history_immutable
    BEFORE UPDATE ON core.tb_place_schedule_history
    FOR EACH ROW EXECUTE FUNCTION seg.tg_history_closed_interval_immutable();

-- ===========================================================================
-- core.tb_review
-- ===========================================================================
CREATE OR REPLACE FUNCTION core.tg_tb_review_history() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_ts timestamp := now();
BEGIN
    IF TG_OP = 'UPDATE'
       AND (to_jsonb(NEW) - 'dt_updated_at') = (to_jsonb(OLD) - 'dt_updated_at') THEN
        RETURN NEW;
    END IF;

    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE core.tb_review_history
           SET dt_valid_to = v_ts
         WHERE co_review = OLD.co_review AND dt_valid_to IS NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        INSERT INTO core.tb_review_history (
            co_review_history, co_review, nu_rating, tx_comment, tx_status,
            dt_deleted_at, tx_operation, co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (
            gen_random_uuid(), OLD.co_review, OLD.nu_rating, OLD.tx_comment,
            OLD.tx_status, OLD.dt_deleted_at, 'DELETE',
            seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO core.tb_review_history (
        co_review_history, co_review, nu_rating, tx_comment, tx_status,
        dt_deleted_at, tx_operation, co_actor_user, dt_valid_from)
    VALUES (
        gen_random_uuid(), NEW.co_review, NEW.nu_rating, NEW.tx_comment,
        NEW.tx_status, NEW.dt_deleted_at, TG_OP::public.change_operation,
        seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE TRIGGER trg_tb_review_history
    AFTER INSERT OR UPDATE OR DELETE ON core.tb_review
    FOR EACH ROW EXECUTE FUNCTION core.tg_tb_review_history();

CREATE TRIGGER trg_tb_review_history_immutable
    BEFORE UPDATE ON core.tb_review_history
    FOR EACH ROW EXECUTE FUNCTION seg.tg_history_closed_interval_immutable();

-- ===========================================================================
-- seg.tb_user_role — composite key, physically deleted. This is what makes
-- "who was ADMIN on date X" answerable.
-- ===========================================================================
CREATE OR REPLACE FUNCTION seg.tg_tb_user_role_history() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_ts timestamp := now();
BEGIN
    IF TG_OP = 'UPDATE'
       AND (to_jsonb(NEW) - 'dt_updated_at') = (to_jsonb(OLD) - 'dt_updated_at') THEN
        RETURN NEW;
    END IF;

    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE seg.tb_user_role_history
           SET dt_valid_to = v_ts
         WHERE co_user = OLD.co_user AND co_role = OLD.co_role
           AND dt_valid_to IS NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        INSERT INTO seg.tb_user_role_history (
            co_user_role_history, co_user, co_role, tx_operation,
            co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (gen_random_uuid(), OLD.co_user, OLD.co_role, 'DELETE',
                seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO seg.tb_user_role_history (
        co_user_role_history, co_user, co_role, tx_operation,
        co_actor_user, dt_valid_from)
    VALUES (gen_random_uuid(), NEW.co_user, NEW.co_role,
            TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE TRIGGER trg_tb_user_role_history
    AFTER INSERT OR UPDATE OR DELETE ON seg.tb_user_role
    FOR EACH ROW EXECUTE FUNCTION seg.tg_tb_user_role_history();

CREATE TRIGGER trg_tb_user_role_history_immutable
    BEFORE UPDATE ON seg.tb_user_role_history
    FOR EACH ROW EXECUTE FUNCTION seg.tg_history_closed_interval_immutable();

-- ===========================================================================
-- seg.tb_role_permission — composite key, physically deleted. Permission drift
-- is a security question as much as an analytical one.
-- ===========================================================================
CREATE OR REPLACE FUNCTION seg.tg_tb_role_permission_history() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_ts timestamp := now();
BEGIN
    IF TG_OP = 'UPDATE'
       AND (to_jsonb(NEW) - 'dt_updated_at') = (to_jsonb(OLD) - 'dt_updated_at') THEN
        RETURN NEW;
    END IF;

    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE seg.tb_role_permission_history
           SET dt_valid_to = v_ts
         WHERE co_role = OLD.co_role AND co_permission = OLD.co_permission
           AND dt_valid_to IS NULL;
    END IF;

    IF TG_OP = 'DELETE' THEN
        INSERT INTO seg.tb_role_permission_history (
            co_role_permission_history, co_role, co_permission, tx_operation,
            co_actor_user, dt_valid_from, dt_valid_to)
        VALUES (gen_random_uuid(), OLD.co_role, OLD.co_permission, 'DELETE',
                seg.current_actor_user(), v_ts, v_ts);
        RETURN OLD;
    END IF;

    INSERT INTO seg.tb_role_permission_history (
        co_role_permission_history, co_role, co_permission, tx_operation,
        co_actor_user, dt_valid_from)
    VALUES (gen_random_uuid(), NEW.co_role, NEW.co_permission,
            TG_OP::public.change_operation, seg.current_actor_user(), v_ts);
    RETURN NEW;
END $$;

CREATE TRIGGER trg_tb_role_permission_history
    AFTER INSERT OR UPDATE OR DELETE ON seg.tb_role_permission
    FOR EACH ROW EXECUTE FUNCTION seg.tg_tb_role_permission_history();

CREATE TRIGGER trg_tb_role_permission_history_immutable
    BEFORE UPDATE ON seg.tb_role_permission_history
    FOR EACH ROW EXECUTE FUNCTION seg.tg_history_closed_interval_immutable();
