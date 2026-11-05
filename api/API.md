# melia API — guía de consumo

Todo lo que necesita un cliente para hablar con la API: contratos, códigos de error, los dos
transportes de sesión y los límites. El contrato legible por máquina es
[`openapi.yaml`](openapi.yaml); este documento lo explica con ejemplos.

Base local: `http://localhost:9000` · todas las rutas de negocio van bajo `/v1`.

---

## 1. Convenciones

| Tema | Regla |
|---|---|
| Identificadores | UUID en texto |
| Fechas | RFC 3339 en UTC (`2026-03-15T10:00:00Z`) |
| Paginación | `?page=` (base 1, por defecto 1) y `?limit=` (por defecto 20, máximo 100) |
| Listas | `{"data": [...], "page": 1, "limit": 20, "total": 137}` |
| Cuerpo de error | siempre `{"error": {"code", "message", "details?"}}` |

**Nunca parsees `message`**: es texto para mostrar y puede cambiar. El contrato es `code`.

```json
{
  "error": {
    "code": "validation_failed",
    "message": "The request has invalid fields.",
    "details": { "fields": [{ "field": "password", "code": "too_short" }] }
  }
}
```

---

## 2. Sesión: dos tokens, dos transportes

| Token | Naturaleza | Vida | Transporte |
|---|---|---|---|
| `access_token` | JWT HS256, sin estado | 15 min | `Authorization: Bearer <token>` |
| `refresh_token` | opaco, con estado, rota en cada uso | 30 días | ver abajo |

El access token lleva **solo** `sub`, `iat`, `exp`, `jti`. No lleva roles ni permisos: la
capacidad se resuelve contra la base en cada request, así que quitar un rol surte efecto en la
llamada siguiente.

### Quién recibe qué

El servidor decide según el header `Origin`:

| Cliente | Cómo se lo reconoce | Refresh token |
|---|---|---|
| **Móvil** | sin `Origin` (o uno no permitido) | en el cuerpo JSON |
| **Panel (SPA)** | `Origin` dentro de `CORS_ALLOWED_ORIGINS` | cookie `HttpOnly`, **nunca** en el cuerpo |

Cookie del panel:

```
Set-Cookie: refresh_token=<opaco>; HttpOnly; Secure; SameSite=Lax; Path=/v1/auth
Set-Cookie: csrf_token=<aleatorio>; Secure; SameSite=Lax; Path=/v1/auth
```

`csrf_token` es legible por JavaScript a propósito: es la mitad del double-submit. Pero la
cookie vive con `Path=/v1/auth`, así que un documento servido desde la raíz de la SPA no
puede leerla (y desde otro host no se lee ninguna cookie). Por eso el cuerpo de login y
refresh la repite en el campo `csrf_token`: el panel la toma de ahí, no de la cookie. Solo
lo recibe el panel; para el móvil el campo no existe.

### Reglas del panel (CSRF)

`/v1/auth/refresh` y `/v1/auth/logout` aceptan el token en el cuerpo **o** en la cookie. Si
viene de la cookie, hay que mandar el header:

```
X-CSRF-Token: <csrf_token del cuerpo del login o del último refresh>
```

Sin él, o si no coincide → `403 csrf_invalid`. El móvil no necesita CSRF porque manda el token
explícito en el cuerpo. El valor rota con cada refresh: hay que guardar el `csrf_token` nuevo
del cuerpo y descartar el anterior.

### Rotación y reuso

Cada `/refresh` revoca el token presentado e inserta uno nuevo en la misma *familia*, en la
misma transacción. **Presentar un token ya rotado se trata como robo**: se revoca la familia
completa y la respuesta es `401 refresh_reused`. El cliente debe volver a loguearse.

---

## 3. Códigos de 6 dígitos

Los usan el registro, la recuperación de contraseña y el cambio de email.

- 6 dígitos numéricos, **un solo uso**, vencen a los **10 minutos**.
- Máximo **5 intentos** por código; al agotarlos hay que pedir uno nuevo.
- En la base vive solo el SHA-256. El código en claro existe únicamente en el mail.
- Reenviar un código **invalida el anterior** (en el cambio de email).

---

## 4. Límites

| Flujo | Límite | Respuesta al excederse |
|---|---|---|
| Login | 5 fallos consecutivos | `423 account_locked` + `Retry-After` (15 min) |
| Cambio de email | **1 pedido cada 24 h** | `429 rate_limited` + `Retry-After` |
| Recuperación de contraseña | **5 pedidos cada 24 h** | `202` **silencioso**: no manda nada |
| Ambos flujos con código | 60 s entre pedidos | igual que arriba, según el flujo |

La recuperación es silenciosa a propósito: un `429` revelaría que la cuenta existe, porque una
dirección inexistente nunca agota la cuota.

---

## 5. Autenticación

### 5.1 Registro

**Paso 1 — pedir el código**

```http
POST /v1/auth/signup
Content-Type: application/json

{"email": "ana@melia.test", "name": "Ana", "password": "una-clave-larga-123"}
```

→ `202` sin cuerpo. **Siempre**, exista o no la cuenta: si ya existe, al dueño le llega un
aviso en lugar de un código. La contraseña se hashea con Argon2id antes de guardarse.

La cuenta **no existe todavía**: el registro pendiente vive aparte hasta que se prueba el
email. Durante el flujo el email es inmutable: cambiarlo es empezar de nuevo.

**Paso 2 — confirmar**

```http
POST /v1/auth/signup/verify
{"email": "ana@melia.test", "code": "042317"}
```

→ `201` con el par de tokens. **Auto-login**: no hace falta llamar a `/login` después.

| Error | Cuándo |
|---|---|
| `400 invalid_code` | código incorrecto, vencido, o registro inexistente |
| `429 too_many_attempts` | se agotaron los 5 intentos |

`invalid_code` es el mismo para los tres casos: distinguirlos diría qué direcciones están a
medio registrar.

### 5.2 Login

```http
POST /v1/auth/login
{"email": "ana@melia.test", "password": "una-clave-larga-123", "device": "iPhone de Ana"}
```

→ `200` con el par. `device` es opcional y es la etiqueta que verá el usuario. Si el cliente
es el panel, el cuerpo incluye además `csrf_token` (§2) y el refresh token le llega solo como
cookie.

| Error | Cuándo |
|---|---|
| `401 invalid_credentials` | contraseña incorrecta, cuenta inexistente **o** cuenta inactiva |
| `423 account_locked` | bloqueada por intentos; trae `details.retry_after_seconds` |
| `422 validation_failed` | email mal formado o contraseña vacía |

### 5.3 Refresh

```http
POST /v1/auth/refresh
{"refresh_token": "..."}          # móvil
                                  # panel: sin cuerpo, con cookie + X-CSRF-Token
```

→ `200` con par nuevo. Para el panel el cuerpo trae también el `csrf_token` rotado: hay que
reemplazar el guardado por el nuevo. Errores: `401 refresh_invalid` (desconocido o vencido) y
`401 refresh_reused` (reuso detectado, familia revocada).

### 5.4 Logout

```http
POST /v1/auth/logout
{"refresh_token": "..."}          # o la cookie, con X-CSRF-Token
```

→ `204` **siempre**, incluso con un token desconocido. Revoca la familia.

Si el token vino de la cookie, la misma respuesta vence `refresh_token` y `csrf_token`
(`Max-Age=0`). Si vino de la cookie y falta o no coincide `X-CSRF-Token`, responde
`403 csrf_invalid` y no revoca nada. No lleva `Authorization`: el token presentado es la
prueba, y un access token vencido no debe impedir cerrar la sesión.

### 5.5 Cambio y recuperación de contraseña

**Olvidé mi contraseña**

```http
POST /v1/auth/password/forgot
{"email": "ana@melia.test"}
```

→ `202` siempre. Si la cuenta existe, llega un código; si no, no pasa nada.

```http
POST /v1/auth/password/reset
{"email": "ana@melia.test", "code": "042317", "new_password": "otra-clave-larga-123"}
```

→ `200`. **Revoca todas las sesiones del usuario**, incluida la del que llama. También limpia
el bloqueo por intentos fallidos.

**Cambiar la contraseña (autenticado)**

```http
POST /v1/auth/password/change
Authorization: Bearer <access>

{"current_password": "una-clave-larga-123", "new_password": "otra-clave-larga-123"}
```

→ `200` y **revoca todas las familias**: hay que loguearse otra vez.
`400 invalid_password` si la actual no coincide.

### 5.6 Cambio de email

```http
POST /v1/auth/email/change
Authorization: Bearer <access>

{"new_email": "nueva@melia.test", "password": "una-clave-larga-123"}
```

→ `202`. El código va a la **dirección nueva**, y la contraseña es obligatoria: un access
token robado por sí solo no puede mover la cuenta.

| Error | Cuándo |
|---|---|
| `400 invalid_password` | contraseña incorrecta |
| `409 email_unavailable` | la dirección ya pertenece a otra cuenta |
| `422 validation_failed` | email inválido, o igual al actual (`unchanged`) |
| `429 rate_limited` | más de un pedido en 24 h |

```http
POST /v1/auth/email/change/verify
Authorization: Bearer <access>

{"code": "042317"}
```

→ `200`. El email cambia y **se revocan todas las sesiones**: el dato que identificaba la
cuenta cambió. Después hay que loguearse con la dirección nueva.

### 5.7 Login social (Google / Apple)

```http
POST /v1/auth/social
{"provider": "GOOGLE", "id_token": "<ID token del SDK nativo>", "device": "Pixel 8"}
```

→ `200` con el par. El `id_token` se valida contra el JWKS del proveedor y **se descarta**: no
es una credencial de esta API.

| Error | Cuándo |
|---|---|
| `401 invalid_token` | el token del proveedor no valida |
| `422 validation_failed` | `provider` inválido (`invalid`) o proveedor no configurado (`not_configured`) |

La identidad se resuelve por `(provider, sub)`, **nunca por email**. Un email no verificado por
el proveedor no se guarda; uno verificado que ya pertenece a otra cuenta **no vincula** las
cuentas: se crea la cuenta federada sin email.

---

## 6. Identidad

```http
GET /v1/me
Authorization: Bearer <access>
```

```json
{
  "id": "9f1c...",
  "name": "Ana",
  "email": "ana@melia.test",
  "status": "ACTIVE",
  "email_verified_at": "2026-03-15T10:00:00Z",
  "roles": ["EDITOR"],
  "permissions": ["admin.access", "place.create"],
  "created_at": "2026-03-15T10:00:00Z"
}
```

`name` y `email` pueden ser `null` (login social sin esos datos). **El panel decide qué mostrar
desde `permissions`**, nunca desde el nombre del rol. Un token válido de una cuenta borrada
responde `401`, no `404`.

---

## 7. Administración

Todas requieren `admin.access`; sin él → `403 permission_denied`.

| Método | Ruta | Qué hace |
|---|---|---|
| `GET` | `/v1/admin/users?page&limit&q&status&scope` | lista paginada; `q` busca en nombre o email |
| `PATCH` | `/v1/admin/users/{userId}` | edita `name` y `status` (nunca el email) |
| `POST` | `/v1/admin/users/invitations` | envía una invitación al panel a esa dirección |
| `POST` | `/v1/admin/users/{userId}/password-reset` | envía el código de reseteo al dueño |
| `PATCH` | `/v1/admin/users/{userId}/roles` | **reemplaza** el set de roles |
| `GET` | `/v1/admin/roles` | catálogo con los permisos de cada rol |
| `GET` | `/v1/admin/permissions` | catálogo, ordenado por módulo |
| `GET` | `/v1/admin/audit-events?page&limit&event&userId&emailHash&from&to` | bitácora, más reciente primero |

`scope` no es un tipo guardado: `admin` son los usuarios con un rol activo que otorga
`admin.access`, `app` los que no lo tienen, y `none` los que no tienen ningún rol activo. Un
valor desconocido → `422` (`field: scope`).

Los eventos de la bitácora traen `metadata` como **JSON libre**: su forma depende del evento, así
que no está tipado y no sirve para decisiones automáticas. En el contrato está declarado con
`additionalProperties: true` para que un cliente generado lo trate como objeto abierto y no como
objeto vacío — que es lo que produce un `object` pelado.

```http
PATCH /v1/admin/users/9f1c...
{"name": "Ana María", "status": "SUSPENDED"}
```

→ `200` con el usuario. **El email no se edita acá**: la dirección se prueba antes de que exista
la fila, así que un cambio desde el panel saltearía la verificación. Suspender no invalida las
sesiones vivas: expiran solas.

```http
POST /v1/admin/users/invitations
{"email": "nueva@melia.test", "name": "Nueva"}
```

→ `202`. **No crea nada**: la persona se registra y prueba la dirección con el código, y
recién después se le asigna el rol. El administrador nunca fija la contraseña de otro: para
eso existe `/password-reset`, que manda el código al dueño de la cuenta.

```http
PATCH /v1/admin/users/9f1c.../roles
{"roles": ["EDITOR"]}
```

→ `200` con el usuario actualizado. Es un **reemplazo**, no un delta: un rol que no mandás se
quita. Un nombre inexistente o inactivo rechaza **todo** con `422` (`field: roles`, `code:
unknown`) — la capacidad nunca queda a medio aplicar. Cada cambio se audita con quién lo hizo y
a quién.

`from` y `to` son RFC 3339; un valor inválido → `422`.

`emailHash` es el HMAC-SHA256 que devuelve cada evento en `email_hash`, no la dirección: la
bitácora nunca guarda el email en claro, así que esa es la única forma de seguir un buzón.

### El primer administrador

Asignar roles necesita `admin.access`, y nadie lo tiene hasta que alguien recibe un rol. Se
rompe una vez, desde el servidor:

```bash
docker compose run --rm --entrypoint /usr/local/bin/bootstrap \
  -e EMAIL=ana@melia.test -e ROLE=SUPERADMIN api
```

Reemplaza el set de roles de ese usuario. La cuenta tiene que existir y estar verificada.

---

## 8. Health

| Ruta | Uso |
|---|---|
| `GET /health/live` | liveness; **nunca** toca una dependencia |
| `GET /health/ready` | `200` si postgres y redis responden; `503` si falla alguno requerido |

```json
{"status": "ok", "checks": [
  {"name": "postgres", "status": "ok", "required": true},
  {"name": "redis", "status": "ok", "required": true},
  {"name": "rabbitmq", "status": "ok", "required": false}
]}
```

`status` global: `ok`, `degraded` (falló una dependencia opcional) o `unavailable`.

---

## 9. CORS

El panel es un SPA en otro origen, así que cada llamada es cross-origin y el header
`Authorization` obliga a un preflight.

- Orígenes permitidos: `CORS_ALLOWED_ORIGINS` (lista exacta, **nunca** `*`, porque hay
  credenciales de por medio).
- El preflight `OPTIONS` responde `204` **sin autenticación** y con
  `Access-Control-Allow-Credentials: true`, `Allow-Headers: Authorization, Content-Type,
  X-CSRF-Token` y `Max-Age: 600`.
- Un origen no permitido recibe la respuesta normal **sin** headers CORS.
- El servidor manda `Vary: Origin`.

---

## 10. Catálogo de códigos de error

| `code` | HTTP | Significado |
|---|---|---|
| `validation_failed` | 422 | campos inválidos; mirá `details.fields` |
| `invalid_json` | 400 | el cuerpo no es JSON |
| `invalid_credentials` | 401 | login fallido (sin distinguir el motivo) |
| `account_locked` | 423 | bloqueo temporal; trae `retry_after_seconds` |
| `invalid_code` | 400 | código de 6 dígitos incorrecto o vencido |
| `too_many_attempts` | 429 | se agotaron los intentos del código |
| `rate_limited` | 429 | cuota de pedidos de código; trae `Retry-After` |
| `invalid_password` | 400 | la contraseña actual no coincide |
| `email_unavailable` | 409 | la dirección ya está en uso |
| `review_already_exists` | 409 | ya tenés una reseña activa en ese lugar |
| `not_near_place` | 422 | la coordenada enviada está fuera del radio del lugar |
| `already_checked_in` | 409 | ya hiciste check-in en ese lugar hoy |
| `invitation_already_pending` | 409 | esa persona ya tiene una invitación viva a ese plan |
| `refresh_invalid` | 401 | refresh desconocido o vencido |
| `refresh_reused` | 401 | reuso detectado; la familia está revocada |
| `invalid_token` | 401 | el ID token del proveedor no valida |
| `csrf_invalid` | 403 | falta el header CSRF o no coincide |
| `unauthorized` | 401 | falta el bearer o no es válido |
| `permission_denied` | 403 | autenticado, sin el permiso requerido |
| `not_found` | 404 | ruta o recurso inexistente |
| `internal_error` | 500 | fallo del servidor; el mensaje no expone detalles |

### Códigos de campo

Cuando el error es `validation_failed`, `details.fields` trae pares `{field, code}`. El
vocabulario de `code` es cerrado:

| `code` de campo | Significado |
|---|---|
| `required` | falta un campo obligatorio |
| `invalid` | el formato no sirve (email, código de 6 dígitos, fecha, provider) |
| `too_short` | no llega al mínimo (contraseña: 12 caracteres; `q`/`dish`: 2) |
| `too_long` | excede el máximo (`q`/`dish`: 80 caracteres; `radius`/`bbox`: su tope) |
| `unchanged` | el email nuevo es igual al actual |
| `unknown` | el rol o el atributo pedido no existe o está inactivo |
| `not_configured` | el proveedor social no está habilitado en este servidor |

---

## 11. Descubrimiento de lugares

Lectura pública, **sin autenticación**. Una sola ruta con dos modos, decididos por el
localizador:

| Localizador | Modo | Parámetros propios |
|---|---|---|
| `near=lat,long` | **lista** | `radius` (metros, por defecto 2000, máximo 50000), `sort`, `page`, `limit` |
| `bbox=minLon,minLat,maxLon,maxLat` | **mapa** | `zoom` (0–21, obligatorio con `bbox`) |

`near` lleva latitud primero; `bbox` lleva longitud primero, como GeoJSON. Mandar ambos
localizadores o ninguno → `422 validation_failed`: ninguno trae
`{field: "locator", code: "required"}` y ambos traen `{field: "bbox", code: "invalid"}`.
El cruce del antimeridiano está fuera de alcance y se rechaza (`bbox` inválido).

Filtros en ambos modos: `country` (`^[A-Z]{2}$`), `region` (`^[A-Z]{2}-[A-Z0-9]{1,3}$`) y
`category` (uuid). Un valor inválido → `422` con su `field`. Los parámetros ajenos al modo
(`page` en modo mapa, `zoom` en modo lista) se ignoran. Búsqueda y filtros de la ficha del
lugar (`q`, `dish`, `attributes`, `maxPrice`, `openAt`): ver §11.3.

**La coordenada del pedido se evalúa y se descarta**: no se persiste, no se loguea y nunca
es material de una clave de caché.

### 11.1 Modo lista

```http
GET /v1/places?near=10.4806,-66.9036&radius=2000&sort=distance&page=1&limit=20
```

```json
{
  "mode": "list",
  "page": 1, "limit": 20, "total": 42,
  "sortApplied": "trending",
  "degraded": false,
  "data": [
    {
      "coPlace": "9f1c...", "name": "Arepera El Hatillo",
      "category": { "id": "43c4...", "name": "Arepera" },
      "country": "VE", "region": "VE-A",
      "latitude": 10.480594, "longitude": -66.903606,
      "address": "Calle 1", "distanceMeters": 940,
      "rating": { "average": 4.6, "count": 128 }
    }
  ]
}
```

- `name` es el nombre comercial (`tx_title`). El nombre legal (`tx_name`) y el dueño del
  lugar **nunca** se exponen en esta lectura.
- `distanceMeters` son metros enteros desde `near`; nunca se fabrica.
- `rating` es `null` cuando el worker no publicó agregado para ese lugar; la ausencia no
  es un error. Nunca se calcula agregando reseñas en el pedido.
- `trendingRank` solo aparece con `sort=trending` y ranking de zona existente.

`sort`: `distance` (por defecto, orden real por cercanía sobre el índice GiST), `rating`,
`trending`. **`sortApplied` es el orden que efectivamente corrió**: si se pidió `trending`
y la zona no tenía ranking publicado, la API cae a `rating` y responde `degraded: true` —
así el cliente distingue un ranking frío de un lugar impopular, sin error.

Un radio sin resultados es `200` con `data: []` y `total: 0`: no es un error.

### 11.2 Modo mapa

```http
GET /v1/places?bbox=-66.99,10.41,-66.82,10.54&zoom=12
```

```json
{
  "mode": "map", "zoom": 12, "resolution": 7,
  "total": 311, "truncated": false,
  "clusters": [
    {
      "cell": "872830828ffffff", "resolution": 7, "count": 37,
      "center": { "latitude": 10.48, "longitude": -66.9 },
      "averageRating": 4.3
    }
  ]
}
```

- `total` es la cantidad de lugares detrás de los clusters, no la cantidad de clusters.
- Cada cluster es una **zona**: una celda H3 serializada como string hex (los números JSON
  pierden precisión por encima de 2^53). Trae conteos y agregados neutros y **ningún
  nombre administrativo**: no es un barrio, ni un distrito, ni un municipio.
- `averageRating` es el promedio de los ratings publicados de la celda; es `null` cuando
  se desconoce, nunca `0`.
- `zoom` elige la resolución H3 según la tabla; la celda se deriva por aritmética de
  padres, nunca hay una columna por nivel de zoom.

| `zoom` | resolución H3 |
|---|---|
| 0–4 | 3 |
| 5–7 | 5 |
| 8–10 | 6 |
| 11–13 | 7 |
| 14–16 | 8 |
| 17–21 | 9 |

La consulta del viewport está topeada a **5000 filas**. Un bbox que supera el tope de área
(100 000 km²) se rechaza **antes de consultar** con `422` (`field: bbox`, `code:
too_long`), pidiendo acercar — truncar en silencio haría mentirosos los conteos. Si el
tope se alcanza igualmente, `truncated: true` y los conteos describen la muestra, no el
total.

### 11.3 Búsqueda y filtros

Cinco parámetros más, válidos en los dos modos y combinables entre sí y con el
localizador:

- **`q` — ¿cómo se llama?** Busca en el nombre comercial del lugar, sin importar
  mayúsculas ni acentos: `terraza`, `Térraza` y `TERRAZA` son la misma consulta. Sin
  stemming: `terrazas` **no** matchea `terraza`. Entre 2 y 80 caracteres
  (`{field: "q", code: "too_short"}` / `too_long`).
- **`dish` — ¿quién sirve este plato?** Busca en nombres y descripciones de ítems de
  menú (activos, de menús activos) y devuelve los **lugares** que lo tienen. Misma cota
  de 2 a 80 caracteres. Son dos preguntas distintas con índices distintos: una sola caja
  haría que «arepa» devuelva un lugar llamado Arepa's y pierda el restaurante que las
  sirve.
- **`attributes` — conjuntivo.** `attributes=terraza,petfriendly` exige **ambos**. Un
  código que no está en el catálogo gobernado → `422` `{field: "attributes", code:
  "unknown"}`. Vocabulario vigente: `terraza`, `petfriendly`, `frente_playa`,
  `aire_libre`, `musica_en_vivo`.
- **`maxPrice` — siempre USD.** Matchea lugares con al menos un ítem activo, de un menú
  activo, cuya moneda sea USD y cuyo precio no supere el monto. Un ítem en otra moneda
  **no** matchea: es comportamiento documentado, no un defecto, y no existe parámetro
  `currency`. Negativo o no numérico → `{field: "maxPrice", code: "invalid"}`.
- **`openAt` — pregunta de horario, no de atributo.** Instantáneo ISO-8601
  (`2026-11-08T03:30:00Z`); se evalúa en la zona horaria **del lugar** e incluye los
  horarios que cruzan la medianoche: un local que abre 20:00 y cierra 02:00 figura
  abierto a las 23:00. Si no es un instante válido → `{field: "openAt", code:
  "invalid"}`.

Una búsqueda sin resultados es `200` con la página vacía, nunca `404`.

---

## 12. Reseñas

Las reseñas son la primera escritura de dominio de la API. **Una por usuario y lugar**: lo
garantiza el índice único parcial `ux_tb_review_user_place_active`, nunca una consulta
previa (chequear antes de insertar tiene carrera).

| Método | Ruta | Autenticación | Qué hace |
|---|---|---|---|
| `GET` | `/v1/places/{coPlace}/reviews?page&limit` | pública | lista paginada, la más reciente primero |
| `POST` | `/v1/places/{coPlace}/reviews` | bearer | crea tu reseña (`201`) |
| `PATCH` | `/v1/places/{coPlace}/reviews/{coReview}` | bearer, solo el autor | edita `rating` y/o `comment` |
| `DELETE` | `/v1/places/{coPlace}/reviews/{coReview}` | bearer, solo el autor | borrado **lógico** (`204`) |
| `PATCH` | `/v1/admin/reviews/{coReview}` | bearer + `review.moderate` | fija `status` (`ACTIVE`/`INACTIVE`/`MODERATED`) |

Un lugar borrado lógico o `INACTIVE` responde `404` en **todas** estas rutas, igual que un
lugar inexistente: no se pueden enumerar lugares retirados por diferencias de error.

### Crear

```http
POST /v1/places/9f1c.../reviews
Authorization: Bearer <access>
{"rating": 5, "comment": "La mejor arepa de Chacao."}
```

→ `201` con la reseña. `rating` es entero de 1 a 5 (el CHECK de la tabla) y `comment`
tiene tope de 2000 caracteres, validado en el borde (`422`, `field: comment`, `code:
too_long`), no en la base.

Si ya tenés una reseña activa (no borrada) en ese lugar → `409 review_already_exists`.
Es la traducción del índice único parcial, nunca un chequeo previo ni un 500.

### Visibilidad

Todos ven las reseñas `ACTIVE` no borradas. Una reseña `INACTIVE`, `MODERATED` o borrada
lógicamente solo la ven **su autora** y quienes tienen `review.moderate` — la lista es
pública, pero si mandás el bearer la API sabe quién sos y te muestra también las tuyas. El
email del autor nunca se expone: `author` trae solo `id` y `name`.

### Editar y borrar (solo el autor)

```http
PATCH /v1/places/9f1c.../reviews/7a2b...
Authorization: Bearer <access>
{"rating": 4}
```

→ `200` con la reseña actualizada. Al menos un campo es obligatorio (`rating` o
`comment`). Cualquier otro usuario → `403 permission_denied`. Editar no cambia `status`.

```http
DELETE /v1/places/9f1c.../reviews/7a2b...
Authorization: Bearer <access>
```

→ `204`. El borrado es **lógico** (`dt_deleted_at`), así que podés reseñar ese lugar de
nuevo después — por eso el índice único es parcial.

### Moderación

```http
PATCH /v1/admin/reviews/7a2b...
Authorization: Bearer <access>
{"status": "MODERATED"}
```

Requiere el permiso `review.moderate` (ya sembrado); sin él → `403 permission_denied`.
Solo usa valores del enum `review_status` existente: no hay permisos ni estados nuevos.
Una reseña `MODERATED` deja de verse para todos salvo su autora y los moderadores.

### Historia

Cada escritura anuncia al actor con `set_config('app.current_user_id', ...)` al abrir su
transacción, así el trigger existente registra `co_actor_user` en
`core.tb_review_history`. El servicio **nunca** inserta en la tabla de historia: la
escribe el trigger.

---

## 13. Recetas

**Registro completo (móvil)**

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:9000/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@melia.test","name":"Ana","password":"una-clave-larga-123"}'
# 202 — el código llega por mail (Mailpit en dev: http://localhost:8025)

curl -s -X POST localhost:9000/v1/auth/signup/verify \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@melia.test","code":"042137"}'
```

**Renovar sesión**

```bash
curl -s -X POST localhost:9000/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"<refresh>"}'
```

**Panel: login con cookie y refresh con CSRF**

```bash
# el Origin hace que el servidor devuelva cookie en vez de token en el cuerpo
curl -s -c jar.txt -X POST localhost:9000/v1/auth/login \
  -H 'Content-Type: application/json' -H 'Origin: http://localhost:4200' \
  -d '{"email":"ana@melia.test","password":"una-clave-larga-123"}'

CSRF=$(grep csrf_token jar.txt | awk '{print $7}')
curl -s -b jar.txt -X POST localhost:9000/v1/auth/refresh \
  -H 'Content-Type: application/json' -H 'Origin: http://localhost:4200' \
  -H "X-CSRF-Token: $CSRF" -d '{}'
```

**Consumir el panel según permisos**

```bash
TOKEN=...
curl -s localhost:9000/v1/me -H "Authorization: Bearer $TOKEN" | jq '.permissions'
# el SPA muestra "Usuarios" solo si permissions incluye "admin.access"
```

---

## 14. Checklist para un cliente nuevo

1. Login o registro → guardar `access_token` y `refresh_token` (el panel no recibe el refresh:
   va en cookie).
2. Mandar `Authorization: Bearer` en cada llamada autenticada.
3. Ante `401`, intentar **un** `/refresh`; si devuelve `401`, mandar al login.
4. Ante `423` o `429`, respetar `Retry-After` y no reintentar en loop.
5. Leer `error.code`, nunca `message`.
6. En el panel: mandar `X-CSRF-Token` en refresh y logout, y nunca leer la cookie
   `refresh_token`.

---

## 13. Grafo social

### 13.1 Seguir

```http
POST /v1/follows          { "coUser": "<uuid>" }   → 204
DELETE /v1/follows/{coUser}                        → 204
GET /v1/follows?direction=following|followers&page=1&limit=20
```

El grafo es asimétrico: seguir a alguien no dice nada sobre si esa persona te sigue. Ambas
mutaciones son **idempotentes** — repetir un follow o borrar uno que no existe también responde
`204`, porque el grafo es un conjunto y no un historial de intenciones.

`mutual: true` en el listado marca un amigo: existe también la arista inversa. Se calcula en
cada lectura y **nunca se guarda**: el grafo cambia todos los días y un flag almacenado se
pudre en uno. Seguirte a vos mismo es `422` con `field: coUser`.

### 13.2 Check-in

```http
POST /v1/places/{coPlace}/checkins   { "near": "10.4806,-66.9036" }   → 201
```

La coordenada **prueba** la presencia y después se descarta: solo se guarda el veredicto
(`PROXIMITY`). No se almacena la coordenada ni la distancia, así que no hay rastro de ubicación
que purgar.

- Fuera del radio del lugar → `422 not_near_place`. No se degrada a "afirmación libre": decir
  "no estás ahí" es honesto; registrar lo que la persona no hizo envenena justamente la señal
  que este endpoint existe para dar.
- Segundo check-in el mismo día en el mismo lugar → `409 already_checked_in`. La regla vive en
  el esquema (índice único sobre usuario, lugar y día), no en el servicio.
- Un lugar borrado o inactivo → `404`, igual que en el resto de las rutas de lugar.

Un check-in es un hecho **append-only**: no se edita ni se borra lógicamente, y por eso no está
en el conjunto de tablas de historia.

### 13.3 Moderación de reseñas

```http
GET /v1/admin/reviews?status=MODERATED&placeId=<uuid>&authorId=<uuid>&page=1&limit=20
PATCH /v1/admin/reviews/{coReview}   { "status": "ACTIVE" | "INACTIVE" | "MODERATED" }
```

Ambas requieren el permiso `review.moderate`.

El listado existe porque la ruta de moderación recibe un `coReview` y **nada más lo hacía
descubrible**: una pantalla de moderación sin listado no tiene de dónde salir. A diferencia del
listado público, este **no aplica el predicado de visibilidad**: devuelve todas las reseñas,
incluidas las moderadas y las borradas lógicamente, con la más reciente primero. Todos los
filtros son opcionales y el valor vacío lista todo.

Ojo con la interacción ya conocida: moderar **no** borra. Una reseña `MODERATED` sigue visible
para su autor y para el moderador, y desde la migración 20 el autor puede volver a reseñar ese
lugar. Si querés impedir que alguien publique, eso es bloqueo de cuenta, no moderación.

---

## 14. Backoffice de lugares

Cinco rutas, todas con bearer y permisos ya sembrados. El **Viewer de ownership** es el que
decide el alcance: quien tiene `place.manage_all` ve todo el catálogo; quien solo tiene
`place.update` ve únicamente los lugares donde `co_owner_user` es su propia cuenta. Los lugares
curados por el backoffice (`co_owner_user` NULL) no son visibles para un editor: pertenecen al
panel.

```http
GET    /v1/admin/places?q=&status=ACTIVE|INACTIVE|DELETED&categoryId=&country=&region=&page=&limit=
GET    /v1/admin/places/{coPlace}
POST   /v1/admin/places
PATCH  /v1/admin/places/{coPlace}
DELETE /v1/admin/places/{coPlace}
```

| Ruta | Permiso | Nota |
|---|---|---|
| `GET /v1/admin/places` | `place.update` | Paginado, alcance por ownership. Por defecto lista el catálogo vivo; `status=DELETED` lista los retirados |
| `GET /v1/admin/places/{coPlace}` | `place.update` | Una fila que no podés ver responde **404**, no 403 |
| `POST /v1/admin/places` | `place.manage_all` | Solo el backoffice crea y asigna dueño: el catálogo se cura, y el autoservicio es otra decisión |
| `PATCH /v1/admin/places/{coPlace}` | `place.update` | Parcial: campo ausente = sin cambios; `null` explícito limpia un nullable. Solo `place.manage_all` reasigna dueño |
| `DELETE /v1/admin/places/{coPlace}` | `place.delete` | **Borrado lógico**: la fila queda, y discovery, reseñas y check-ins dejan de servirla |

**Por qué 404 y no 403** en una fila ajena: un 403 confirma que la fila existe. Un llamador que
no puede verla no debe poder enumerar el catálogo por diferencia de códigos.

Toda escritura anuncia el actor (`app.current_user_id`) en la misma transacción, así que el
trigger de historia de `tb_place` registra `co_actor_user`. El servicio nunca escribe una fila
de historia.

---

## 15. Planes colaborativos

```http
POST   /v1/plans                                    { name, description?, visibility?, scheduledAt?, partySize? }
GET    /v1/plans?page=&limit=
GET    /v1/plans/{coRoute}
PATCH  /v1/plans/{coRoute}
POST   /v1/plans/{coRoute}/members
DELETE /v1/plans/{coRoute}/members/{coUser}
```

Un festival público y un viaje privado son la misma entidad con puertas distintas: la
visibilidad es un atributo del plan y la membresía es una relación que lleva el rol
(`OWNER` / `EDITOR` / `VIEWER`).

**Reglas que importan:**

- `visibility` arranca en `PRIVATE`. Un plan nunca se vuelve público por omisión.
- El creador queda como `OWNER` en la misma transacción que crea el plan: un plan sin dueño es
  inválido.
- **El self-join a un plan público otorga `VIEWER`, siempre.** Una puerta abierta que reparte
  `EDITOR` es un plan que cualquiera puede reordenar y vaciar, y un festival es el peor caso.
  Promover a `EDITOR` es una acción del dueño.
- Unirse a un plan **privado** responde `404`: no confirma que exista.
- Un plan **visible** con rol insuficiente responde `403`: ya sabés que existe, te falta permiso.
- Cambiar la visibilidad es **solo del `OWNER`**: pasar a público toca a todos los ya invitados.
- El dueño no puede salir del plan por su cuenta: primero transfiere la propiedad.
- Un plan inactivo (moderado) responde `404` a los no-owner, y sus miembros conservan la
  lectura para no perder el itinerario.

---

## 16. Catálogo de categorías

```http
GET /v1/place-categories?status=ACTIVE&page=1&limit=100
```

Público, sin bearer. Es **taxonomía, no dato de cliente**: el mismo listado sirve para el
select del panel y para los filtros de discovery del móvil, así que no tiene sentido esconderlo
detrás de un permiso de backoffice.

`status=ACTIVE` devuelve solo las elegibles; sin `status` devuelve el catálogo vivo. El orden es
por nombre, para que el vocabulario mantenga un orden estable entre llamadas. `code` es la clave
estable (`arepera`, `hotel`, `rumba`): usá esa en los filtros, no el uuid.

### El lugar en la fila de moderación

El listado `GET /v1/admin/reviews` agrega `place: {id, title}` a cada fila. No está en la
lectura pública a propósito: ahí el cliente ya conoce el lugar porque consultó por `coPlace`.
En el listado admin sí hace falta, porque un moderador puede tener `review.moderate` sin
`place.update` y entonces no puede resolver el uuid por su cuenta.

### 15.1 Itinerario y votos

```http
GET    /v1/plans/{coRoute}/places
POST   /v1/plans/{coRoute}/places                          { coPlace, order? }
DELETE /v1/plans/{coRoute}/places/{coPlace}
POST   /v1/plans/{coRoute}/places/{coPlace}/votes          { vote: -1 | 1 }
```

El itinerario se ordena por **votos netos descendente**, y los empates se rompen por el orden
manual y después por fecha de creación. Ese desempate es lo que lo hace determinístico: dos
lecturas seguidas devuelven el mismo orden, y una lista votada no se reordena sola entre pedidos.

- Agregar o quitar lugares es de `EDITOR` u `OWNER`. Un `VIEWER` lee y vota: no da forma al plan.
- **Solo un miembro vota.** Un voto de alguien que no pertenece al plan se rechaza con `403`, no
  se descarta en silencio. En un plan público cualquiera puede *leer*; votar sigue siendo de
  miembros.
- Votar dos veces cambia el voto, no agrega otro: un miembro tiene como máximo un voto por lugar.
  Por eso `nu_vote` es `-1` o `1` y no existe el `0` — no votar es simplemente no tener fila.
- Sacar un lugar del plan borra sus votos: la FK es compuesta a `(co_route, co_place)`, así que
  tampoco se puede votar un lugar que no está en el plan.

### 15.2 Invitaciones

```http
POST   /v1/plans/{coRoute}/invitations            { coUser, expiresAt, mode?, message?, hint? }
GET    /v1/plans/{coRoute}/invitations            ← la vista del anfitrión
GET    /v1/invitations                            ← mi bandeja
POST   /v1/invitations/{coInvitation}/accept
POST   /v1/invitations/{coInvitation}/decline
```

Una invitación tiene ciclo de vida propio (`PENDING → ACCEPTED | DECLINED | EXPIRED | REVOKED`),
no es un flag ni una membresía con estado. Invitar es de `EDITOR` u `OWNER`; **un no-miembro no
puede invitar**.

- **Aceptar crea la membresía `VIEWER` en la misma transacción.** Aceptar nunca da `EDITOR`.
- **Rechazar es terminal y no crea nada.**
- **El vencimiento lo impone la lectura.** Toda lectura y todo aceptar exigen
  `dt_expires_at > now()`: una invitación vencida no se puede aceptar aunque el sweeper esté
  caído. La columna `expired` te dice el veredicto sin que tengas que comparar fechas.
- **Visibilidad asimétrica:** el anfitrión ve el estado de todos; un invitado ve solo el suyo.
  Quién rechazó no es información de la comunidad.
- Una invitación vencida, ya respondida o ajena responden `404`, indistinguibles a propósito.
- `SURPRISE` exige al menos `message` o `hint`: un sobre vacío no es una sorpresa.
- **El destape** lo dice el campo `revealed`: una invitación `PLAIN` siempre lo trae en `true`;
  una `SURPRISE` lo pasa a `true` cuando su regla se cumple — al aceptar (`ON_ACCEPT`) o a la
  hora del plan (`AT_SCHEDULED`, el default). Mientras sea `false`, el itinerario del plan
  responde `403` a ese invitado: el itinerario **es** el lugar, y mostrarlo antes de tiempo
  rompe la sorpresa. Un miembro ya está adentro y nunca se le oculta nada.
- Si el plan no tiene fecha, `AT_SCHEDULED` no puede cumplirse: el destape ocurre al aceptar.
- El **sweeper de higiene** corre en el worker y pasa a `EXPIRED` las invitaciones vencidas.
  Es higiene, no corrección: el predicado de lectura ya las trataba como inutilizables.

### 13.3 Bloqueo

```http
POST   /v1/blocks          { "coUser": "<uuid>" }
GET    /v1/blocks
DELETE /v1/blocks/{coUser}
```

Un bloqueo es una **relación, no un flag**, y eso decide tres cosas:

- **Aplica en las dos direcciones.** El bloqueado tampoco te ve: ni en su lista de follow, ni
  en las reseñas. Un bloqueo de una sola dirección no protege a nadie.
- **Sobrevive a un unfollow.** No es el inverso de seguir.
- **No necesita que exista un follow.** Se puede bloquear a un desconocido.

Ambas mutaciones son idempotentes (`204`), y bloquearse a uno mismo es `422` con
`field: coUser` — lo rechaza el servicio y también un `CHECK` en la base.

Un moderador con `review.moderate` sigue viendo todo: la moderación no es una relación social.

---

## 17. Notificaciones

```http
GET   /v1/notifications/preferences
PATCH /v1/notifications/preferences   { "kind": "invitation.received", "enabled": false }
```

La entrega hoy es **email**, por la misma cola que ya usa el servicio. Push espera al cliente
móvil, y un canal que nadie puede recibir no es un canal.

- Cada tipo se puede apagar por separado, y **la ausencia de preferencia significa activado**:
  así un tipo nuevo no requiere rellenar una fila por cada usuario.
- `GET` devuelve el **catálogo completo** con el switch de cada tipo, no solo los apagados: el
  cliente no tiene que inferir qué existe.
- Un `kind` fuera del catálogo es `422` con `field: kind, code: unknown`. Un switch para un tipo
  que no existe es un switch que miente.
- **La notificación es cortesía, nunca condición:** si el correo falta, el usuario la apagó o la
  cola está caída, se registra y se sigue. La invitación ya existe cuando esto corre, y una
  cortesía no puede hacer fallar aquello que anuncia.

Tipos disponibles hoy: `invitation.received`.

---

## 18. Moderación de planes e invitaciones

```http
GET    /v1/admin/plans?status=ACTIVE|INACTIVE&ownerId=&visibility=&page=&limit=      plan.moderate
PATCH  /v1/admin/plans/{coRoute}          { "status": "INACTIVE" }                   plan.moderate
GET    /v1/admin/invitations?status=&planId=&inviteeId=&page=&limit=                invitation.moderate
DELETE /v1/admin/invitations/{coInvitation}                                        invitation.moderate
```

El panel **modera, no participa**: no se une a un plan, no invita, no vota. Por eso los permisos
son propios (`plan.moderate`, `invitation.moderate`) y no `admin.access`, que solo significa
"puede entrar al panel".

- **Suspender un plan** (`status: INACTIVE`) lo saca de circulación: deja de ser visible para
  quien no es miembro. Los miembros conservan la lectura para no perder su itinerario.
- **Revocar una invitación** la pasa a `REVOKED` y **ya no se puede aceptar** — el aceptar solo
  matchea filas `PENDING`, así que la regla no necesita código extra. Revocar no crea membresía.
- Los dos listados devuelven todo el sitio, sin alcance por ownership: moderar exige ver.
- `expiresAt` y `revealed` viajan en la fila de invitación, así el moderador ve qué está vivo,
  qué venció y qué nunca se respondió.

### 14.1 Horarios y atributos de un lugar

```http
GET /v1/admin/places/{coPlace}/schedules     PUT /v1/admin/places/{coPlace}/schedules   { schedules: [...] }
GET /v1/admin/places/{coPlace}/attributes    PUT /v1/admin/places/{coPlace}/attributes  { codes: [...] }
```

Ambos requieren `place.update` y respetan el alcance por ownership del resto del backoffice.

- **Se reemplaza el conjunto completo**, no slot por slot. Un operador edita "el horario de este
  lugar", y un conjunto semanal aplicado a medias respondería "cerrado" un día que nadie tocó.
- Un `close` igual o anterior a `open` **cruza medianoche**: así el modelo ya dice "abre 20:00,
  cierra 02:00". Horas iguales se rechazan, porque se leen como cerrado todo el día o abierto
  todo el día según quién pregunte.
- Un atributo fuera del catálogo es `422` con `field: codes, code: unknown`. Descartarlo en
  silencio dejaría al operador creyendo que el filtro va a encontrarlo.
- El `GET /v1/admin/places/{coPlace}` ya trae `schedules` y `attributes`, así el formulario de
  edición se llena con una sola llamada. El listado NO los trae: serían dos consultas por fila.

Estos dos endpoints son los que vuelven operables los filtros **`openAt`** y de **atributos** de
discovery, que hasta ahora solo se podían alimentar por SQL.

### 14.2 Menú y platos

```http
GET /v1/admin/places/{coPlace}/menu     PUT /v1/admin/places/{coPlace}/menu   { menus: [...] }
GET /v1/place-attributes                (público)
```

El `PUT` reemplaza **la carta completa** en una transacción: un menú aplicado a medias mostraría
un precio que el filtro de discovery lee y el cliente nunca ve.

- Cada plato exige `currency` del catálogo de monedas: un número sin moneda no significa nada, y
  el modelo tampoco lo acepta. Una moneda desconocida es `422` con `field: menus`.
- Un precio negativo es `422`, no una violación de constraint: el cliente tiene que poder leer
  por qué falló.
- Una sección **sin platos es válida**: es un estado real (carta cargada, platos todavía no).

`GET /v1/place-attributes` es el vocabulario **público** de atributos — el `select` del panel y
los filtros del móvil necesitan la misma lista, igual que con las categorías. Usá `code` en los
filtros (`terraza`, `petfriendly`); el uuid es del catálogo y cambia entre entornos.

### 14.3 Fotos

```http
POST   /v1/admin/places/{coPlace}/photos        multipart/form-data: file, type?, order?
DELETE /v1/admin/places/{coPlace}/photos/{coPhoto}
```

Ambos requieren `place.update` y respetan el alcance por ownership.

- **Máximo 5 MB** por archivo. Más grande devuelve `422` con `field: file, code: too_long`.
- Solo se aceptan `image/jpeg`, `image/png` e `image/webp`, y **se verifica por contenido**, no
  por el header que declara el cliente: un ejecutable renombrado es `422`.
- `type` acepta `COVER` o `GALLERY` (default `GALLERY`). **Subir una `COVER` retira la anterior**,
  que es lo que un cliente quiere decir con "poné esta de portada". Además la base tiene un índice
  único parcial: dos portadas vivas son imposibles, no solo improbables.
- El borrado es **lógico**: la fila queda para auditar qué se publicó.
- La URL devuelta es relativa al origen del API (`/media/places/...`). Hoy los bytes viven en un
  **volumen nombrado de Docker** (`media:/app/media`); la interfaz de storage permite pasar a S3
  sin cambiar estos endpoints ni el contrato.
