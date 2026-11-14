-- ============================================================================
-- 24 — place.manage_all — the capability that means "manages the whole catalogue".
-- Change: openspec/changes/social-discovery · Feature: odd/tasks/place-ownership.md
--
-- WHY A NEW PERMISSION
--   The places backoffice scopes rows by ownership: a caller sees only the places whose
--   co_owner_user is their own account, unless they manage the whole catalogue.
--
--   The obvious discriminator, admin.access, is WRONG and this was caught by driving the
--   running system rather than by reading code: admin.access means "may enter the
--   backoffice", and the seeded EDITOR role already holds it (06-seeds.sql). Scoping with it
--   would have handed every editor the entire catalogue including places they do not own —
--   the opposite of the decision.
--
--   So the capability gets its own code, granted to the roles that actually operate the
--   catalogue. Additive: no enum change, no rewrite, and a future "regional curator" role is
--   one more grant rather than a code change.
--
--   SUPERADMIN is granted explicitly here: its seed is a CROSS JOIN over the permissions that
--   existed when 06 ran, so a later permission does not inherit automatically.
-- ============================================================================

BEGIN;

INSERT INTO seg.tb_permission
    (co_permission, tx_code, tx_module, tx_name, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'place.manage_all', 'PLACES',
     'Gestionar todo el catálogo de lugares, no solo los propios', 'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;

INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
SELECT r.co_role, p.co_permission, now(), now()
FROM seg.tb_role r
JOIN seg.tb_permission p ON p.tx_code = 'place.manage_all' AND p.dt_deleted_at IS NULL
WHERE r.tx_name IN ('SUPERADMIN', 'ADMIN') AND r.dt_deleted_at IS NULL
ON CONFLICT (co_role, co_permission) DO NOTHING;

COMMIT;
