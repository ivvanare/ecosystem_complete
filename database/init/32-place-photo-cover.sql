-- ============================================================================
-- 32 — ONE COVER PER PLACE — the rule the diagram declared but never enforced.
-- Change: openspec/changes/social-discovery · Feature: odd/tasks/place-photos.md
--
-- database/diagram_base.txt already says "Una sola COVER por lugar: regla de aplicación" on
-- core.tb_place_photo.tx_type. Nothing enforced it, so three covers were one insert away and
-- nothing would have noticed: the first one wins in some queries and the last in others.
--
-- The rule belongs in the database for the same reason self-follow does: an application rule
-- that nothing else can check is a rule that holds until a second writer appears.
-- ============================================================================

BEGIN;

CREATE UNIQUE INDEX ux_tb_place_photo_cover
    ON core.tb_place_photo (co_place)
    WHERE tx_type = 'COVER' AND dt_deleted_at IS NULL;

COMMENT ON INDEX core.ux_tb_place_photo_cover IS
    'Una sola foto COVER viva por lugar. Materializa la regla que el diagrama declaraba como "de aplicación": sin el índice, dos portadas conviven y distintas consultas eligen una distinta. Las GALLERY no se limitan.';

COMMIT;
