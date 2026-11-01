-- ============================================================================
-- UTC CONVENTION — every timestamp in this database stores UTC, pinned at the
-- database level so it never depends on the role name or the server config.
-- Spec: database/diagram_base.txt (model rule 12).
--
-- WHY A DATABASE-LEVEL SETTING
--   All timestamp columns are `timestamp without time zone` and all of them
--   store UTC. Until now that was only true by accident of the container
--   booting with timezone=UTC: a role-level setting, a postgresql.conf change
--   or a client SET would have shifted now() and every stored instant with it.
--   `ALTER DATABASE ... SET timezone TO 'UTC'` makes every NEW session inherit
--   UTC no matter which role or client connects. Running it through a DO block
--   with dynamic SQL keeps the file free of the database name, so it applies
--   unchanged wherever the applier runs it.
--
-- THE ROW OWNS THE ZONE, NEVER THE SERVER
--   UTC is only the STORAGE convention. The timezone that makes opening hours
--   interpretable — the one "open now" buckets by — is a property of the ROW
--   (core.tb_place.tx_timezone, added in 11-locale-columns.sql), never of the
--   server. That is why multi-country does not mean multi-database: one UTC
--   timeline in one database, and each row carries the zone that gives its
--   local times meaning.
-- ============================================================================
\connect melia

DO $$
BEGIN
    EXECUTE format('ALTER DATABASE %I SET timezone TO ''UTC''', current_database());
END $$;
