# plan-invitations — P3b: invitations with RSVP, expiry and the host's view

Feature: a plan can invite a registered person, who accepts or declines, and the invitation
expires. The host sees every answer; a guest sees only their own.
Branch: `feat/api-plan-invitations` off `feat/api-plan-votes`.
Authority: `openspec/changes/social-discovery/spec.md` §S19, §S26; `design.md` §D16, §D20 and
the resolved Q1/Q5. This document pins commit A (lifecycle and RSVP); the SURPRISE reveal rules
and the hygiene sweeper are commit B.

## Specs

**S1 — A lifecycle, not a flag.** `tx_status` from `public.plan_invitation_status`
(`PENDING`, `ACCEPTED`, `DECLINED`, `EXPIRED`, `REVOKED`). One live invitation per person and
plan, enforced by a partial unique index over `PENDING`; earlier attempts stay as history.

**S2 — The read predicate is the truth.** Every read and every accept requires
`dt_expires_at > now()`. A `PENDING` row past its expiry is already unusable, even if no sweeper
ever ran. The sweeper is hygiene, not enforcement.

**S3 — Only an EDITOR or OWNER invites.** A non-member's invitation is rejected, not silently
dropped. Invitations address **registered users only**: no shareable token in this phase.

**S4 — Accepting creates the least privilege.** In one transaction: the invitation flips to
`ACCEPTED`, `dt_responded_at` is stamped, and the membership row is inserted with role
`VIEWER` — never `EDITOR` (D8). Declining is terminal and creates no membership.

**S5 — Response visibility is asymmetric.** The host and any `EDITOR`/`OWNER` read every
invitee's status. An invitee reads only their own invitation. Who declined is not community
information.

**S6 — Re-inviting is a new row.** After a decline or an expiry, inviting the same person again
inserts a new invitation rather than reopening the old one, so the audit trail stays readable.

**S7 — Contract first, no new error codes.** Reuse `validation_failed`, `not_found`,
`permission_denied`, `unauthorized`, `internal_error`. An invitation the caller may not see is
`404`; a plan the caller may see but may not invite into is `403`.

## Routes (commit A)

| Method | Path | Who |
|---|---|---|
| `POST` | `/v1/plans/{coRoute}/invitations` | `EDITOR`/`OWNER`; body `{coUser, expiresAt, message?, hint?}` |
| `GET` | `/v1/plans/{coRoute}/invitations` | `EDITOR`/`OWNER` — **the host's RSVP view** |
| `GET` | `/v1/invitations` | bearer; the caller's own invitations |
| `POST` | `/v1/invitations/{coInvitation}/accept` | the invitee |
| `POST` | `/v1/invitations/{coInvitation}/decline` | the invitee |

## Commit B (next)

`SURPRISE` mode with the host-chosen reveal rule, and the scheduled sweeper that flips stale
`PENDING` rows to `EXPIRED` for hygiene. Both are additive on this commit.

---

## Log

- L1 — Human authorization, verbatim: "toma la mejora alternativa, lo dejo a tu criterio,
  continuemos". Read as delegating the two open product questions to the orchestrator and
  authorizing the next slice; both decisions are recorded in the change log (L19) with reasons.
- L2 — Q2 (blocking) decided as a public-launch gate rather than a feature, and Q6 (capacity) as
  party size only; neither is implemented here.
