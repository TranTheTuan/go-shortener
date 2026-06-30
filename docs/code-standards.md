# Code Standards & Guidelines

## Go Version & Idioms

- **Minimum Go version**: 1.26
- **Module**: `github.com/TranTheTuan/go-shortener`
- **Style**: Follow [Effective Go](https://golang.org/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- **Formatting**: Use `gofmt` (enforced by IDE)
- **Linting**: Use `golangci-lint` or similar (non-blocking; prioritize functionality over strict enforcement)

## Package Organization

### Directory Structure
```
go-shortener/
├── cmd/server/              # Binary entrypoint(s) only (main.go)
├── internal/                # Private application code (not importable)
│   ├── handler/             # HTTP transport layer (request/response)
│   ├── service/             # Business logic & validation
│   ├── repository/          # Data access abstractions (interfaces + implementations)
│   ├── middleware/          # HTTP middleware (auth, logging, etc.)
│   └── router/              # Route registration & wiring
├── pkg/                     # Public, importable packages
│   ├── apperror/            # Structured error type
│   ├── response/            # HTTP response helpers
│   ├── database/            # DB connection factories
│   ├── keycloak/            # Keycloak OIDC token validation
│   └── shortcode/           # Random code generation
├── configs/                 # Configuration loading
├── migrations/              # SQL migrations (*.up.sql / *.down.sql)
└── docs/                    # Documentation (Markdown, not code)
```

### Package Naming
- Use **lowercase, single-word names** when possible (`handler`, `service`, `repository`)
- Avoid underscores in package names; use multi-word names sparingly
- Packages in `internal/` are private to this module
- Packages in `pkg/` are reusable and importable by other modules

### File Naming
- Use **snake_case** for Go files (e.g., `user_handler.go`, `link_service.go`)
- Group related functionality in the same file or use suffixes: `*_handler.go`, `*_service.go`, `*_repository.go`, `*_test.go`
- Keep files under **200 LOC** for readability; split larger files

## Error Handling

### The `apperror.Error` Type

All application errors use the structured `apperror.Error` type:

```go
type Error struct {
    Status  int    // HTTP status code
    Code    string // Machine-readable error code
    Message string // Human-readable, client-safe message
    Err     error  // Optional wrapped cause (internal only)
}
```

### Constructor Functions

Use helpers from `pkg/apperror` for common cases:

```go
apperror.BadRequest("missing email")           // HTTP 400
apperror.NotFound("user not found")            // HTTP 404
apperror.Conflict("username already taken")    // HTTP 409
apperror.Gone("link expired")                  // HTTP 410
apperror.Internal(databaseErr)                 // HTTP 500 (wraps internal cause)
apperror.New(status, code, message)            // Custom error
```

### Error Propagation

- **Services** return `*apperror.Error` (domain errors) or `error` (internal errors)
- **Handlers** convert errors to responses via `response.Error(c, err)`
- **Never expose internal errors to clients**: wrap DB errors with `apperror.Internal(err)`
- **Generic auth failures**: Use single error message for login/refresh to prevent user enumeration

Example:
```go
func (s *userService) GetByID(ctx context.Context, id int64) (*User, error) {
    user, err := s.repo.GetByID(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, apperror.NotFound("user not found")
        }
        return nil, apperror.Internal(err)
    }
    return user, nil
}
```

## Response Envelope

All HTTP responses use a uniform envelope via `pkg/response`:

```go
// Success: wrap data in {"data": ...}
response.Success(c, http.StatusOK, user)
response.Success(c, http.StatusCreated, newLink)

// Error: wrap error in {"error": {"code": "...", "message": "..."}}
response.Error(c, apperror.NotFound("not found"))
```

### Response Format
```json
// Success
{"data": {"id": 1, "name": "Alice", ...}}

// Error
{"error": {"code": "NOT_FOUND", "message": "user not found"}}
```

## Interface-Based Design

### Repository Layer
All data access is abstracted behind interfaces in `internal/repository/`:

```go
// Repository interface (in same package as implementation)
type UserRepository interface {
    GetByID(ctx context.Context, id int64) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
    Create(ctx context.Context, user *User) error
    Update(ctx context.Context, user *User) error
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context, limit, offset int) ([]*User, error)
}

// Implementation using GORM
type gormUserRepository struct {
    db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
    return &gormUserRepository{db: db}
}

func (r *gormUserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
    // GORM query logic here
}
```

### Service Layer
Services depend on repository interfaces (not concrete implementations):

```go
type UserService interface {
    GetByID(ctx context.Context, id int64) (*User, error)
    // ... other methods
}

type userService struct {
    repo UserRepository  // Interface, not concrete type
}

func NewUserService(repo UserRepository) UserService {
    return &userService{repo: repo}
}
```

### Benefits
- **Testability**: Mock repositories for unit tests
- **Flexibility**: Swap implementations (PostgreSQL → MySQL) without changing services
- **Clarity**: Explicit dependencies visible in constructor

## Middleware Patterns

### Authentication Middleware

**Keycloak middleware** (`middleware/keycloak.go`):
- Validates Bearer token via go-oidc (Keycloak-issued only)
- Verifies token signature (RS256 via JWKS), expiry, issuer, audience (optional)
- JIT-provisions Keycloak identity to local users table
- Extracts local user ID into context (downstream handlers see only local id)
- Applied to routes requiring authentication (`POST /api/links`, `GET /auth/me`, etc.)

Example usage:
```go
// In router setup
api := e.Group("/api")
api.Use(appmw.Keycloak(keycloakVerifier, userService))
api.POST("/links", h.Link.Create)

// In handler
func (h *LinkHandler) Create(c echo.Context) error {
    userID := appmw.UserIDFrom(c)  // Extract local id (JIT-mapped from Keycloak sub)
    // ... create link owned by userID
}
```

**Token Verifier Interface** (`pkg/keycloak/verifier.go`):
```go
type Identity struct {
    Sub      string // Keycloak subject (UUID)
    Email    string // User email
    Username string // preferred_username claim
}

type TokenVerifier interface {
    Verify(ctx context.Context, rawToken string) (*Identity, error)
}
```

## Testing Patterns

### Mock Repositories
Create mocks by hand (no code generation required) in `*_test.go` files:

```go
// In handler test file
type mockUserService struct {
    getByIDFunc func(ctx context.Context, id int64) (*User, error)
}

func (m *mockUserService) GetByID(ctx context.Context, id int64) (*User, error) {
    if m.getByIDFunc != nil {
        return m.getByIDFunc(ctx, id)
    }
    return nil, nil
}

// In test
func TestGetUser(t *testing.T) {
    mock := &mockUserService{
        getByIDFunc: func(ctx context.Context, id int64) (*User, error) {
            return &User{ID: 1, Name: "Alice"}, nil
        },
    }
    
    h := handler.NewUserHandler(mock)
    // ... test h.Get()
}
```

### Table-Driven Tests
Use subtests for multiple scenarios:

```go
func TestValidateEmail(t *testing.T) {
    tests := []struct {
        name      string
        email     string
        wantError bool
    }{
        {"valid", "alice@example.com", false},
        {"empty", "", true},
        {"no at", "alice.example.com", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateEmail(tt.email)
            if (err != nil) != tt.wantError {
                t.Errorf("validateEmail(%q) error = %v, want %v", tt.email, err, tt.wantError)
            }
        })
    }
}
```

## Naming Conventions

### Variables
- Use **camelCase** for local variables and function parameters
- Use **snake_case for struct field JSON tags** (`json:"user_id"`)
- Use **PascalCase for exported functions and types** (`GetByID`, `CreateUser`)
- Use **lowercase for unexported types and functions** (`userRepository`)

### Error Codes
Use **UPPER_SNAKE_CASE** for error codes:
- `BAD_REQUEST` — Client error (HTTP 400)
- `UNAUTHORIZED` — Auth failure (HTTP 401)
- `NOT_FOUND` — Missing resource (HTTP 404)
- `CONFLICT` — Duplicate / state conflict (HTTP 409)
- `GONE` — Expired resource (HTTP 410)
- `INTERNAL` — Unexpected server error (HTTP 500)

### Database Schema
- Use **snake_case** for table and column names
- Use **singular table names** (`user`, `link`, `click`, not `users`, `links`)
- Use **suffixes for relationships**: `user_id` (foreign key), `created_at` (timestamp)
- Use **NOT NULL where possible**; nullable fields are explicit

### JSON API
- Use **snake_case** for JSON field names (`short_code`, `user_id`, `created_at`)
- Omit zero/null fields with `omitempty` tag (except required fields)
- Document field types in struct tags and comments

Example:
```go
type User struct {
    ID        int64     `json:"id"`                    // User ID
    Username  string    `json:"username"`              // Unique username
    Email     string    `json:"email"`                 // Email address
    Name      *string   `json:"name,omitempty"`        // Optional full name
    CreatedAt time.Time `json:"created_at"`            // Account creation time
}
```

## Configuration Management

### Environment Variables
- Defined in `configs/config.go` with struct tags (`env:"VAR_NAME"`)
- All variables have sensible defaults (fail-safe)
- Keycloak variables (`KEYCLOAK_ISSUER`, `KEYCLOAK_JWKS_URL`) required outside development env
- Namespace variables with prefixes: `SERVER_*`, `DB_*`, `SHORTENER_*`, `KEYCLOAK_*`, `QUOTA_*`

### Loading Config
```go
cfg, err := configs.Load()  // Reads environment + applies defaults
if err != nil {
    return err  // Config validation failed
}

// Access typed fields
serverAddr := cfg.Server.Addr()
dbDSN := cfg.Database.DSN()
```

## Comments & Documentation

### File Header Comments
Every `.go` file starts with a package comment:

```go
// Package router wires HTTP routes to handlers and configures global
// middleware. It owns the Echo instance and keeps routing concerns out of the
// handler and main packages.
package router
```

### Function Comments
Exported functions have doc comments:

```go
// Create handles POST /api/links and creates a short URL.
// The X-API-Key header must match one of SHORTENER_API_KEYS.
func (h *LinkHandler) Create(c echo.Context) error {
    // ...
}
```

### Inline Comments
Use sparingly; explain *why*, not *what*:

```go
// Fail closed: empty key set rejects all requests
if len(cfg.Shortener.APIKeys) == 0 {
    return apperror.Conflict("no API keys configured")
}

// SHA256 hash, not raw token; prevents accidental token exposure in logs
token := generateToken()
hash := sha256.Sum256([]byte(token))
```

## Concurrency & Performance

### Goroutines
- Use goroutines for async operations (analytics recording)
- Accept acceptable loss on crash (no guaranteed delivery for non-critical operations)
- Example: `go a.recordClick(userID, linkID, referrer)` (fire-and-forget)

### Connection Pooling
Configure via environment:
- `DB_MAX_OPEN_CONNS`: 25 (adjust for load)
- `DB_MAX_IDLE_CONNS`: 25
- `DB_CONN_MAX_LIFETIME`: 5m

### Timeouts
- Server read timeout: 5s
- Server write timeout: 10s
- Database queries: no explicit timeout (relies on server timeout)
- Graceful shutdown: 10s max

### Token Handling (Keycloak-Delegated)
- Access tokens: RS256-signed by Keycloak (validate signature + expiry + issuer + audience)
- JIT provisioning: Keycloak `sub`/`email`/`preferred_username` → local user (get-or-create)
- Token revocation: Managed by Keycloak (app doesn't track revocation separately)
- JWKS fetching: In-cluster endpoint (lazy initialization, no startup block)

### Input Validation (User Claims)
- Email: From Keycloak token claim (assume pre-validated by Keycloak)
- Username: From `preferred_username` claim
- Keycloak Sub: Unique identifier (UUID, stored in `users.keycloak_sub`)

### Audience Validation
- Optional (gated by `KEYCLOAK_CLIENT_ID` env var)
- If set, Keycloak must have audience mapper to include backend client in `aud` claim
- If not set, skip audience check (useful for testing or single-audience realms)

## Logging

### Structured Logging
Use `log/slog` (Go 1.21+ standard library):

```go
slog.Info("user created", "user_id", user.ID, "email", user.Email)
slog.Error("database error", "error", err, "query", "SELECT...")
slog.Debug("cache hit", "code", code)
```

### What to Log
- Request entry/exit (automatic via middleware)
- Important state changes (user created, token issued)
- Errors (with wrapped cause for debugging)
- Performance milestones (cache hit/miss)

### What NOT to Log
- Raw passwords, tokens, API keys
- Full request/response bodies (PII)
- Internal stack traces (safe for logs, but not for clients)

---

**Last Updated**: 2026-06-30  
**Enforced**: Commit time (code reviews check standards)  
**Flexibility**: Prioritize working code over lint perfection  
**Auth Model**: Keycloak OIDC resource server (no self-issued tokens)
