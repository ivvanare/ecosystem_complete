# Seed — demo content for the development catalogue

The seeded catalogue has twenty places and almost no content, so a client shows empty detail
screens and nobody can tell a rendering bug from a missing row. This fills it.

```bash
python3 tests/seed/demo.py              # hours, menus, and a gallery for the first 10 places
python3 tests/seed/demo.py --dry-run    # what it would do, changing nothing
python3 tests/seed/demo.py --limit-photos 0   # hours and menus only
```

Requires `python3` and `curl`. No third-party packages: the images are gradients encoded by hand.

## It talks to the API, never to SQL

Two reasons, both about honesty:

- the bytes of a photo have to land in the media volume, or the URL the contract returns answers
  `404`;
- the rows pass through the same paths an operator uses, so the demo exercises the real writes
  instead of a shape only this script knows.

## What it writes

| Content | Route | Repeatable? |
|---|---|---|
| Weekly hours, per category | `PUT /v1/admin/places/{coPlace}/schedules` | Yes — replaces the set |
| Menus, per category | `PUT /v1/admin/places/{coPlace}/menu` | Yes — replaces the card |
| Galleries (two gradients) | `POST …/photos` then `PUT …/photos` | **No by default** — a place that already has a live gallery is skipped, because the upload appends |

`--force` skips that check and adds another pair of images.

## Why the templates look like that

- **Areperas close after midnight on Saturday** (`20:00–02:00`). That is the case the open-now
  predicate exists for: hours that do not fit inside one day cannot be exercised by hours that do.
- **Hotels are open 24 hours** and **parks close at 18:00**, so the same request returns `true`,
  `false` and — at a place with no hours at all — `null`, which is the state a client most often
  gets wrong.
- **Menus mix USD and VES.** `maxPrice` only matches USD items; a demo where every price shares
  one currency never shows that rule.

## Credentials

The default account is the development `SUPERADMIN` created for the k6 write scripts
(`k6-admin@melia.test`). Point it elsewhere with environment variables:

```bash
MELIA_API=https://api.example MELIA_EMAIL=ops@example MELIA_PASSWORD=… python3 tests/seed/demo.py
```

The account needs `place.update` with `place.manage_all`, or ownership of the places it touches.
