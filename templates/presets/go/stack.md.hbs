# Stack Context: Go

## Tech Stack

- **Language**: Go 1.22+
- **Modules**: Go modules
- **Testing**: `go test` (standard library)
- **Linting**: golangci-lint
- **Formatting**: gofmt (enforced)

## Project Structure

```
cmd/
  server/
    main.go               # Entry point
internal/
  handler/                # HTTP handlers
    user.go
    user_test.go
  service/                # Business logic
    user.go
    user_test.go
  repository/             # Data access
    user.go
    user_test.go
  model/                  # Domain types
    user.go
  middleware/              # HTTP middleware
    auth.go
    logging.go
  config/                 # Configuration
    config.go
pkg/                      # Public packages (if any)
```

## Key Patterns

### Handler Structure
```go
type UserHandler struct {
    service *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
    return &UserHandler{service: svc}
}

func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
    var input model.CreateUserInput
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
        http.Error(w, "invalid request body", http.StatusBadRequest)
        return
    }

    user, err := h.service.Create(r.Context(), input)
    if err != nil {
        // Handle specific error types
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(user)
}
```

### Error Handling
- Return errors, don't panic
- Wrap errors with context: `fmt.Errorf("create user: %w", err)`
- Use sentinel errors for known conditions
- Check errors immediately after function calls

### Interfaces
- Define interfaces where they are USED, not where they are implemented
- Keep interfaces small (1-3 methods)
- Accept interfaces, return structs

### Testing
```go
func TestUserService_Create(t *testing.T) {
    tests := []struct {
        name    string
        input   model.CreateUserInput
        want    *model.User
        wantErr bool
    }{
        {
            name:  "valid input",
            input: model.CreateUserInput{Name: "Alice", Email: "alice@example.com"},
            want:  &model.User{Name: "Alice", Email: "alice@example.com"},
        },
        {
            name:    "empty name",
            input:   model.CreateUserInput{Name: "", Email: "alice@example.com"},
            wantErr: true,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

### Context Propagation
- Pass `context.Context` as first parameter to all functions that do I/O
- Use context for cancellation, timeouts, and request-scoped values
- Never store context in structs

### Dependency Injection
- Use constructor injection (NewXxx functions)
- Wire dependencies in `main.go`
- No DI frameworks — keep it simple

## Anti-Patterns

- Never use `interface{}` or `any` without strong reason
- Never ignore errors (`_ = someFunc()`)
- Never use `init()` for complex logic
- Never use package-level mutable state
- Never use `panic` for expected errors
- Never use `*` imports (dot imports)
- Never hardcode secrets — use environment variables

## Quality Gates

- `go vet ./...` — Static analysis
- `golangci-lint run` — Comprehensive linting
- `go test ./...` — Tests
- `gofmt -w .` — Formatting (enforced)
