-- ============================================================================
-- 22 — SOCIAL GRAPH COMMENTS — every column of the two new tables documented.
-- Follows database/init/21-social-graph.sql.
--
-- WHY THIS IS A SEPARATE FILE
--   21 was already recorded in seg.tb_schema_migration when the drift check
--   (verify_base_model.sql, S24) reported the new columns as undocumented. An applied
--   migration is never edited — the ledger is environment state and rewriting it would
--   make the history lie about what a database actually ran. The fix ships as its own
--   file, which is exactly the discipline that caught the omission.
-- ============================================================================

BEGIN;

COMMENT ON COLUMN core.tb_follow.co_follower IS
    'Cuenta que sigue. Parte de la PK compuesta (co_follower, co_followed); el grafo es asimétrico, una dirección por fila (D6).';
COMMENT ON COLUMN core.tb_follow.co_followed IS
    'Cuenta seguida. Indexada por separado porque "quién me sigue" es una lectura de primera clase.';
COMMENT ON COLUMN core.tb_follow.dt_created_at IS
    'Cuándo se creó la arista. Sin dt_updated_at ni borrado lógico: con PK compuesta una fila borrada lógicamente impediría volver a seguir a esa persona.';

COMMENT ON COLUMN core.tb_checkin.co_checkin IS
    'UUID del check-in. Append-only: no hay camino de actualización ni borrado lógico.';
COMMENT ON COLUMN core.tb_checkin.co_user IS
    'Quién hizo el check-in. FK real a seg.tb_user (entre esquemas).';
COMMENT ON COLUMN core.tb_checkin.co_place IS
    'Lugar donde se declaró la presencia. FK a core.tb_place.';
COMMENT ON COLUMN core.tb_checkin.tx_verification IS
    'Cómo se estableció la presencia (decisión Q1): SELF_REPORTED o PROXIMITY. La coordenada que probó la proximidad se valida en el borde y se descarta: nunca se guarda la coordenada ni la distancia (D11).';
COMMENT ON COLUMN core.tb_checkin.js_metadata IS
    'Metadatos libres del cliente (por ejemplo, origen). No debe contener coordenadas: la ubicación no se almacena (D11).';
COMMENT ON COLUMN core.tb_checkin.dt_created_at IS
    'Instante del check-in. Inmutable: el check-in es un hecho histórico, no un estado.';
COMMENT ON COLUMN core.tb_checkin.dt_day IS
    'Día del check-in, GENERATED ALWAYS AS (dt_created_at::date) STORED. Existe para poder indexar la regla de negocio: un check-in por persona, lugar y día (S10).';

COMMIT;
