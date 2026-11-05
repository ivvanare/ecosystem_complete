-- ============================================================================
-- 30 — NOTIFICATIONS — per-kind preferences, and the first delivered trigger (S13).
-- Change: openspec/changes/social-discovery (Phase 4)
--
-- S13 requires notifications derived from triggers a user can ENUMERATE, each individually
-- disableable. This table is the "disableable" half: a row means "this person turned this
-- kind off". Absence means on, so a new kind does not need a backfill for every user.
--
-- Delivery is email, through the queue the service already owns. Push waits for the mobile
-- client, and a channel nobody can receive is not a channel.
--
-- tx_kind is varchar with the dot convention, not an enum: the catalogue of kinds grows with
-- every feature, and 13-comments.sql already recorded why a growing catalogue is not an enum
-- (an enum forces a migration per new value).
-- ============================================================================

BEGIN;

CREATE TABLE core.tb_notification_optout (
    co_user       uuid NOT NULL,
    tx_kind       varchar NOT NULL,
    dt_created_at timestamp NOT NULL,

    CONSTRAINT pk_tb_notification_optout PRIMARY KEY (co_user, tx_kind),
    CONSTRAINT fk_tb_notification_optout_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user),
    CONSTRAINT ck_tb_notification_optout_kind CHECK (tx_kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$')
);

COMMENT ON TABLE core.tb_notification_optout IS
    'Tipos de notificación que un usuario desactivó (S13). La ausencia de fila significa activado: así un tipo nuevo no requiere rellenar una preferencia por cada usuario. El conjunto de tipos vive en el código, con convención punto, no en un enum: crece con cada funcionalidad.';
COMMENT ON COLUMN core.tb_notification_optout.co_user IS
    'Usuario que desactivó el tipo. PK compuesta con tx_kind.';
COMMENT ON COLUMN core.tb_notification_optout.tx_kind IS
    'Tipo de notificación, por ejemplo invitation.received. El CHECK solo valida la forma de la convención; la pertenencia al catálogo la valida el borde.';
COMMENT ON COLUMN core.tb_notification_optout.dt_created_at IS
    'Cuándo lo desactivó. Volver a activarlo borra la fila.';

COMMIT;
