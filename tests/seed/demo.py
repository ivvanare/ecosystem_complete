#!/usr/bin/env python3
"""Seed the development catalogue with demo content: weekly hours, menus and photo galleries.

Why this exists: the seeded catalogue has twenty places and almost no content, so the client sees
empty detail screens and cannot tell a rendering bug from a missing row. This fills it.

It goes through the **HTTP API**, never SQL. Two reasons, both about honesty:

- the bytes of a photo have to land in the media volume, or the URL the contract returns 404s;
- the rows have to pass through the same paths an operator uses, so the demo exercises the real
  writes instead of a shape only this script knows.

Schedules and menus are `PUT` (replace), so re-running refreshes them. Photos are `POST` (append),
so a place that already has a live gallery is skipped unless `--force` is passed — without that
check, every run would pile another pair of images onto the same place.

Usage, against the local stack:

    python3 tests/seed/demo.py

Against another deployment:

    MELIA_API=https://api.example MELIA_EMAIL=ops@example MELIA_PASSWORD=… \\
        python3 tests/seed/demo.py --limit-photos 0

Requires only python3 and curl: no third-party packages.
"""

from __future__ import annotations

import argparse
import json
import os
import struct
import subprocess
import sys
import tempfile
import zlib

# Weekly hours per category. Two of them carry an overnight row on purpose: a place that closes
# after midnight is the case the open-now predicate exists for, and it cannot be exercised by
# hours that all fit inside one day.
WEEKLY = {
    "Hotel": [{"dayOfWeek": day, "open": "00:00", "close": "23:59"} for day in range(1, 8)],
    "Parque": [{"dayOfWeek": day, "open": "06:00", "close": "18:00"} for day in range(1, 8)],
    "Arepera": [{"dayOfWeek": day, "open": "08:00", "close": "21:00"} for day in range(1, 6)]
    + [{"dayOfWeek": 6, "open": "20:00", "close": "02:00"}],
}

# Menus per category. The mixed currencies are deliberate: `maxPrice` only matches USD items, and
# a demo where every price shares one currency never shows that rule.
MENUS = {
    "Arepera": [
        {"name": "Carta", "description": "Todo el día", "items": [
            {"name": "Arepa Reina Pepiada", "description": "Pollo y aguacate", "price": 4.5, "currency": "USD"},
            {"name": "Arepa de queso", "description": None, "price": 3.0, "currency": "USD"},
            {"name": "Pabellón Criollo", "description": "El plato nacional", "price": 12.0, "currency": "USD"},
            {"name": "Jugo natural", "description": "Parchita o papelón", "price": 180.0, "currency": "VES"},
        ]},
        {"name": "Parrilla", "description": "A partir de las 18:00", "items": [
            {"name": "Parrilla para dos", "description": "Res, pollo y chorizo", "price": 24.0, "currency": "USD"},
        ]},
    ],
    "Hotel": [
        {"name": "Desayuno", "description": None, "items": [
            {"name": "Buffet criollo", "description": None, "price": 9.5, "currency": "USD"},
            {"name": "Café de la casa", "description": None, "price": 2.0, "currency": "USD"},
        ]},
    ],
    "Parque": [
        {"name": "Kiosco", "description": "Al paso", "items": [
            {"name": "Agua", "description": None, "price": 1.0, "currency": "USD"},
            {"name": "Helado", "description": None, "price": 2.5, "currency": "USD"},
        ]},
    ],
}

# One hue family per place, so a gallery does not look like the same photo twice.
PALETTES = [
    ((214, 108, 54), (120, 40, 12)),
    ((54, 126, 214), (12, 40, 120)),
    ((74, 168, 96), (16, 78, 40)),
    ((168, 74, 168), (78, 16, 78)),
    ((214, 178, 54), (120, 88, 12)),
    ((54, 178, 168), (12, 78, 78)),
]

PHOTO_WIDTH, PHOTO_HEIGHT = 960, 600


def api(method: str, path: str, token: str | None = None, body=None, files=None):
    """Call the API with curl: one dependency that is already on every machine that runs this."""
    command = ["curl", "-s", "-X", method, API + path, "-w", "\n%{http_code}"]
    if token:
        command += ["-H", f"Authorization: Bearer {token}"]
    if body is not None:
        command += ["-H", "Content-Type: application/json", "-d", json.dumps(body)]
    for spec in files or []:
        command += ["-F", spec]

    completed = subprocess.run(command, capture_output=True, text=True)
    payload, _, status = completed.stdout.rpartition("\n")
    return int(status or 0), payload


def gradient_png(width: int, height: int, top, bottom) -> bytes:
    """A vertical gradient, encoded by hand: no Pillow, no dependency to install."""
    pixels = bytearray()
    for y in range(height):
        ratio = y / (height - 1)
        row = bytes(int(top[i] + (bottom[i] - top[i]) * ratio) for i in range(3)) * width
        pixels += b"\x00" + row

    def chunk(tag: bytes, data: bytes) -> bytes:
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(bytes(pixels), 9))
        + chunk(b"IEND", b"")
    )


def seed_hours_and_menus(places, token, dry_run):
    for place in places:
        category = place["category"]["name"]
        weekly = WEEKLY.get(category)
        menus = MENUS.get(category)
        if not weekly or not menus:
            print(f"  skipped {place['name']}: no template for category {category}")
            continue
        if dry_run:
            continue

        status, payload = api("PUT", f"/v1/admin/places/{place['coPlace']}/schedules", token,
                              {"schedules": weekly})
        if status not in (200, 201, 204):
            print(f"  hours failed for {place['name']}: {status} {payload[:160]}")

        status, payload = api("PUT", f"/v1/admin/places/{place['coPlace']}/menu", token,
                              {"menus": menus})
        if status not in (200, 201, 204):
            print(f"  menu failed for {place['name']}: {status} {payload[:160]}")
    print(f"hours and menus: {len(places)} places")


def seed_photos(places, token, limit, force, dry_run):
    for index, place in enumerate(places[:limit]):
        place_id = place["coPlace"]

        if not force:
            status, payload = api("GET", f"/v1/places/{place_id}")
            if status == 200 and json.loads(payload).get("photos"):
                print(f"  {place['name']}: already has a gallery, skipped (--force to add anyway)")
                continue

        top, bottom = PALETTES[index % len(PALETTES)]
        uploaded = []
        for shot in range(2):
            handle, path = tempfile.mkstemp(suffix=".png", prefix=f"seed-{index}-{shot}-")
            with os.fdopen(handle, "wb") as file:
                file.write(gradient_png(PHOTO_WIDTH, PHOTO_HEIGHT, top, bottom))
            try:
                if dry_run:
                    continue
                status, payload = api("POST", f"/v1/admin/places/{place_id}/photos", token,
                                      files=[f"file=@{path};type=image/png"])
                if status != 201:
                    print(f"  upload failed for {place['name']}: {status} {payload[:160]}")
                    continue
                uploaded.append(json.loads(payload)["coPhoto"])
            finally:
                os.unlink(path)

        if not uploaded or dry_run:
            continue

        # The upload only appends: the cover is chosen by replacing the live set, because the
        # cover is a property of the gallery and not of the image.
        gallery = [
            {"coPhoto": photo, "type": "COVER" if position == 0 else "GALLERY", "order": position}
            for position, photo in enumerate(uploaded)
        ]
        status, payload = api("PUT", f"/v1/admin/places/{place_id}/photos", token, {"photos": gallery})
        if status not in (200, 201, 204):
            print(f"  gallery failed for {place['name']}: {status} {payload[:160]}")
        else:
            print(f"  {place['name']}: {len(uploaded)} photos")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--api", default=os.environ.get("MELIA_API", "http://localhost:9000"))
    parser.add_argument("--email", default=os.environ.get("MELIA_EMAIL", "k6-admin@melia.test"))
    parser.add_argument("--password", default=os.environ.get("MELIA_PASSWORD", "k6-clave-larga-123"))
    parser.add_argument("--near", default="10.496,-66.853",
                        help="Origin for the catalogue read. The seeded places are in Caracas.")
    parser.add_argument("--limit-photos", type=int, default=10,
                        help="How many places get a gallery. 0 leaves every place without photos, "
                             "which is what a client needs to test its placeholder.")
    parser.add_argument("--force", action="store_true", help="Add photos even when a gallery exists.")
    parser.add_argument("--dry-run", action="store_true", help="Print what would happen, change nothing.")
    args = parser.parse_args()

    global API
    API = args.api.rstrip("/")

    status, payload = api("POST", "/v1/auth/login",
                          body={"email": args.email, "password": args.password})
    if status != 200:
        print(f"login failed: {status} {payload[:200]}", file=sys.stderr)
        return 1
    token = json.loads(payload)["access_token"]

    status, payload = api("GET", f"/v1/places?near={args.near}&radius=50000&limit=100", token)
    if status != 200:
        print(f"catalogue read failed: {status} {payload[:200]}", file=sys.stderr)
        return 1
    places = json.loads(payload)["data"]
    if not places:
        print("no places around that origin: pass --near", file=sys.stderr)
        return 1
    print(f"catalogue: {len(places)} places around {args.near}")

    seed_hours_and_menus(places, token, args.dry_run)
    seed_photos(places, token, args.limit_photos, args.force, args.dry_run)
    print("done")
    return 0


if __name__ == "__main__":
    sys.exit(main())
