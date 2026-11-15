-- ============================================================================
-- 33 — DEVICE TOKENS — where a push can be delivered, and to whom it belongs.
-- Change: odd/tasks/device-notifications.md (P1 of the push plan)
--
-- A push token is not a session: it survives logins, it is handed out by FCM/APNs, and it
-- rotates without anyone asking. This table answers one question — "which device of which user
-- can be reached right now" — and nothing else.
--
-- Why the token is stored in the clear here, when refresh tokens are hashed: a refresh token is
-- verified (the server hashes what it received and compares), so it never needs to be readable.
-- A push token is USED: the sender has to hand it to the provider verbatim. Hashing it would
-- make it impossible to send to, which is the only thing it is for.
--
-- tx_platform is varchar with a CHECK, not an enum. It is a catalogue that grows (the web
-- client will want it) and an enum would force a migration per new value — the same reason
-- 30-notifications.sql recorded for tx_kind.
--
-- Purely additive: one empty table. Nothing existing changes, no row is rewritten.
-- ============================================================================

BEGIN;

CREATE TABLE seg.tb_device_token (
    co_device     uuid PRIMARY KEY,
    co_user       uuid NOT NULL,
    -- The registration token the provider handed to the client.
    tx_token      varchar NOT NULL,
    -- ANDROID | IOS. What the provider needs to know to route the message.
    tx_platform   varchar NOT NULL,
    -- Refreshed on every app start, so an old token can be told apart from a live one.
    dt_last_seen  timestamp NOT NULL,
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,
    dt_deleted_at timestamp,

    CONSTRAINT fk_tb_device_token_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user),
    CONSTRAINT ck_tb_device_token_platform CHECK (tx_platform IN ('ANDROID', 'IOS'))
);

-- [PARTIAL UNIQUE] — the model's rule for every soft-delete table: a plain unique would burn
-- the value forever, and a token that was retired must be registerable again.
CREATE UNIQUE INDEX ux_tb_device_token_token
    ON seg.tb_device_token (tx_token)
    WHERE dt_deleted_at IS NULL;

-- The read a notification does: every live device of one person.
CREATE INDEX ix_tb_device_token_user
    ON seg.tb_device_token (co_user)
    WHERE dt_deleted_at IS NULL;

COMMENT ON TABLE seg.tb_device_token IS
    'Dispositivos a los que se puede enviar un push, por usuario. No es una sesión: sobrevive a los logins y rota cuando el proveedor quiere. El token se guarda en claro porque el emisor tiene que entregarlo tal cual al proveedor; hashearlo lo haría inutilizable para lo único que sirve.';
COMMENT ON COLUMN seg.tb_device_token.co_device IS
    'Identificador del registro de dispositivo. Es lo que la app manda para dar de baja su propio token.';
COMMENT ON COLUMN seg.tb_device_token.co_user IS
    'Dueño actual del dispositivo. Si otro usuario inicia sesión en el mismo teléfono, el token se reasigna: un token pertenece a un device, no a una persona.';
COMMENT ON COLUMN seg.tb_device_token.tx_token IS
    'Token de registro del proveedor (FCM/APNs). Único entre las filas vivas: el índice es parcial para que un token retirado se pueda volver a registrar.';
COMMENT ON COLUMN seg.tb_device_token.tx_platform IS
    'ANDROID | IOS. Catálogo que crece (el cliente web va a quererlo), por eso varchar con CHECK y no un enum: un enum obliga a una migración por cada valor nuevo.';
COMMENT ON COLUMN seg.tb_device_token.dt_last_seen IS
    'Se actualiza en cada arranque de la app, para distinguir un token vivo de uno viejo.';
COMMENT ON COLUMN seg.tb_device_token.dt_created_at IS
    'Cuándo se registró el dispositivo por primera vez.';
COMMENT ON COLUMN seg.tb_device_token.dt_updated_at IS
    'Última modificación de la fila (un re-registro del mismo token mueve dt_last_seen).';
COMMENT ON COLUMN seg.tb_device_token.dt_deleted_at IS
    'Baja lógica del dispositivo (desinstalación, logout con revocación, o rechazo del proveedor). Conserva la fila para poder auditar qué se rechazó.';

COMMIT;
