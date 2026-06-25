# Deployment Guide

## Prerequisites

### Local Development
- **Go**: 1.26 or later
- **PostgreSQL**: 12 or later
- **Redis**: 6 or later
- **golang-migrate**: Latest version (for running migrations manually)
- **Docker** (optional): For containerized local setup
- **Docker Compose** (optional): For multi-service setup

### Production
- **Go**: 1.26 (binary compiled locally or in CI)
- **PostgreSQL**: 12+ with backups enabled
- **Redis**: 6+ with persistence enabled
- **Reverse proxy**: nginx or similar (for TLS, load balancing)
- **Secrets manager**: Vault, AWS Secrets Manager, or environment variables

---

## Local Development Setup

### 1. Clone & Configure

```bash
# Clone the repository
git clone https://github.com/TranTheTuan/go-shortener.git
cd go-shortener

# Copy environment template
cp .env.example .env

# Edit .env with your local values (or use defaults)
# Defaults: PostgreSQL on localhost:5432, Redis on localhost:6379
```

### 2. Start Services (Docker Compose)

```bash
# Start PostgreSQL + Redis
docker-compose up -d

# Verify services are running
docker-compose ps

# Check PostgreSQL is ready
docker-compose exec postgres psql -U postgres -c "SELECT version();"

# Check Redis is ready
docker-compose exec redis redis-cli ping
```

Or manually start services:
```bash
# PostgreSQL (example: on macOS with Homebrew)
brew services start postgresql@15

# Redis
brew services start redis

# Or in Docker:
docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:15
docker run --rm -d -p 6379:6379 redis:7
```

### 3. Install Dependencies

```bash
go mod download
```

### 4. Run Migrations

```bash
# Apply all pending migrations
make migrate-up

# Verify migration
make migrate-version

# Create a new migration (for development)
make migrate-create NAME=my_migration
```

### 5. Run the Server

```bash
# Development (hot reload recommended; install cosmtrek/air)
make run

# Production build
make build
./build/main

# Run tests
make test
```

The server listens on `http://localhost:8080` by default.

### 6. Test the API

```bash
# Health check
curl http://localhost:8080/healthz

# Create short link (requires API key)
curl -X POST http://localhost:8080/api/links \
  -H 'X-API-Key: dev-key-1' \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'

# Redirect
curl -i http://localhost:8080/<short-code>

# Get stats
curl http://localhost:8080/api/links/<short-code>/stats \
  -H 'X-API-Key: dev-key-1'

# Register user
curl -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{
    "username": "alice",
    "email": "alice@example.com",
    "password": "securepassword123",
    "name": "Alice"
  }'

# Login
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{
    "email": "alice@example.com",
    "password": "securepassword123"
  }'
```

---

## Docker Deployment

### Single-Container Build

```bash
# Build Docker image
docker build -t go-shortener:latest .

# Run container
docker run --rm -p 8080:8080 \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=5432 \
  -e REDIS_HOST=host.docker.internal \
  -e REDIS_PORT=6379 \
  go-shortener:latest
```

### Docker Compose (Multi-Service)

```bash
# Start all services (PostgreSQL, Redis, app)
docker-compose up

# Stop services
docker-compose down

# View logs
docker-compose logs -f app

# Stop database & cache, but keep app data
docker-compose down -v
```

Example `docker-compose.yml`:
```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: app
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data

  app:
    build: .
    environment:
      DB_HOST: postgres
      DB_USER: postgres
      DB_PASSWORD: postgres
      DB_NAME: app
      REDIS_HOST: redis
      REDIS_PORT: 6379
      SHORTENER_API_KEYS: dev-key-1,dev-key-2
      AUTH_JWT_SECRET: dev-insecure-change-me
    ports:
      - "8080:8080"
    depends_on:
      - postgres
      - redis
    command: sh -c "make migrate-up && make run"

volumes:
  postgres_data:
  redis_data:
```

---

## Production Deployment

### Environment Configuration

Create a `.env` file (or use secrets manager):

```bash
# Deployment
ENV=production

# HTTP Server
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
SERVER_READ_TIMEOUT=5s
SERVER_WRITE_TIMEOUT=10s
SERVER_IDLE_TIMEOUT=120s
SERVER_SHUTDOWN_TIMEOUT=10s
SERVER_PPROF_ADDR=              # Empty in production (disable pprof)

# PostgreSQL (production database)
DB_HOST=prod-postgres.internal
DB_PORT=5432
DB_USER=app_user
DB_PASSWORD=<STRONG_PASSWORD_FROM_SECRETS>
DB_NAME=url_shortener
DB_SSLMODE=require              # TLS to database
DB_TIMEZONE=UTC
DB_MAX_OPEN_CONNS=50            # Increase for production load
DB_MAX_IDLE_CONNS=25
DB_CONN_MAX_LIFETIME=5m

# Redis (production cache)
REDIS_HOST=prod-redis.internal
REDIS_PORT=6379
REDIS_PASSWORD=<STRONG_PASSWORD_FROM_SECRETS>
REDIS_DB=0
REDIS_POOL_SIZE=50

# URL Shortener
SHORTENER_BASE_URL=https://sho.rt
SHORTENER_API_KEYS=<PROD_KEY_1>,<PROD_KEY_2>,<PROD_KEY_3>
SHORTENER_CODE_LENGTH=7
SHORTENER_CACHE_TTL=24h

# Authentication
AUTH_JWT_SECRET=<STRONG_RANDOM_SECRET_256BIT+>
AUTH_ACCESS_TTL=15m
AUTH_REFRESH_TTL=168h
AUTH_BCRYPT_COST=12
```

### Binary Compilation

```bash
# Build optimized binary
make build

# Binary location
ls -lh ./build/main

# Run with configuration
export $(cat .env | grep -v '^#')
./build/main
```

Or compile with custom flags:
```bash
go build -ldflags="-s -w" -o build/main ./cmd/server/main.go
```

### Systemd Service (Linux)

Create `/etc/systemd/system/go-shortener.service`:

```ini
[Unit]
Description=Go URL Shortener API
After=network.target postgresql.service redis-server.service

[Service]
Type=simple
User=shortener
WorkingDirectory=/opt/go-shortener
EnvironmentFile=/opt/go-shortener/.env
ExecStart=/opt/go-shortener/build/main
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Commands:
```bash
# Enable service
sudo systemctl enable go-shortener

# Start service
sudo systemctl start go-shortener

# View logs
sudo journalctl -u go-shortener -f

# Stop service
sudo systemctl stop go-shortener
```

### Nginx Reverse Proxy

```nginx
upstream go_shortener {
    server localhost:8080;
    # Add multiple backends for load balancing
    # server localhost:8081;
    # server localhost:8082;
}

server {
    listen 443 ssl http2;
    server_name sho.rt;

    ssl_certificate /etc/letsencrypt/live/sho.rt/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sho.rt/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    # Redirect HTTP to HTTPS
    error_page 497 =301 https://$server_name$request_uri;

    location / {
        proxy_pass http://go_shortener;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Timeouts
        proxy_connect_timeout 5s;
        proxy_send_timeout 10s;
        proxy_read_timeout 10s;
    }

    # Swagger UI
    location /swagger/ {
        proxy_pass http://go_shortener/swagger/;
    }

    # Health check endpoint
    location /healthz {
        proxy_pass http://go_shortener/healthz;
        access_log off;
    }
}

# HTTP redirect
server {
    listen 80;
    server_name sho.rt;
    return 301 https://$server_name$request_uri;
}
```

### Database Setup (PostgreSQL)

```bash
# Connect to PostgreSQL
psql -h prod-postgres.internal -U postgres

# Create database & user
CREATE DATABASE url_shortener;
CREATE USER app_user WITH PASSWORD '<STRONG_PASSWORD>';
GRANT ALL PRIVILEGES ON DATABASE url_shortener TO app_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO app_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO app_user;

# Verify
\l                              # List databases
\du                             # List users
\c url_shortener                # Connect to database
\dt                             # List tables (empty initially)
```

### Redis Setup

```bash
# Connect to Redis
redis-cli -h prod-redis.internal -a <PASSWORD>

# Set password (if not already done)
CONFIG SET requirepass <STRONG_PASSWORD>

# Save configuration
CONFIG REWRITE

# Verify
PING                            # Should return PONG
INFO                            # Server info
```

### Migration Management (Production)

```bash
# Before deployment, test migrations on staging
# Set environment to staging database
export DB_HOST=staging-postgres.internal
make migrate-version

# Run migrations (one-time, before starting app)
make migrate-up

# Verify
psql -h prod-postgres.internal -U app_user -d url_shortener \
  -c "SELECT * FROM users LIMIT 1;"
```

### Monitoring & Logging

#### Application Logs
```bash
# View JSON logs (production)
docker logs <container-id> | jq .

# Filter errors
docker logs <container-id> | jq 'select(.level == "ERROR")'

# Tail real-time
docker logs -f <container-id>
```

#### Health Check
```bash
# Setup health check in load balancer
curl http://localhost:8080/healthz

# Response
{"data": "ok"}

# Also check status code (should be 200)
curl -i http://localhost:8080/healthz
```

#### Pprof (Development/Staging Only)
```bash
# CPU profile (30s)
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Heap profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine dump
curl http://localhost:6060/debug/pprof/goroutine?debug=1 > goroutines.txt
```

---

## Scaling Strategies

### Horizontal Scaling (Multiple Instances)

1. **Build binary**: `make build`
2. **Deploy to N instances** (e.g., with Kubernetes, Docker Swarm)
3. **Configure shared database**: All instances connect to same PostgreSQL
4. **Configure shared cache**: All instances connect to same Redis
5. **Load balancer**: nginx, HAProxy, or cloud LB distributes traffic

```
[Load Balancer] (nginx, ELB, etc.)
    ├─ [App Instance 1] :8080
    ├─ [App Instance 2] :8080
    ├─ [App Instance 3] :8080
    └─ ...
         ├─ PostgreSQL (shared)
         └─ Redis (shared)
```

### Database Scaling

**PostgreSQL**:
- **Read replicas**: Set up with replication for read scaling
- **Connection pooling**: Use pgBouncer in front of PostgreSQL
- **Backup**: Enable WAL archiving + daily backups

**Redis**:
- **Persistence**: Enable RDB snapshots + AOF
- **Replication**: Master-replica setup for failover
- **Sentinel**: Automatic failover on master failure

### Performance Tuning

#### PostgreSQL
```sql
-- Connection pool tuning (pgBouncer config)
max_client_conn = 1000
default_pool_size = 25
reserve_pool_size = 5

-- Indexes (from migrations)
CREATE INDEX idx_links_short_code ON links(short_code);
CREATE INDEX idx_clicks_link_id ON clicks(link_id);
```

#### Redis
```bash
# Increase max clients
CONFIG SET maxclients 10000

# Monitor memory usage
INFO memory

# Eviction policy (if memory constrained)
CONFIG SET maxmemory-policy allkeys-lru
```

#### Go Server
```bash
# Environment tuning
GOMAXPROCS=8                    # Match CPU cores
DB_MAX_OPEN_CONNS=50            # For high concurrency
REDIS_POOL_SIZE=50
```

---

## Disaster Recovery

### Backup Strategy

```bash
# PostgreSQL daily backup
pg_dump -h prod-postgres.internal -U app_user url_shortener \
  | gzip > /backups/db-$(date +%Y%m%d).sql.gz

# Store in S3 / object storage
aws s3 cp /backups/db-$(date +%Y%m%d).sql.gz \
  s3://backups-bucket/go-shortener/

# Redis backup (enable RDB snapshots)
# /etc/redis/redis.conf:
# save 900 1                    # Save if 1 key changes in 900s
# save 300 10                   # Save if 10 keys change in 300s
```

### Recovery Procedure

```bash
# Restore PostgreSQL from backup
gunzip -c /backups/db-20260601.sql.gz | \
  psql -h prod-postgres.internal -U postgres url_shortener

# Restart application
systemctl restart go-shortener

# Verify data
curl http://localhost:8080/healthz
```

---

## CI/CD Integration

### GitHub Actions Example

```yaml
name: CI/CD

on:
  push:
    branches: [master, main, feat/*]
  pull_request:
    branches: [master, main]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_PASSWORD: postgres
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
      redis:
        image: redis:7
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: 1.26

      - run: go mod download
      - run: make test
      - run: make build

  deploy:
    needs: test
    if: github.ref == 'refs/heads/master'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: |
          docker build -t go-shortener:${{ github.sha }} .
          docker push registry.example.com/go-shortener:${{ github.sha }}
          # Trigger production deployment (e.g., ArgoCD, Terraform)
```

---

## Troubleshooting

### Server Won't Start

```bash
# Check environment variables
echo $DB_HOST $DB_PORT $REDIS_HOST

# Check database connection
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "SELECT 1;"

# Check Redis connection
redis-cli -h $REDIS_HOST PING

# View logs
journalctl -u go-shortener -n 50
```

### High Memory Usage

```bash
# Check Go memory profile
curl http://localhost:6060/debug/pprof/heap > heap.out
go tool pprof -http=:8888 heap.out

# Check database connections
psql -h $DB_HOST -U $DB_USER -d $DB_NAME \
  -c "SELECT count(*) FROM pg_stat_activity;"

# Check Redis memory
redis-cli INFO memory
```

### Database Locked / Slow Queries

```bash
# PostgreSQL: view active queries
psql -c "SELECT * FROM pg_stat_statements ORDER BY total_time DESC;"

# PostgreSQL: kill long-running query
SELECT pg_terminate_backend(pid) FROM pg_stat_activity 
  WHERE duration > interval '5 minutes';
```

---

**Last Updated**: 2026-06-22  
**Version**: 1.0  
**Status**: Production-ready for single-instance and scaled deployments
