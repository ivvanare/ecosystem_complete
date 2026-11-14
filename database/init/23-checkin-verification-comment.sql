-- ============================================================================
-- 23 — ENUM COMMENT — public.checkin_verification documented.
-- Follows 21 (the type) and 22 (the column comments).
--
-- The drift check (verify_base_model.sql, S24) requires a COMMENT on every table, every
-- column and every enum type in the model. 22 covered tables and columns; the type itself
-- was still undocumented, so the check kept failing. This file closes that gap — again as a
-- new ledger file rather than by editing 21, because an applied migration is never edited.
-- ============================================================================

BEGIN;

COMMENT ON TYPE public.checkin_verification IS
    'Cómo se estableció la presencia en un check-in (decisión Q1). SELF_REPORTED: el cliente afirmó presencia sin coordenada usable. PROXIMITY: una coordenada se validó contra el radio del lugar y luego se descartó; ni la coordenada ni la distancia se almacenan (D11). Conjunto cerrado a propósito: agregar SIGNED_TOKEN más adelante es aditivo, mientras que un veredicto en texto libre sería un bug de privilegios silencioso.';

COMMIT;
