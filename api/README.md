# api — melia service

Servicio Go que es dueño de la API y de las escrituras. Reemplaza al andamiaje Laravel
(`api-core/`), que no tenía código de producto.

**Para consumir la API, la guía es [`API.md`](API.md).** El contrato legible por máquina es
[`openapi.yaml`](openapi.yaml). Este archivo es para quien corre y mantiene el servicio.

## Requisitos

- Go 1.27
- PostgreSQL 15 (base `melia`, esquemas `seg` + `core`), Redis 7, RabbitMQ 3
- Docker, para el stack completo

## Correr

Con Docker, desde la raíz del repositorio:

```bash
docker compose up --build
```

El servicio escucha en `:8080` dentro del contenedor y se publica como `9000`.

Local, con el stack de infraestructura ya arriba:

```bash
cp .env.example .env      # exportá las variables; el servicio lee el entorno, no el archivo
go run ./cmd/api
go run ./cmd/worker       # consumidor de la cola de correo
```

## Componentes

| Binario | Qué hace |
|---|---|
| `cmd/api` | HTTP: auth, identidad, administración, health |
| `cmd/worker` | consume `melia.mail` y entrega por SMTP |
| `cmd/bootstrap` | otorga el primer administrador (ver `API.md` §7) |

## Configuración

El servicio lee **solo el entorno**. Una variable requerida faltante aborta el arranque
nombrando la variable; no hay fallback silencioso a otra base.

| Variable | Requerida | Default | Qué es |
|---|---|---|---|
| `DATABASE_URL` | sí | — | DSN de PostgreSQL |
| `REDIS_ADDR` | sí | — | `host:port` |
| `JWT_SECRET` | sí | — | clave HS256, mínimo 32 caracteres |
| `HTTP_ADDR` | no | `:8080` | dirección de escucha |
| `RABBITMQ_URL` | no | vacío | sin él, registro y recuperación quedan deshabilitados |
| `DB_INIT_DIR` | no | `/app/database/init` | archivos `NN-*.sql` del esquema |
| `DB_AUTO_MIGRATE` | no | `true` | aplica archivos pendientes al arrancar |
| `CORS_ALLOWED_ORIGINS` | no | vacío | orígenes del panel, separados por coma; vacío = sin CORS |
| `COOKIE_SECURE` | no | `true` | `false` solo para http local |
| `ACCESS_TOKEN_TTL` | no | `15m` | vida del access token |
| `REFRESH_TOKEN_TTL` | no | `720h` | vida del refresh token |
| `LOGIN_MAX_ATTEMPTS` | no | `5` | fallos antes del bloqueo |
| `LOGIN_LOCK_DURATION` | no | `15m` | duración del bloqueo |
| `MAIL_QUEUE` | no | `melia.mail` | cola de correo |
| `SMTP_ADDR` | no | `mailpit:1025` | destino SMTP del worker |
| `MAIL_FROM` | no | `no-reply@melia.test` | remitente |
| `GOOGLE_CLIENT_ID` | no | vacío | habilita el login social de Google |
| `APPLE_CLIENT_ID` | no | vacío | habilita el login social de Apple |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |

## Esquema

`database/init/NN-*.sql` es la única fuente de verdad y **el servicio es el único aplicador**.
La imagen copia ese directorio a `/app/database/init` (nunca bind-mount: un directorio del host
que el usuario del contenedor no puede leer aborta el arranque). Un cambio de DDL requiere
reconstruir la imagen.

Al arrancar aplica los archivos pendientes y registra cada uno en `seg.tb_schema_migration`, así
que reiniciar es no-op. Si el ledger está vacío pero ya existe `seg.tb_user`, **adopta** el
esquema (lo registra sin ejecutarlo) y avisa; `database/verify_base_model.sql` es el chequeo de
deriva. `database/provision/01-create-database.sql` es aprovisionamiento, no migración: no lo
aplica ni el contenedor ni el servicio.

## Tests

```bash
go test ./...        # sin base, Redis ni RabbitMQ: los tests usan fakes
go vet ./...
gofmt -l .
```

Las pruebas de carga viven en [`../tests/k6/`](../tests/k6/README.md): `smoke.js` no escribe
nada, `signup.js` y `auth-flow.js` sí. El viaje de `auth-flow.js` recorre registro, login,
lockout, refresh, reuso, logout, CSRF, identidad, RBAC, recuperación de contraseña y cambio de
email, leyendo los códigos desde Mailpit.

## Mail en desarrollo

El worker entrega a **Mailpit**, que atrapa todo y lo muestra en <http://localhost:8025>. Nada
sale de la máquina.

## Estructura

```
cmd/api/          entrada HTTP: config, wiring, señales, apagado ordenado
cmd/worker/       consumidor de la cola: entrega por SMTP
cmd/bootstrap/    otorga el primer administrador
internal/config/  parseo y validación del entorno
internal/httpapi/ router, handlers, middleware de auth y CORS, cookies
internal/db/      pool pgx, reintento, bootstrap del esquema
internal/session/ access JWT, refresh rotativo, familias
internal/signup/  registro pendiente y verificación del código
internal/login/   autenticación con bloqueo
internal/password/ recuperación y cambio de contraseña
internal/emailchange/ cambio de dirección
internal/social/  validación de ID tokens (go-oidc)
internal/identity/ perfil, RBAC y administración
internal/audit/   bitácora append-only
internal/security/ Argon2id, códigos, cuotas
internal/queue/   publicador AMQP
internal/mailer/  envío SMTP
internal/probe/   sondas de readiness
```

## Notas de operación

- **Un solo aplicador de DDL.** Nunca agregues un mount `docker-entrypoint-initdb.d`: dos
  aplicadores chocan con `schema "seg" already exists` y el contenedor entra en crash loop.
- **Sin proxy adelante**: `r.RemoteAddr` es la IP del cliente, que es lo que guarda la
  auditoría. Cuando aparezca un borde (LB/ingress), `X-Forwarded-For` se confía solo desde ese
  borde.
- **Auditoría best-effort**: si escribir en `seg.tb_security_event` falla, se loguea y la
  operación sigue. Un log roto no puede tumbar un login.
- **Secretos en dev**: el compose trae credenciales de desarrollo y un `JWT_SECRET` de juguete.
  En producción vienen del gestor de secretos.
