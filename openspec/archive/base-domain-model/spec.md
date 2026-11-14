# Spec: Base Domain Model — One Database, Two Schemas

Canonical source: `database/diagram_base.txt` (DBML for dbdiagram.io, committed with
this change). Quotes below are verbatim from that file; the diagram itself is the
authority for every column, index, and note not repeated here.

## Specs

### S1 — Topology: one database, two schemas, enums in public

> "TOPOLOGÍA: UNA BASE DE DATOS, DOS ESQUEMAS
>   - seg    → identidad y permisos: usuarios, refresh tokens, roles, permisos.
>   - core   → dominio: lugares, horarios, menús, reseñas y rutas.
>   - public → solo los Enum compartidos, porque son los únicos objetos que usan
>     ambos esquemas (en PostgreSQL un tipo vive en un esquema y `public` está en
>     el search_path por defecto, así que se usan sin calificar)."

> "Este modelo REEMPLAZA la topología anterior de dos bases (apiseg + coreapp).
> Motivo: con un único servicio dueño de todo, dos bases costaban FK y JOIN entre
> seguridad y dominio sin comprar aislamiento real. Los esquemas dan la misma
> separación lógica, permiten GRANT por esquema, y las FK y los JOIN entre esquemas
> funcionan de verdad. Extraer a futuro sigue siendo un `pg_dump -n seg`."

### S2 — PKs and column prefixes

> "1. PK: UUID en todas las tablas de negocio. Prefijos de columna:
>    co_ (código/identificador), tx_ (texto), nu_ (numérico), dt_ (fecha/hora),
>    js_ (json), co_X_user (referencias a usuario)."

### S3 — FKs are real, including cross-schema, and every FK is indexed

> "2. LAS FK SON REALES, INCLUIDAS LAS QUE CRUZAN ESQUEMAS.
>    A diferencia de la versión anterior de este diagrama, ninguna relación es
>    "lógica": PostgreSQL sí admite foreign keys entre esquemas de una misma base.
>    Toda columna co_* tiene su Ref, y toda FK tiene índice porque PostgreSQL no
>    indexa las FK automáticamente."

### S4 — Lifecycle: three buckets

> "3. CICLO DE VIDA — tres cubos, según la naturaleza de la tabla:
>    - Entidades de negocio y catálogos de seguridad: auditoría completa
>      (dt_created_at + dt_updated_at + dt_deleted_at, soft delete).
>    - Puentes y configuración por fila: dt_created_at + dt_updated_at, borrado
>      físico. Motivo: con soft delete y clave compuesta, una fila "borrada"
>      bloquea para siempre volver a insertar el mismo par.
>    - Estado operativo (tb_refresh_token, tb_user_token, tb_signup_request): sin
>      soft delete, el estado se expresa con columnas propias (dt_expires_at,
>      dt_revoked_at, dt_used_at). Un token revocado o ya usado no está "borrado",
>      y hay que conservarlo para poder detectar el reuso."

> "     - Bitácora inmutable (tb_security_event): solo dt_created_at. Nunca se actualiza
>        ni se borra una fila individual; la retención se aplica por partición o lote."

### S5 — Partial uniques on soft-deletable tables

> "4. UNIQUE + SOFT DELETE = TRAMPA.
>    Un unique plano sobre una tabla con dt_deleted_at impide reutilizar el valor
>    para siempre. Todos los uniques de tablas con soft delete son PARCIALES
>    (WHERE dt_deleted_at IS NULL) y están marcados [PARTIAL UNIQUE]."

Applies to: `ux_tb_user_email_active (tx_email)`,
`ux_tb_user_identity_provider_sub (tx_provider, tx_provider_user_id)`,
`ux_tb_role_name_active (tx_name)`, `ux_tb_permission_code_active (tx_code)`,
`ux_tb_review_user_place_active (co_user, co_place)`.

### S6 — CHECK constraints

> "5. Los CHECK se expresan en el propio DBML con la sintaxis `check:` de columna,
>    así que no quedan como comentario: rating 1..5, rango de latitud y longitud,
>    y precio >= 0."

Plus `nu_day_of_week BETWEEN 1 AND 7` in `core.tb_place_schedule`
("ISO-8601: 1=Lunes ... 7=Domingo").

### S7 — Identity vs capacity (RBAC)

> "6. IDENTIDAD VS CAPACIDAD (RBAC).
>    tb_user es LA tabla base de usuarios: los de la app y los del backoffice son la
>    misma entidad. No hay tb_admin_user ni columna tx_type. Las capacidades no se
>    guardan en el usuario: se derivan del RBAC (usuario → rol → permiso). Un usuario
>    entra al módulo administrativo porque tiene un rol activo que otorga el permiso
>    admin.access, no porque su registro sea "de tipo admin"."

### S8 — Tokens

> "7. TOKENS.
>    Access token corto y SIN ESTADO: no se guarda en ninguna tabla.
>    Refresh token largo y CON ESTADO: se guarda su hash (nunca el token crudo) y
>    rota en cada uso. Reusar un refresh ya rotado invalida la familia completa.
>    Al cambiar la contraseña se revocan TODAS las familias de refresh del usuario."

Schema consequence: NO access-token table exists. `seg.tb_refresh_token` stores
`tx_token_hash` (unique), `co_token_family`, `dt_expires_at`, `dt_revoked_at`.

### S9 — Federated identity resolves by (provider, sub), never by email

> "8. IDENTIDAD FEDERADA (Google / Apple).
>    El proveedor devuelve un ID token con un claim `sub`. La identidad se resuelve
>    SIEMPRE por (provider, sub) en tb_user_identity, NUNCA por email: el email
>    cambia, Apple inventa direcciones de relé y no lo envía en todas las
>    autorizaciones. El token del proveedor NUNCA es la credencial de la API: el
>    backend lo valida contra el JWKS del proveedor y emite su propio JWT."

Schema consequence: partial unique `ux_tb_user_identity_provider_sub` on
`(tx_provider, tx_provider_user_id)`; `tx_email` is nullable and informational.

### S10 — Email is verified BEFORE the user exists

> "9. VERIFICACIÓN DE EMAIL ANTES DE QUE EL USUARIO EXISTA.
>    El email se prueba ANTES de crear la fila en tb_user: mientras nadie demuestre
>    acceso a la casilla, el registro vive en tb_signup_request. Evita dos cosas: que
>    alguien ocupe (squatting) la dirección de otra persona con un registro que nunca
>    va a verificar, y que tb_user se llene de basura. Durante el registro el email es
>    INMUTABLE: si cambia, hay que emitir un código nuevo para la dirección nueva."

### S11 — Delivery by 6-digit code, not by link

> "10. ENTREGA POR CÓDIGO, NO POR LINK (por ahora).
>    Todos los flujos que confirman acceso a un email usan un código de 6 dígitos:
>    registro, recuperación de contraseña y cambio de email. [...]
>    Un código son 1.000.000 de combinaciones: por eso TODA tabla con código lleva
>    contador de intentos (cortar en 3 a 5) y vencimiento corto."

Schema consequence: `tb_signup_request` and `tb_user_token` carry `tx_code_hash`
(SHA-256, never the raw code), `nu_attempts` default 0, `dt_expires_at`.

### S12 — Security audit log: append-only, no FK to user, no secrets

> "11. AUDITORÍA DE SEGURIDAD Y ACCESOS (tb_security_event).
>    Bitácora append-only de todo lo que importa para seguridad: logins, fallos,
>    cambios de contraseña, vínculos de proveedores, cambios de RBAC y accesos al
>    módulo administrativo, con IP y ubicación aproximada derivada de la IP.
>    Reglas duras: NUNCA guarda códigos, tokens ni contraseñas; NUNCA guarda el email
>    en claro de un intento fallido (sería un oráculo de enumeración de usuarios),
>    guarda un HMAC; y NO lleva FK a tb_user, porque la auditoría tiene que sobrevivir
>    al borrado del usuario."

And its table comment: "la retención se aplica por partición mensual
(PARTITION BY RANGE (dt_created_at), borrar = DROP PARTITION) o por lote. DBML no
puede expresar el particionado, va como nota de implementación."

Schema consequence: `tx_event` is `varchar` with dot convention, NOT an enum
("Un catálogo de eventos crece con cada funcionalidad nueva, y un Enum de PostgreSQL
obliga a migrar el tipo cada vez (y no se puede quitar un valor)."); `co_user` and
`co_actor_user` are nullable and carry NO foreign key; `tx_email_hash` is HMAC-SHA256.

### S13 — Enums (in `public`)

> "Enum activatable_status { ACTIVE INACTIVE }"
> "Enum suspendable_status { ACTIVE INACTIVE SUSPENDED }"
> "Enum review_status { ACTIVE INACTIVE MODERATED }"
> "Enum photo_kind { COVER GALLERY }"
> "Enum identity_provider { GOOGLE APPLE }"
> "Enum user_token_purpose { PASSWORD_RESET EMAIL_CHANGE }"

### S14 — Table inventory (columns exactly as the diagram)

`seg`: tb_user, tb_refresh_token, tb_user_token, tb_signup_request, tb_user_identity,
tb_role, tb_permission, tb_user_role, tb_role_permission, tb_security_event.
`core`: tb_place, tb_place_schedule, tb_place_photo, tb_menu, tb_menu_item,
tb_review, tb_route, tb_route_place.

Behavior-encoding notes honored verbatim, among them:
- `seg.tb_user.tx_email` / `tx_password_hash`: "NULL permitido" (Apple relay emails;
  federated-only users have no password).
- `core.tb_place_schedule.dt_close_time`: "Si <= dt_open_time, el cierre es al día
  siguiente (cruza medianoche). Ej. abre 20:00, cierra 02:00".
- `core.tb_place.co_owner_user`: "Dueño del lugar. NULL en lugares curados por el
  backoffice" (`Ref: core.tb_place.co_owner_user >? seg.tb_user.co_user`, optional).
- `seg.tb_user_role` / `seg.tb_role_permission` / `core.tb_route_place`: composite
  PKs, no soft delete (bridge rule from S4).
- `core.tb_place_schedule`: plain unique `ux_tb_place_schedule_slot` on
  `(co_place, nu_day_of_week, dt_open_time)` (no soft delete on that table).

### S15 — Out of scope on purpose

> "Fuera del alcance a propósito: tablas de sesión (con JWT no hacen falta) y las
> acciones ON DELETE/UPDATE de las FK (son una decisión de negocio aparte)."

Also out of scope for THIS change (from "CONTEXTO DE ESTA ITERACIÓN"): the Go API
service, the Laravel decommissioning itself, and the admin frontend ("PENDIENTE: el
frontend del módulo administrativo").
