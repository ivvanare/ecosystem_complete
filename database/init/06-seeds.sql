-- ============================================================================
-- 06-seeds.sql — catálogo de permisos y roles base.
--
-- Se aplica una sola vez a través del ledger (seg.tb_schema_migration) como cualquier
-- otro archivo, y es idempotente: todo insert choca contra el índice único parcial del
-- modelo, así que reejecutarlo es un no-op.
--
-- Regla del modelo (diagrama, punto 6): los roles NO son tipos de usuario. Un usuario
-- entra al backoffice porque un rol activo le otorga admin.access, nunca porque su
-- registro sea "de tipo admin".
-- ============================================================================

-- ---- Permisos: la clave estable que evalúa el código -----------------------
INSERT INTO seg.tb_permission
    (co_permission, tx_code, tx_module, tx_name, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'admin.access',    'ADMIN',   'Acceder al backoffice',        'ACTIVE', now(), now()),
    (gen_random_uuid(), 'user.read',       'USERS',   'Ver usuarios',                 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'user.manage',     'USERS',   'Gestionar usuarios y roles',   'ACTIVE', now(), now()),
    (gen_random_uuid(), 'role.manage',     'ROLES',   'Gestionar roles y permisos',   'ACTIVE', now(), now()),
    (gen_random_uuid(), 'place.create',    'PLACES',  'Crear lugar',                  'ACTIVE', now(), now()),
    (gen_random_uuid(), 'place.update',    'PLACES',  'Editar lugar',                 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'place.delete',    'PLACES',  'Eliminar lugar',               'ACTIVE', now(), now()),
    (gen_random_uuid(), 'menu.manage',     'MENUS',   'Gestionar menús',              'ACTIVE', now(), now()),
    (gen_random_uuid(), 'review.moderate', 'REVIEWS', 'Moderar reseñas',              'ACTIVE', now(), now()),
    (gen_random_uuid(), 'route.manage',    'ROUTES',  'Gestionar rutas',              'ACTIVE', now(), now())
ON CONFLICT (tx_code) WHERE dt_deleted_at IS NULL DO NOTHING;

-- ---- Roles base ------------------------------------------------------------
INSERT INTO seg.tb_role
    (co_role, tx_name, tx_description, tx_status, dt_created_at, dt_updated_at)
VALUES
    (gen_random_uuid(), 'SUPERADMIN', 'Acceso total, incluida la gestión de roles', 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'ADMIN',      'Opera el negocio; no cambia roles ni permisos', 'ACTIVE', now(), now()),
    (gen_random_uuid(), 'EDITOR',     'Carga y edita contenido', 'ACTIVE', now(), now())
ON CONFLICT (tx_name) WHERE dt_deleted_at IS NULL DO NOTHING;

-- ---- SUPERADMIN: todos los permisos del catálogo ---------------------------
INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
SELECT r.co_role, p.co_permission, now(), now()
FROM seg.tb_role r
CROSS JOIN seg.tb_permission p
WHERE r.tx_name = 'SUPERADMIN'
  AND r.dt_deleted_at IS NULL
  AND p.dt_deleted_at IS NULL
ON CONFLICT (co_role, co_permission) DO NOTHING;

-- ---- ADMIN: todo menos role.manage ----------------------------------------
INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
SELECT r.co_role, p.co_permission, now(), now()
FROM seg.tb_role r
CROSS JOIN seg.tb_permission p
WHERE r.tx_name = 'ADMIN'
  AND r.dt_deleted_at IS NULL
  AND p.dt_deleted_at IS NULL
  AND p.tx_code IN (
      'admin.access', 'user.read', 'user.manage',
      'place.create', 'place.update', 'place.delete',
      'menu.manage', 'review.moderate', 'route.manage'
  )
ON CONFLICT (co_role, co_permission) DO NOTHING;

-- ---- EDITOR: contenido, sin usuarios ni borrados --------------------------
INSERT INTO seg.tb_role_permission (co_role, co_permission, dt_created_at, dt_updated_at)
SELECT r.co_role, p.co_permission, now(), now()
FROM seg.tb_role r
CROSS JOIN seg.tb_permission p
WHERE r.tx_name = 'EDITOR'
  AND r.dt_deleted_at IS NULL
  AND p.dt_deleted_at IS NULL
  AND p.tx_code IN ('admin.access', 'place.create', 'place.update', 'menu.manage', 'route.manage')
ON CONFLICT (co_role, co_permission) DO NOTHING;
