# Archive Report: social-discovery

## Change Summary

**What was done**: The product itself, in five phases: geospatial foundation, discovery and the
reviews write path, the social graph, collaborative plans with invitations, and the taste profile
with notifications. Later, live vote propagation went on top of the same design.
**Why**: "melia" is a social discovery product — nearby places, plans with friends, and "was
here" — and the database model already described it while the API had only auth.
**How**: contract first on every route (`openapi.yaml` + `API.md` before the handler), one ledger
file per schema change applied by the service, one package per concern behind narrow interfaces,
and a suite that needs no PostgreSQL, Redis, RabbitMQ or Soketi.

## Delivered

- **Ledgers 15–32**: PostGIS and the derived `co_location`, `tx_region`, search columns and
  trigram indexes, place photos and the single-cover partial index, plans (visibility, members,
  votes, invitations), `tb_follow`, `tb_checkin` with its generated day bucket, taste profile,
  blocking, notification opt-outs, moderation and the place photo cover rule.
- **Packages**: `geo`, `places`, `reviews`, `trending`, `follow`, `checkin`, `plans`,
  `invitations`, `taste`, `blocks`, `notifications`, `placeadmin`, `realtime`.
- **Routes**: discovery (list, map/cluster, search, filters, trending), place detail, reviews
  (write, edit, delete, moderate), follows, check-ins with edge-validated proximity, plans and
  their members/places/votes, invitations with the `SURPRISE` projection, the backoffice over
  places (content, hours, attributes, menu, photos), moderation, notifications and
  `POST /v1/realtime/auth`.
- `database/diagram_base.txt` and `database/verify_base_model.sql` kept true in the same commit as
  each change; the drift check now covers every table the change added.

## Verification

- The suite is green with every service stopped: 30 packages, plus `internal/contract` for
  documentation drift.
- Behaviour was proven against the running stack, not only in tests: discovery over real
  coordinates and clustering, the reviews partial index after a logical delete, the gallery
  replacement inside a rolled-back transaction, the drift check on a scratch database built from
  all 31 ledger files, and — for realtime — our client publishing to Soketi with a real WebSocket
  client receiving the event.
- Every slice landed as a pull request against `dev` (#11–#23) with its verification recorded in
  the body.

## Notes

Decisions taken deliberately and recorded rather than implemented: `FOLLOWERS_ONLY` visibility
(Q3), capacity limits on public plans apart from party size (Q6), and FX conversion for prices —
Phase 1 compares inside one named currency, USD, and the rate path is written down instead of
guessed. The `SURPRISE` model was inverted by the human mid-change: the venue is hidden and the
host picks the reveal rule (D20, rewritten rather than appended to).
