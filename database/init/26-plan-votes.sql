-- ============================================================================
-- 26 — PLAN VOTES — collaborative ordering, Spotify style (S9/D9).
-- Change: openspec/changes/social-discovery (Phase 3b) · Feature: odd/tasks/plan-collaboration.md
--
-- core.tb_route_place already models the ordered itinerary (composite PK + nu_order). A vote
-- is a separate fact about a membership: one vote per member per place per plan, reversible
-- without an update storm.
--
-- The vote references (co_route, co_place) — not just co_place — so a vote cannot exist for a
-- place that is not in the plan, and removing the place from the plan removes its votes. That
-- is the whole reason the FK is composite.
--
-- NOT IN THE POINT-IN-TIME SET: a vote is a reversible opinion, not entity state.
-- ============================================================================

BEGIN;

CREATE TABLE core.tb_route_place_vote (
    co_route      uuid NOT NULL,
    co_place      uuid NOT NULL,
    co_user       uuid NOT NULL,
    nu_vote       smallint NOT NULL,
    dt_created_at timestamp NOT NULL,
    dt_updated_at timestamp NOT NULL,

    CONSTRAINT pk_tb_route_place_vote PRIMARY KEY (co_route, co_place, co_user),
    CONSTRAINT ck_tb_route_place_vote_value CHECK (nu_vote IN (-1, 1)),
    -- The composite FK is the rule: you can only vote for a place that is in this plan.
    CONSTRAINT fk_tb_route_place_vote_place FOREIGN KEY (co_route, co_place)
        REFERENCES core.tb_route_place (co_route, co_place) ON DELETE CASCADE,
    CONSTRAINT fk_tb_route_place_vote_user FOREIGN KEY (co_user) REFERENCES seg.tb_user (co_user)
);

CREATE INDEX ix_tb_route_place_vote_place ON core.tb_route_place_vote (co_route, co_place);

COMMENT ON TABLE core.tb_route_place_vote IS
    'Voto de un miembro sobre un lugar de un plan (S9/D9). Un voto por miembro, lugar y plan, garantizado por la PK compuesta: cambiar de opinión es un UPDATE, no un voto nuevo. nu_vote ∈ {-1, 1} (abajo/arriba). La FK compuesta a tb_route_place impide votar un lugar que no está en el plan y borra los votos al sacar el lugar.';
COMMENT ON COLUMN core.tb_route_place_vote.co_route IS
    'Plan al que pertenece el voto. Parte de la FK compuesta al itinerario.';
COMMENT ON COLUMN core.tb_route_place_vote.co_place IS
    'Lugar votado. Debe estar en el plan: la FK compuesta (co_route, co_place) lo exige.';
COMMENT ON COLUMN core.tb_route_place_vote.co_user IS
    'Miembro que vota. FK real a seg.tb_user; el servicio exige además que sea miembro del plan.';
COMMENT ON COLUMN core.tb_route_place_vote.nu_vote IS
    'Sentido del voto: -1 abajo, 1 arriba. Sin 0: no votar es no tener fila, así el ranking suma sin distinguir "no votó" de "votó neutral".';
COMMENT ON COLUMN core.tb_route_place_vote.dt_created_at IS
    'Cuándo votó por primera vez este lugar en este plan.';
COMMENT ON COLUMN core.tb_route_place_vote.dt_updated_at IS
    'Último cambio de sentido del voto.';

COMMIT;
