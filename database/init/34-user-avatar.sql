-- ============================================================================
-- 34 — USER AVATAR — the only picture a person publishes about themselves.
-- Change: odd/tasks/user-avatar.md
--
-- One nullable column holding the URL, not the bytes: the same rule place photos follow. The
-- storage seam already exists (internal/storage), so S3 later is one more implementation, not a
-- schema change.
--
-- Purely additive: the column is nullable and has no default, so no existing row is rewritten.
-- Every current account reads NULL, which the contract already treats as "no avatar" — the client
-- renders initials, not a broken image.
-- ============================================================================

BEGIN;

ALTER TABLE seg.tb_user
    ADD COLUMN tx_avatar_url varchar;

COMMENT ON COLUMN seg.tb_user.tx_avatar_url IS
    'URL pública de la foto de perfil (relativa al origen del API, /media/...). NULL es un estado real: la cuenta nunca subió una y el cliente dibuja iniciales. Los bytes nunca viven en la base.';

COMMIT;
