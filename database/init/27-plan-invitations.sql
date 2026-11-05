-- ============================================================================
-- 27 — PLAN INVITATIONS — a lifecycle with a read-enforced expiry (S19/D16).
-- Change: openspec/changes/social-discovery · Feature: odd/tasks/plan-invitations.md
--
-- An invitation is not a flag and not a member row with a status: it has its own lifecycle
-- (PENDING -> ACCEPTED | DECLINED | EXPIRED | REVOKED), its own expiry, and the host's
-- content. Accepting it creates the membership; declining it creates nothing.
--
-- THE READ PREDICATE IS THE TRUTH. Every read and every accept requires
-- dt_expires_at > now(); the scheduled sweeper only flips stale rows to EXPIRED for hygiene.
-- Inverting that order is how an expired invitation stays acceptable for a month after a
-- deploy that broke the cron.
--
-- NOT IN THE POINT-IN-TIME SET: an invitation is a lifecycle record, not entity state that
-- changes in place.
-- ============================================================================

BEGIN;

CREATE TYPE public.plan_invitation_status AS ENUM ('PENDING', 'ACCEPTED', 'DECLINED', 'EXPIRED', 'REVOKED');
CREATE TYPE public.plan_invitation_mode AS ENUM ('PLAIN', 'SURPRISE');
CREATE TYPE public.plan_reveal_rule AS ENUM ('AT_SCHEDULED', 'ON_ACCEPT');

COMMENT ON TYPE public.plan_invitation_status IS
    'Estado de una invitación (S19). PENDING: viva y respondible si no venció. ACCEPTED: el invitado aceptó y es miembro. DECLINED: rechazo terminal, no crea membresía. EXPIRED: barrida por el sweeper; el predicado de lectura ya la consideraba vencida antes. REVOKED: el anfitrión la retiró.';
COMMENT ON TYPE public.plan_invitation_mode IS
    'Modo de la invitación (D20, decisión Q5). PLAIN: llega normal, con el nombre del local. SURPRISE: el invitado ve el mensaje, la fecha y hora, la pista y la zona, pero no el nombre, la dirección ni las coordenadas del lugar.';
COMMENT ON TYPE public.plan_reveal_rule IS
    'Cuándo se destapa el lugar de una invitación SURPRISE (D20). AT_SCHEDULED: a la hora del plan, que es lo que hace durar la sorpresa. ON_ACCEPT: cuando el invitado acepta, para quien necesita prepararse.';

CREATE TABLE core.tb_plan_invitation (
    co_invitation   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    co_route        uuid NOT NULL,
    co_user         uuid NOT NULL,
    co_inviter      uuid NOT NULL,
    tx_status       public.plan_invitation_status NOT NULL DEFAULT 'PENDING',
    tx_mode         public.plan_invitation_mode NOT NULL DEFAULT 'PLAIN',
    tx_message      varchar(500),
    tx_hint         varchar(200),
    tx_reveal       public.plan_reveal_rule NOT NULL DEFAULT 'AT_SCHEDULED',
    dt_expires_at   timestamp NOT NULL,
    dt_created_at   timestamp NOT NULL,
    dt_responded_at timestamp,

    CONSTRAINT fk_tb_plan_invitation_route FOREIGN KEY (co_route) REFERENCES core.tb_route (co_route),
    CONSTRAINT fk_tb_plan_invitation_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user),
    CONSTRAINT fk_tb_plan_invitation_inviter FOREIGN KEY (co_inviter) REFERENCES seg.tb_user (co_user),
    CONSTRAINT ck_tb_plan_invitation_expiry CHECK (dt_expires_at > dt_created_at)
);

-- One live invitation per person per plan; past attempts remain as history.
CREATE UNIQUE INDEX ux_tb_plan_invitation_live
    ON core.tb_plan_invitation (co_route, co_user)
    WHERE tx_status = 'PENDING';

CREATE INDEX ix_tb_plan_invitation_user ON core.tb_plan_invitation (co_user, tx_status);
CREATE INDEX ix_tb_plan_invitation_route ON core.tb_plan_invitation (co_route, tx_status);

COMMENT ON TABLE core.tb_plan_invitation IS
    'Invitación a un plan, con ciclo de vida propio (S19/D16). Una invitación viva por persona y plan (índice único parcial sobre PENDING); los intentos pasados quedan como historial. Aceptar crea la membresía VIEWER en la misma transacción; rechazar no crea nada.';
COMMENT ON COLUMN core.tb_plan_invitation.co_invitation IS
    'UUID de la invitación.';
COMMENT ON COLUMN core.tb_plan_invitation.co_route IS
    'Plan al que invita. FK a core.tb_route.';
COMMENT ON COLUMN core.tb_plan_invitation.co_user IS
    'Invitado. Solo usuarios registrados: no hay token compartible en esta fase.';
COMMENT ON COLUMN core.tb_plan_invitation.co_inviter IS
    'Quién invita: un miembro EDITOR u OWNER del plan.';
COMMENT ON COLUMN core.tb_plan_invitation.tx_status IS
    'Estado del ciclo de vida. El predicado de lectura trata como vencida cualquier PENDING con dt_expires_at <= now(), aunque el sweeper no haya corrido.';
COMMENT ON COLUMN core.tb_plan_invitation.tx_mode IS
    'PLAIN o SURPRISE (decisión Q5). En SURPRISE el invitado siempre ve el plan, el lugar y el día; lo que se destapa es el mensaje y la pista del anfitrión, no el destino.';
COMMENT ON COLUMN core.tb_plan_invitation.tx_message IS
    'Mensaje del anfitrión para el invitado (≤500). Lo escribe una persona, nunca lo genera el servicio.';
COMMENT ON COLUMN core.tb_plan_invitation.tx_hint IS
    'Pista del anfitrión (≤200), por ejemplo "llevá atuendo cómodo, es un rooftop". Opcional.';
COMMENT ON COLUMN core.tb_plan_invitation.tx_reveal IS
    'Regla de destape para SURPRISE: a la hora del plan o al aceptar.';
COMMENT ON COLUMN core.tb_plan_invitation.dt_expires_at IS
    'Vencimiento, impuesto por el predicado de lectura. No puede superar la fecha del plan cuando esta existe: invitar a un plan ya pasado es ruido.';
COMMENT ON COLUMN core.tb_plan_invitation.dt_created_at IS
    'Cuándo se envió la invitación.';
COMMENT ON COLUMN core.tb_plan_invitation.dt_responded_at IS
    'Cuándo respondió el invitado. NULL mientras siga PENDING.';

COMMIT;
