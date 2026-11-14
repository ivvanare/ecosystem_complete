-- ============================================================================
-- DEMO SEED — Caracas, Venezuela. NOT A MIGRATION.
--
-- This file is deliberately outside database/init: it is not applied by the service,
-- it is not recorded in seg.tb_schema_migration, and it must never run in production.
-- Apply it by hand to a development database only:
--
--   docker exec -i melzone_db psql -U admin -d melia < database/seed/01-caracas-demo.sql
--
-- Why it exists: the discovery read path answers over core.tb_place, and an empty
-- table proves nothing about a map. These rows give the radius query, the viewport
-- query, the H3 clustering, the attribute/price/open-at filters and the name/dish
-- search something to work on. The attribute links and menus need ledger 18
-- (tb_place_attribute) applied first.
--
-- Nature of the data, stated rather than implied: the coordinates are approximate
-- positions inside the named zones, and the place names are illustrative development
-- data — this is not a verified directory and no rating, price or availability claim
-- is made about any business. Public landmarks keep their real names because they are
-- public geography. The attributes are illustrative too: they exist so every filter
-- has rows to match and rows to exclude, and frente_playa on a Caracas demo is
-- vocabulary exercise, not a claim about the city.
--
-- Idempotent: fixed UUIDs plus ON CONFLICT DO NOTHING, so re-running is a no-op.
-- ============================================================================

BEGIN;

INSERT INTO core.tb_place
    (co_place, tx_name, tx_title, co_place_category, tx_country, tx_region, tx_timezone,
     nu_latitude, nu_longitude, tx_address, js_social_media, dt_created_at, dt_updated_at)
SELECT
    v.co_place::uuid, v.tx_name, v.tx_title, c.co_place_category, 'VE', v.tx_region,
    'America/Caracas', v.lat::numeric(9,6), v.lng::numeric(9,6), v.tx_address, NULL,
    now(), now()
FROM (VALUES
    -- co_place                               name                                    title                        category   region
    ('a0000001-0000-4000-8000-000000000001', 'Arepera Chacao',                       'Arepera Chacao',            'arepera', 'VE-A', 10.496000, -66.853000, 'Av. Francisco de Miranda, Chacao'),
    ('a0000001-0000-4000-8000-000000000002', 'Arepera Altamira',                     'Arepera Altamira',          'arepera', 'VE-A', 10.496500, -66.848000, 'Calle Los Laboratorios, Altamira'),
    ('a0000001-0000-4000-8000-000000000003', 'Casa de Arepas La Castellana',         'Arepas La Castellana',      'arepera', 'VE-A', 10.497500, -66.857000, 'Av. Principal La Castellana'),
    ('a0000001-0000-4000-8000-000000000004', 'Hotel Las Mercedes Plaza',             'Las Mercedes Plaza',        'hotel',   'VE-A', 10.483000, -66.860000, 'Av. Principal Las Mercedes'),
    ('a0000001-0000-4000-8000-000000000005', 'Arepera Sabana Grande',                'Arepera Sabana Grande',     'arepera', 'VE-A', 10.494000, -66.878000, 'Av. Abraham Lincoln, Sabana Grande'),
    ('a0000001-0000-4000-8000-000000000006', 'Arepera Los Palos Grandes',            'Arepera P. Grandes',        'arepera', 'VE-A', 10.500000, -66.845000, '3ra Av. Los Palos Grandes'),
    ('a0000001-0000-4000-8000-000000000007', 'Parque El Hatillo',                    'Parque El Hatillo',         'parque',  'VE-M', 10.430000, -66.820000, 'Pueblo de El Hatillo'),
    ('a0000001-0000-4000-8000-000000000008', 'Arepera Catia',                        'Arepera Catia',             'arepera', 'VE-A', 10.510000, -66.940000, 'Av. Sucre, Catia'),
    ('a0000001-0000-4000-8000-000000000009', 'Parque Petare',                        'Parque Petare',             'parque',  'VE-M', 10.480000, -66.810000, 'Casco de Petare'),
    ('a0000001-0000-4000-8000-00000000000a', 'Arepera La Candelaria',                'Arepera La Candelaria',     'arepera', 'VE-A', 10.506000, -66.906000, 'Esquina La Candelaria'),
    ('a0000001-0000-4000-8000-00000000000b', 'Parque Los Caobos',                    'Los Caobos',                'parque',  'VE-A', 10.498000, -66.886000, 'Av. Méjico, Bellas Artes'),
    ('a0000001-0000-4000-8000-00000000000c', 'Hotel Bello Monte',                    'Bello Monte',               'hotel',   'VE-A', 10.487000, -66.870000, 'Av. Principal Bello Monte'),
    ('a0000001-0000-4000-8000-00000000000d', 'Parque Generalísimo Francisco de Miranda', 'Parque del Este',       'parque',  'VE-M', 10.494000, -66.839000, 'Av. Francisco de Miranda'),
    ('a0000001-0000-4000-8000-00000000000e', 'Parque Nacional El Ávila',             'Sabas Nieves',              'parque',  'VE-M', 10.535000, -66.856000, 'Entrada Sabas Nieves'),
    ('a0000001-0000-4000-8000-00000000000f', 'Hotel La Trinidad',                    'La Trinidad',               'hotel',   'VE-M', 10.470000, -66.870000, 'Av. Intercomunal La Trinidad'),
    ('a0000001-0000-4000-8000-000000000010', 'Arepera Santa Mónica',                 'Arepera Santa Mónica',      'arepera', 'VE-A', 10.470000, -66.890000, 'Av. Principal Santa Mónica'),
    ('a0000001-0000-4000-8000-000000000011', 'Hotel Country Club',                   'Country Club',              'hotel',   'VE-A', 10.490000, -66.865000, 'Campo Alegre'),
    ('a0000001-0000-4000-8000-000000000012', 'Parque Caricuao',                      'Parque Caricuao',           'parque',  'VE-A', 10.430000, -66.980000, 'Av. Principal Caricuao'),
    ('a0000001-0000-4000-8000-000000000013', 'Hotel Macaracuay',                     'Macaracuay',                'hotel',   'VE-M', 10.485000, -66.810000, 'Av. Principal Macaracuay'),
    ('a0000001-0000-4000-8000-000000000014', 'Arepera Bellas Artes',                 'Arepera Bellas Artes',      'arepera', 'VE-A', 10.501000, -66.892000, 'Av. Bolívar, Bellas Artes')
) AS v(co_place, tx_name, tx_title, cat_code, tx_region, lat, lng, tx_address)
JOIN core.tb_place_category c ON c.tx_code = v.cat_code AND c.dt_deleted_at IS NULL
ON CONFLICT (co_place) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Schedules: enough shape to exercise "open at an instant", including the
-- overnight case the model already supports (close 02:00 belongs to the day it
-- opened). 1 = Monday ... 7 = Sunday, ISO-8601.
-- ---------------------------------------------------------------------------
INSERT INTO core.tb_place_schedule (co_schedule, co_place, nu_day_of_week, dt_open_time, dt_close_time, dt_created_at, dt_updated_at)
SELECT gen_random_uuid(), v.co_place::uuid, v.dow, v.opens::time, v.closes::time, now(), now()
FROM (VALUES
    -- areperas: lunch through late evening, one crossing midnight
    ('a0000001-0000-4000-8000-000000000001', 1, '10:00', '22:00'),
    ('a0000001-0000-4000-8000-000000000001', 5, '10:00', '23:30'),
    ('a0000001-0000-4000-8000-000000000002', 1, '09:00', '21:00'),
    ('a0000001-0000-4000-8000-000000000003', 6, '11:00', '02:00'),  -- overnight
    ('a0000001-0000-4000-8000-000000000005', 1, '10:00', '23:00'),
    ('a0000001-0000-4000-8000-000000000006', 1, '10:00', '22:30'),
    ('a0000001-0000-4000-8000-000000000008', 1, '10:00', '21:00'),
    ('a0000001-0000-4000-8000-00000000000a', 1, '10:00', '22:00'),
    ('a0000001-0000-4000-8000-000000000010', 1, '10:00', '21:30'),
    ('a0000001-0000-4000-8000-000000000014', 1, '08:00', '20:00'),
    -- parks: daylight only
    ('a0000001-0000-4000-8000-00000000000b', 1, '06:00', '18:00'),
    ('a0000001-0000-4000-8000-00000000000d', 1, '06:00', '18:00'),
    ('a0000001-0000-4000-8000-00000000000e', 1, '05:30', '17:00'),
    -- hotels: all day, the overnight run makes the closed-at-noon case explicit
    ('a0000001-0000-4000-8000-000000000004', 1, '00:00', '23:59'),
    ('a0000001-0000-4000-8000-000000000011', 1, '00:00', '23:59')
) AS v(co_place, dow, opens, closes)
ON CONFLICT (co_place, nu_day_of_week, dt_open_time) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Attributes (S23): the three the seed must demonstrate, spread so that the
-- conjunctive filter has intersections to find — terraza and petfriendly share
-- place ...0001 on purpose — plus the rest of the vocabulary where it reads
-- naturally. Links are resolved by tx_code, the governed key.
-- ---------------------------------------------------------------------------
INSERT INTO core.tb_place_attribute_link (co_place, co_attribute, dt_created_at, dt_updated_at)
SELECT v.co_place::uuid, a.co_attribute, now(), now()
FROM (VALUES
    -- terraza: areperas con mesa afuera
    ('a0000001-0000-4000-8000-000000000001', 'terraza'),
    ('a0000001-0000-4000-8000-000000000002', 'terraza'),
    ('a0000001-0000-4000-8000-000000000014', 'terraza'),
    -- petfriendly: parques y una arepera
    ('a0000001-0000-4000-8000-000000000001', 'petfriendly'),
    ('a0000001-0000-4000-8000-00000000000b', 'petfriendly'),
    ('a0000001-0000-4000-8000-00000000000d', 'petfriendly'),
    ('a0000001-0000-4000-8000-00000000000e', 'petfriendly'),
    -- frente_playa: vocabulary exercise (see the header)
    ('a0000001-0000-4000-8000-000000000007', 'frente_playa'),
    ('a0000001-0000-4000-8000-000000000013', 'frente_playa'),
    -- aire_libre: los parques
    ('a0000001-0000-4000-8000-000000000007', 'aire_libre'),
    ('a0000001-0000-4000-8000-000000000009', 'aire_libre'),
    ('a0000001-0000-4000-8000-00000000000b', 'aire_libre'),
    ('a0000001-0000-4000-8000-00000000000d', 'aire_libre'),
    ('a0000001-0000-4000-8000-00000000000e', 'aire_libre'),
    ('a0000001-0000-4000-8000-000000000012', 'aire_libre'),
    -- musica_en_vivo: un hotel y una arepera
    ('a0000001-0000-4000-8000-000000000004', 'musica_en_vivo'),
    ('a0000001-0000-4000-8000-000000000005', 'musica_en_vivo')
) AS v(co_place, attr_code)
JOIN core.tb_place_attribute a ON a.tx_code = v.attr_code AND a.dt_deleted_at IS NULL
ON CONFLICT (co_place, co_attribute) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Menus and items (S23): at least two USD-priced items, one under 10 and one
-- over, so maxPrice has both sides of the line. One VES item ships on purpose:
-- maxPrice is USD-only and that item must never match it (documented behaviour,
-- spec.md S22). The dish search reads tx_name and tx_description of these rows.
-- ---------------------------------------------------------------------------
INSERT INTO core.tb_menu (co_menu, co_place, tx_name, tx_description, tx_status, dt_created_at, dt_updated_at)
VALUES
    ('a0000002-0000-4000-8000-000000000001', 'a0000001-0000-4000-8000-000000000001', 'Todo el día', NULL, 'ACTIVE', now(), now()),
    ('a0000002-0000-4000-8000-000000000002', 'a0000001-0000-4000-8000-000000000002', 'Carta',       NULL, 'ACTIVE', now(), now())
ON CONFLICT (co_menu) DO NOTHING;

INSERT INTO core.tb_menu_item
    (co_menu_item, co_menu, tx_name, tx_description, nu_price, co_currency, tx_status, dt_created_at, dt_updated_at)
SELECT v.co_menu_item::uuid, v.co_menu::uuid, v.tx_name, v.tx_description,
       v.nu_price::numeric(10,2), c.co_currency, 'ACTIVE', now(), now()
FROM (VALUES
    ('a0000003-0000-4000-8000-000000000001', 'a0000002-0000-4000-8000-000000000001', 'Arepa Reina Pepiada', 'Aguacate y pollo',        '4.50',  'USD'),
    ('a0000003-0000-4000-8000-000000000002', 'a0000002-0000-4000-8000-000000000001', 'Pabellón Criollo',   'El plato nacional',        '12.00', 'USD'),
    ('a0000003-0000-4000-8000-000000000003', 'a0000002-0000-4000-8000-000000000002', 'Arepa Dominó',       'Caraotas negras y queso',  '3.80',  'USD'),
    ('a0000003-0000-4000-8000-000000000004', 'a0000002-0000-4000-8000-000000000002', 'Asado Negro',        'Con arroz y tajadas',      '14.00', 'USD'),
    ('a0000003-0000-4000-8000-000000000005', 'a0000002-0000-4000-8000-000000000002', 'Refresco de Papelón', 'Frío, con limón',         '2.00',  'VES')
) AS v(co_menu_item, co_menu, tx_name, tx_description, nu_price, currency_code)
JOIN core.tb_currency c ON c.tx_code = v.currency_code AND c.dt_deleted_at IS NULL
ON CONFLICT (co_menu_item) DO NOTHING;

COMMIT;

-- ---------------------------------------------------------------------------
-- Sanity check: run it and expect 20 places, 15 schedules, 17 attribute links,
-- 2 menus, 5 items and zero rows without a derived location.
-- ---------------------------------------------------------------------------
SELECT
    (SELECT count(*) FROM core.tb_place WHERE co_place::text LIKE 'a0000001-%')            AS demo_places,
    (SELECT count(*) FROM core.tb_place WHERE co_place::text LIKE 'a0000001-%'
        AND co_location IS NULL)                                                          AS missing_location,
    (SELECT count(*) FROM core.tb_place_schedule
        WHERE co_place::text LIKE 'a0000001-%')                                            AS demo_schedules,
    (SELECT count(*) FROM core.tb_place_attribute_link
        WHERE co_place::text LIKE 'a0000001-%')                                            AS demo_attribute_links,
    (SELECT count(*) FROM core.tb_menu WHERE co_menu::text LIKE 'a0000002-%')              AS demo_menus,
    (SELECT count(*) FROM core.tb_menu_item WHERE co_menu_item::text LIKE 'a0000003-%')    AS demo_menu_items;
