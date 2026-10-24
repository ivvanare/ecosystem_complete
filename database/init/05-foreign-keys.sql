-- ============================================================================
-- RELATIONS — all 17 FKs are real, including the cross-schema ones (rule 2).
-- Canonical spec: database/diagram_base.txt (section "RELACIONES").
-- No ON DELETE/UPDATE actions: excluded on purpose by the diagram ("son una
-- decisión de negocio aparte"), so PostgreSQL default NO ACTION applies.
-- seg.tb_security_event intentionally has NO FK to tb_user: the audit must
-- survive user deletion (rule 11).
-- ============================================================================
\connect melzone

-- ---- seg: identity and permissions ----

ALTER TABLE seg.tb_refresh_token
    ADD CONSTRAINT fk_tb_refresh_token_user
    FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user);

ALTER TABLE seg.tb_user_token
    ADD CONSTRAINT fk_tb_user_token_user
    FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user);

ALTER TABLE seg.tb_user_identity
    ADD CONSTRAINT fk_tb_user_identity_user
    FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user);

ALTER TABLE seg.tb_user_role
    ADD CONSTRAINT fk_tb_user_role_user
    FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user);

ALTER TABLE seg.tb_user_role
    ADD CONSTRAINT fk_tb_user_role_role
    FOREIGN KEY (co_role) REFERENCES seg.tb_role (co_role);

ALTER TABLE seg.tb_role_permission
    ADD CONSTRAINT fk_tb_role_permission_role
    FOREIGN KEY (co_role) REFERENCES seg.tb_role (co_role);

ALTER TABLE seg.tb_role_permission
    ADD CONSTRAINT fk_tb_role_permission_permission
    FOREIGN KEY (co_permission) REFERENCES seg.tb_permission (co_permission);

-- ---- core -> seg: cross-schema, real FKs ----

-- `>?` in DBML marks the optional side: co_owner_user is nullable, a curated
-- place has no owner.
ALTER TABLE core.tb_place
    ADD CONSTRAINT fk_tb_place_owner_user
    FOREIGN KEY (co_owner_user) REFERENCES seg.tb_user (co_user);

ALTER TABLE core.tb_review
    ADD CONSTRAINT fk_tb_review_user
    FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user);

ALTER TABLE core.tb_route
    ADD CONSTRAINT fk_tb_route_user
    FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user);

-- ---- core: domain ----

ALTER TABLE core.tb_place_schedule
    ADD CONSTRAINT fk_tb_place_schedule_place
    FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place);

ALTER TABLE core.tb_place_photo
    ADD CONSTRAINT fk_tb_place_photo_place
    FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place);

ALTER TABLE core.tb_menu
    ADD CONSTRAINT fk_tb_menu_place
    FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place);

ALTER TABLE core.tb_menu_item
    ADD CONSTRAINT fk_tb_menu_item_menu
    FOREIGN KEY (co_menu) REFERENCES core.tb_menu (co_menu);

ALTER TABLE core.tb_review
    ADD CONSTRAINT fk_tb_review_place
    FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place);

ALTER TABLE core.tb_route_place
    ADD CONSTRAINT fk_tb_route_place_route
    FOREIGN KEY (co_route) REFERENCES core.tb_route (co_route);

ALTER TABLE core.tb_route_place
    ADD CONSTRAINT fk_tb_route_place_place
    FOREIGN KEY (co_place) REFERENCES core.tb_place (co_place);
