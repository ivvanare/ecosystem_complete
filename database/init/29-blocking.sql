-- ============================================================================
-- 29 — BLOCKING — the gate the social graph needs before public exposure (Q2).
-- Change: openspec/changes/social-discovery · Decision Q2 (resolved as a launch gate)
--
-- Decision Q2 explicitly said block is NOT a feature of P2 — it is the condition for
-- exposing the graph publicly at all. It is built now because it is cheap now: once people
-- have built a graph, retrofitting a block means changing every viewer-relative projection
-- while they watch their feeds change.
--
-- WHY A RELATION AND NOT A FLAG ON THE FOLLOW ROW: blocking is not the inverse of following.
-- A block must hold even when neither person follows the other, must survive an unfollow, and
-- must apply in both directions — the blocked person must not see the blocker's content
-- either. A flag can express none of that.
--
-- NOT IN THE POINT-IN-TIME SET: a block is a current relationship, not entity state with a
-- history worth reconstructing.
-- ============================================================================

BEGIN;

CREATE TABLE core.tb_block (
    co_blocker     uuid NOT NULL,
    co_blocked     uuid NOT NULL,
    dt_created_at  timestamp NOT NULL,

    CONSTRAINT pk_tb_block PRIMARY KEY (co_blocker, co_blocked),
    CONSTRAINT ck_tb_block_not_self CHECK (co_blocker <> co_blocked),
    CONSTRAINT fk_tb_block_blocker FOREIGN KEY (co_blocker) REFERENCES seg.tb_user (co_user),
    CONSTRAINT fk_tb_block_blocked FOREIGN KEY (co_blocked) REFERENCES seg.tb_user (co_user)
);

-- Both directions are read: "whom do I block" and "who blocks me" are one predicate.
CREATE INDEX ix_tb_block_blocked ON core.tb_block (co_blocked);

COMMENT ON TABLE core.tb_block IS
    'Bloqueo entre usuarios (decisión Q2). Relación, no flag: se aplica en las dos direcciones (el bloqueado tampoco ve al que bloquea), sobrevive a un unfollow y no depende de que exista un follow. El bloqueo propio lo rechaza ck_tb_block_not_self, no solo el servicio.';
COMMENT ON COLUMN core.tb_block.co_blocker IS
    'Quién bloquea. Parte de la PK compuesta con co_blocked.';
COMMENT ON COLUMN core.tb_block.co_blocked IS
    'Quién queda bloqueado. Indexado aparte porque el filtro se evalúa en ambas direcciones.';
COMMENT ON COLUMN core.tb_block.dt_created_at IS
    'Cuándo se bloqueó. Sin dt_updated_at ni borrado lógico: desbloquear borra la fila y volver a bloquear es un bloqueo nuevo.';

COMMIT;
