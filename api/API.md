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

`csrf_token` es legible por JavaScript a propósito: es la mitad del double-submit.

### Reglas del panel (CSRF)

`/v1/auth/refresh` y `/v1/auth/logout` aceptan el token en el cuerpo **o** en la cookie. Si
viene de la cookie, hay que mandar el header:

```
X-CSRF-Token: <valor de la cookie csrf_token>
```

Sin él, o si no coincide → `403 csrf_invalid`. El móvil no necesita CSRF porque manda el token
explícito en el cuerpo.

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

→ `200` con el par. `device` es opcional y es la etiqueta que verá el usuario.

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

→ `200` con par nuevo. Errores: `401 refresh_invalid` (desconocido o vencido) y
`401 refresh_reused` (reuso detectado, familia revocada).

### 5.4 Logout

```http
POST /v1/auth/logout
{"refresh_token": "..."}          # o la cookie, con X-CSRF-Token
```

→ `204` **siempre**, incluso con un token desconocido. Revoca la familia.

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
| `GET` | `/v1/admin/users?page&limit&q&status` | lista paginada; `q` busca en nombre o email |
| `PATCH` | `/v1/admin/users/{userId}/roles` | **reemplaza** el set de roles |
| `GET` | `/v1/admin/roles` | catálogo con los permisos de cada rol |
| `GET` | `/v1/admin/permissions` | catálogo, ordenado por módulo |
| `GET` | `/v1/admin/audit-events?page&limit&event&userId&from&to` | bitácora, más reciente primero |

```http
PATCH /v1/admin/users/9f1c.../roles
{"roles": ["EDITOR"]}
```

→ `200` con el usuario actualizado. Es un **reemplazo**, no un delta: un rol que no mandás se
quita. Un nombre inexistente o inactivo rechaza **todo** con `422` (`field: roles`, `code:
unknown`) — la capacidad nunca queda a medio aplicar. Cada cambio se audita con quién lo hizo y
a quién.

`from` y `to` son RFC 3339; un valor inválido → `422`.

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
| `too_short` | no llega al mínimo (contraseña: 12 caracteres) |
| `too_long` | excede el máximo |
| `unchanged` | el email nuevo es igual al actual |
| `unknown` | el rol pedido no existe o está inactivo |
| `not_configured` | el proveedor social no está habilitado en este servidor |

---

## 11. Recetas

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

## 12. Checklist para un cliente nuevo

1. Login o registro → guardar `access_token` y `refresh_token` (el panel no recibe el refresh:
   va en cookie).
2. Mandar `Authorization: Bearer` en cada llamada autenticada.
3. Ante `401`, intentar **un** `/refresh`; si devuelve `401`, mandar al login.
4. Ante `423` o `429`, respetar `Retry-After` y no reintentar en loop.
5. Leer `error.code`, nunca `message`.
6. En el panel: mandar `X-CSRF-Token` en refresh y logout, y nunca leer la cookie
   `refresh_token`.
