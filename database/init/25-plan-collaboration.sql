-- ============================================================================
-- 25 — PLAN COLLABORATION — visibility, scheduling, party size and membership.
-- Change: openspec/changes/social-discovery (Phase 3a, S7/S8/S25)
-- Feature: odd/tasks/plan-collaboration.md
--
-- A public festival and a private trip are the same entity with different doors (D7), so
-- visibility is an attribute of the plan and membership is a relation. The role lives on the
-- member row, not on the plan: the creator reference in tb_route.co_user stays untouched so
-- existing reads keep working.
--
-- NOT IN THE POINT-IN-TIME SET: only four history tables exist (place, menu_item,
-- place_schedule, review). Nothing here needs a history payload or a trigger replacement —
-- verified against the database before writing this file.
--
-- A backfill is included because any environment with existing routes must not end up with
-- ownerless plans: the creator becomes OWNER.
-- ============================================================================

BEGIN;

-- Closed sets: these values participate in authorization checks, and a typo in free text
-- would be a silent privilege bug.
CREATE TYPE public.plan_visibility AS ENUM ('PRIVATE', 'PUBLIC');
CREATE TYPE public.plan_role AS ENUM ('OWNER', 'EDITOR', 'VIEWER');

COMMENT ON TYPE public.plan_visibility IS
    'Quién puede ver el plan (D7). PRIVATE: solo miembros. PUBLIC: cualquiera puede verlo y unirse. El default es PRIVATE a propósito: un plan nunca se vuelve público por omisión.';
COMMENT ON TYPE public.plan_role IS
    'Rol de un miembro dentro de un plan (S7). OWNER: crea, cambia visibilidad y ordena; EDITOR: ordena y agrega lugares; VIEWER: lee y vota. Conjunto cerrado: los roles participan de chequeos de autorización.';

-- ---------------------------------------------------------------------------
-- core.tb_route — the plan gains a door, a date and a party size.
-- ---------------------------------------------------------------------------
ALTER TABLE core.tb_route
    ADD COLUMN tx_visibility public.plan_visibility NOT NULL DEFAULT 'PRIVATE',
    ADD COLUMN dt_scheduled_at timestamp,
    ADD COLUMN nu_party_size smallint
        CONSTRAINT ck_tb_route_party_size CHECK (nu_party_size BETWEEN 1 AND 50);

COMMENT ON COLUMN core.tb_route.tx_visibility IS
    'Quién puede ver y unirse al plan (D7). Default PRIVATE: la puerta cerrada es el valor seguro y publicar es un acto explícito del OWNER.';
COMMENT ON COLUMN core.tb_route.dt_scheduled_at IS
    'Cuándo ocurre el plan. NULL es un estado real ("todavía sin fecha"), nunca "hoy": un plan puede existir antes de decidirse la fecha, y el reveal de una invitación sorpresa necesita este instante.';
COMMENT ON COLUMN core.tb_route.nu_party_size IS
    'Cuánta gente va. Numérico y no enum porque "solo", "en pareja" y un festival de 50 son 1, 2 y 50; un enum necesitaría una migración la primera vez que un grupo es de cuatro.';

-- ---------------------------------------------------------------------------
-- core.tb_route_member — the relation that carries the role.
-- ---------------------------------------------------------------------------
CREATE TABLE core.tb_route_member (
    co_route       uuid NOT NULL,
    co_user        uuid NOT NULL,
    tx_role        public.plan_role NOT NULL,
    dt_created_at  timestamp NOT NULL,
    dt_updated_at  timestamp NOT NULL,

    CONSTRAINT pk_tb_route_member PRIMARY KEY (co_route, co_user),
    CONSTRAINT fk_tb_route_member_route FOREIGN KEY (co_route) REFERENCES core.tb_route (co_route),
    CONSTRAINT fk_tb_route_member_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user)
);

-- "Which plans am I in" is a first-class read.
CREATE INDEX ix_tb_route_member_user ON core.tb_route_member (co_user);
CREATE INDEX ix_tb_route_member_role ON core.tb_route_member (co_route, tx_role);

COMMENT ON TABLE core.tb_route_member IS
    'Membresía de un plan con su rol (S7). PK compuesta (co_route, co_user), sin borrado lógico: salir del plan es borrar la fila, y una fila borrada lógicamente impediría volver a unirse. El creador se registra como OWNER en la misma transacción que crea el plan: un plan sin dueño es inválido.';
COMMENT ON COLUMN core.tb_route_member.co_route IS
    'Plan al que pertenece la fila. FK a core.tb_route.';
COMMENT ON COLUMN core.tb_route_member.co_user IS
    'Miembro del plan. FK real a seg.tb_user (entre esquemas). Indexado por separado: "mis planes" es una lectura de primera clase.';
COMMENT ON COLUMN core.tb_route_member.tx_role IS
    'Rol del miembro (S7). El self-join a un plan público otorga VIEWER siempre; promover a EDITOR es una acción del OWNER (D8).';
COMMENT ON COLUMN core.tb_route_member.dt_created_at IS
    'Cuándo se unió al plan.';
COMMENT ON COLUMN core.tb_route_member.dt_updated_at IS
    'Último cambio de rol.';

-- ---------------------------------------------------------------------------
-- Backfill: every existing plan gets its creator as OWNER.
-- ---------------------------------------------------------------------------
INSERT INTO core.tb_route_member (co_route, co_user, tx_role, dt_created_at, dt_updated_at)
SELECT co_route, co_user, 'OWNER', now(), now()
FROM core.tb_route
WHERE co_user IS NOT NULL
ON CONFLICT (co_route, co_user) DO NOTHING;

COMMIT;
