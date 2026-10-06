# goapp

A small Go mini-framework for multi-domain REST APIs on top of **Gin + GORM +
Uber fx**. Each domain just *declares* itself; the library handles wiring, the DB
connection, migrations, routing, auth, logging, and graceful shutdown. `main.go`
barely ever changes.

> Current module path: `github.com/kamu/goapp`. Change it if you publish under
> your own repository path.

## Features

- **Wiring by type via fx** — constructors are resolved automatically from their
  parameter types. No service locator, `container.Get()`, or type assertions.
- **Per-domain declaration** — `Provide` / `Handler` / `Migrate` inside a single
  `goapp.Module(...)`.
- **Config from the environment** — DB, port, prefix, JWT, logging
  (`ConfigDefault()`).
- **JWT auth** (`reqctx`) — middleware parses the Bearer token; access + refresh
  tokens; an `ApiHeader` (userId, ip, requestId, …) flows into services through
  `context.Context`.
- **BaseRepository[T]** — generic CRUD.
- **Standard response** — `{ success, requestId, data, error }`.
- **Outbound adapter** (`httpx`) — a shared HTTP client (timeout + `X-Request-ID`
  forwarding); headers are configured per adapter.
- **Errors with stacks** (`errs`) — the stack trace points to the error's origin.
- **Structured logging** (`logging`) — one JSON record per request, ready for
  Kibana Discover; types `api` / `db` / `adaptor` / `error` / `app`, all sharing
  one `request_id`, and toggleable per type.

## Layout

```
goapp/                 # library (module github.com/kamu/goapp)
├── app.go             # Config, Run, newDB, newEngine, autoMigrate, routing, server
├── module.go          # Module / Provide / Handler / Migrate
├── config.go          # DatabaseConfig + ConfigDefault (from env)
├── repository.go      # BaseRepository[T] (generic CRUD)
├── reqctx/            # ApiHeader, middleware, JWT (parse + Signer), transport
├── httpx/             # JSON HTTP client for adapters (outbound)
├── errs/              # errors that carry a stack trace
├── logging/           # structured JSON logger + middleware + GORM logger
└── response/          # standard response format
```

## Quick start

```go
package main

import (
    "github.com/kamu/goapp"

    _ "yourapp/internal/domain/pasien"   // blank imports register each domain
    _ "yourapp/internal/domain/dokter"
)

func main() {
    goapp.Run(goapp.ConfigDefault()) // all configuration comes from the environment
}
```

```bash
DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=secret DB_NAME=mydb \
JWT_SECRET=supersecret \
go run .
```

## Configuration (environment)

| Env | Default | Description |
|-----|---------|-------------|
| `DB_HOST` / `DB_PORT` | `localhost` / `5432` | PostgreSQL connection |
| `DB_USER` / `DB_PASSWORD` / `DB_NAME` | — | credentials & database name |
| `DB_SSLMODE` | `disable` | SSL mode |
| `DB_TIMEZONE` | — | e.g. `Asia/Jakarta` |
| `PORT` | `:8080` | listen address |
| `API_PREFIX` | `/api/v1` | route prefix |
| `AUTO_MIGRATE` | `true` | run AutoMigrate on boot |
| `JWT_SECRET` | — | HMAC secret (empty disables auth) |
| `JWT_ACCESS_TTL` | `15m` | access token lifetime |
| `JWT_REFRESH_TTL` | `168h` | refresh token lifetime |
| `LOG_FILE` | stdout | JSON log destination file |
| `LOG_ENABLED` | `true` | master switch for all logging |
| `LOG_DISABLE` | — | silence specific types, e.g. `db,adaptor` |

## Adding a domain

Create `internal/domain/<name>/` with the entity, dto, repository, service, and
handler (which has `RegisterRoutes(r *gin.RouterGroup)`), then:

```go
var Module = goapp.Module("pasien",
    goapp.Provide(NewRepository, NewService),
    goapp.Handler(NewHandler),
    goapp.Migrate(&Pasien{}),
)
```

Add one blank import in `main.go`: `_ "yourapp/internal/domain/pasien"`.

Cross-domain: just add another domain's service type as a constructor parameter;
fx resolves it automatically.

## Auth

- `reqctx.Middleware()` + `reqctx.Auth(secret)` are installed globally by the library.
- Protect routes with `reqctx.RequireAuth()`.
- Issue tokens via `*reqctx.Signer` (`Access` / `Refresh`), injected by type.
- In a service, read the caller identity from the context:

```go
meta := reqctx.FromContext(ctx)
_ = meta.UserID    // from the JWT
_ = meta.RequestID
_ = meta.IP
```

## Outbound adapter

```go
func NewGateway(hc *http.Client, log *logging.Logger) Gateway {
    return &client{c: httpx.New(hc,
        httpx.BaseURL(os.Getenv("PAYMENT_BASE_URL")),
        httpx.Header("Authorization", "Bearer "+os.Getenv("PAYMENT_API_KEY")),
        httpx.WithLogger(log, "payment"), // emits a type=adaptor log per call
    )}
}

func (c *client) Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error) {
    var out ChargeResult
    return &out, c.c.PostJSON(ctx, "/charges", req, &out)
}
```

## Logging

Every JSON record carries a `type` and the `request_id` (from the context), so
all the work for one request lines up under the same id.

| type | source (automatic) |
|------|--------------------|
| `api` | gin middleware (method, path, status, latency, body) |
| `db` | GORM logger (sql, rows, latency) |
| `adaptor` | `httpx` (outbound) |
| `error` | middleware + `response.Error` (stack + `error_caller`) |
| `app` | manual `log.Info` / `log.Warn` / `log.Error` in services |

Manual logging in a service (inject `*logging.Logger`):

```go
s.log.Info(ctx, "pasien created", slog.Uint64("id", uint64(p.ID)))
s.log.Error(ctx, errs.Wrap(err))   // the stack points to the error's origin
```

Toggle: `LOG_ENABLED=false` (everything) or `LOG_DISABLE=db,adaptor` (per type).

### Shipping to Kibana (optional)

The app writes JSON to a file (`LOG_FILE`); Filebeat ships it to Elasticsearch →
Kibana Discover. A sample Filebeat config lives in the example project.

## License

MIT (or adjust as needed).
