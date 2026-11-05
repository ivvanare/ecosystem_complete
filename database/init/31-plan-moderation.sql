-- ============================================================================
-- 31 — PLAN MODERATION — the capabilities a backoffice needs to take something out
-- of circulation without becoming a participant.
-- Change: openspec/changes/social-discovery · D21 (the panel administers, it does not join)
--
-- The hint from the SPA session is the rule: "de este lado sería poder moderar esas
-- invitaciones, moderar todo el sitio pero no interactuar como participante". So moderation is
-- its own capability, not admin.access — that permission means "may enter the backoffice", and
-- the seeded EDITOR role already holds it (the same trap the places backoffice hit).
--
-- Granting: ADMIN and SUPERADMIN. SUPERADMIN is granted explicitly because its seed is a
-- CROSS JOIN over the permissions that existed when 06 ran.
-- ============================================================================

BEGIN;

INSERT INTO seg.tb_permission
    (co_permission, tx_code, tx_module, tx_name, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'plan.moderate', 'PLANS', 'Moderar planes: listarlos y suspenderlos', 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'invitation.moderate', 'PLANS', 'Moderar invitaciones: listarlas y revocarlas', 'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;

INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
SELECT r.co_role, p.co_permission, now(), now()
FROM seg.tb_role r
JOIN seg.tb_permission p ON p.tx_code IN ('plan.moderate', 'invitation.moderate')
                        AND p.dt_deleted_at IS NULL
WHERE r.tx_name IN ('SUPERADMIN', 'ADMIN') AND r.dt_deleted_at IS NULL
ON CONFLICT (co_role, co_permission) DO NOTHING;

COMMIT;
