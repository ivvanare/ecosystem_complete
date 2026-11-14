-- ============================================================================
-- 20 — REVIEW SLOT — moderation no longer blocks the author from reviewing again.
-- Spec: database/diagram_base.txt · Change: openspec/changes/social-discovery
-- (Q7 resolved) · Feature: odd/tasks/social-graph.md (P2)
--
-- THE BUG THIS FIXES
--   ux_tb_review_user_place_active was:
--       UNIQUE (co_user, co_place) WHERE dt_deleted_at IS NULL
--   so a review that moderation set to MODERATED — but which was not soft-deleted —
--   still occupied the slot. The author could never review that place again: every
--   new POST answered 409. The diagram's comment claimed the filter freed moderated
--   *and* deleted reviews; it only freed deleted ones. That was inert until the
--   moderation route existed, which is why it survived review.
--
-- WHAT CHANGES
--   The predicate gains tx_status = 'ACTIVE':
--       UNIQUE (co_user, co_place) WHERE dt_deleted_at IS NULL AND tx_status = 'ACTIVE'
--   A moderated or inactive review leaves the slot free, so the author may post a
--   new one while the moderated record stays visible to them and to moderators. The
--   history rows are untouched: moderation remains an UPDATE in the point-in-time
--   table, and nothing is rewritten.
--
-- WHY THIS IS SAFE
--   The new predicate indexes FEWER rows than the old one (it adds a conjunct), so it
--   cannot introduce a uniqueness violation the old index tolerated. Verified on the
--   live database before this file was written: zero duplicate (co_user, co_place)
--   pairs among rows with dt_deleted_at IS NULL AND tx_status = 'ACTIVE'.
--
--   The index is rebuilt under the same name on purpose: code that names the
--   constraint in an error mapping (the reviews store maps 23505 on this name to 409)
--   keeps working, and there is no window with two indexes on the same columns.
-- ============================================================================

BEGIN;

DROP INDEX core.ux_tb_review_user_place_active;

CREATE UNIQUE INDEX ux_tb_review_user_place_active
    ON core.tb_review (co_user, co_place)
    WHERE dt_deleted_at IS NULL AND tx_status = 'ACTIVE';

COMMENT ON INDEX core.ux_tb_review_user_place_active IS
    'One ACTIVE review per user and place. The predicate covers only live, ACTIVE rows so a MODERATED review frees the slot and the author may review that place again, while the moderated record stays readable. Rebuilt in migration 20 (Q7); the name is load-bearing: the reviews store maps 23505 on it to review_already_exists.';

COMMIT;
