-- ============================================================================
-- 28 — TASTE PROFILE — a derived cache of explicit signals (S12/D11).
-- Change: openspec/changes/social-discovery (Phase 4) · Feature: odd/tasks/taste-profile.md
--
-- One row per user, REPLACED WHOLESALE by the worker. Not append-only and not history: it is a
-- cache with a policy, not a record of events. Deleting a row is a complete and safe operation
-- — the next batch rebuilds it.
--
-- WHAT IT MAY CONTAIN: aggregate affinities only (categories, coarse zones). Never a raw
-- coordinate, never a per-event log, never IP-derived location: D11 forbids storing where a
-- person was, and a profile is an opinion about tastes, not a trail.
--
-- NOT IN THE POINT-IN-TIME SET: it changes wholesale on every batch, so history would record
-- noise rather than state.
-- ============================================================================

BEGIN;

CREATE TABLE core.tb_user_taste_profile (
    co_user       uuid PRIMARY KEY,
    js_profile    jsonb NOT NULL,
    nu_version    int NOT NULL DEFAULT 1,
    dt_computed_at timestamp NOT NULL,

    CONSTRAINT fk_tb_user_taste_profile_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user)
);

COMMENT ON TABLE core.tb_user_taste_profile IS
    'Perfil de gustos por usuario (S12/D11): un agregado derivado de señales explícitas, reemplazado entero por el worker. No es historial ni append-only: es un caché con política. Borrar la fila es una operación completa y segura, y el siguiente batch la reconstruye. Su ausencia nunca es un error: la recomendación cae a trending no personalizado.';
COMMENT ON COLUMN core.tb_user_taste_profile.co_user IS
    'Usuario dueño del perfil. PK y FK real a seg.tb_user: un perfil por persona.';
COMMENT ON COLUMN core.tb_user_taste_profile.js_profile IS
    'Agregados, nunca datos crudos: afinidad por categoría y por zona gruesa (celda H3 de baja resolución). Sin coordenadas, sin log de eventos, sin datos derivados de IP (D11).';
COMMENT ON COLUMN core.tb_user_taste_profile.nu_version IS
    'Versión del cálculo. Permite cambiar la forma del perfil sin confundir filas viejas con nuevas.';
COMMENT ON COLUMN core.tb_user_taste_profile.dt_computed_at IS
    'Cuándo lo calculó el worker. Un perfil viejo se puede reconocer sin adivinar.';

COMMIT;
