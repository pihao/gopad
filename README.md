# Gopad

A self-hosted collaborative code editor in a single Go binary — a Go take on
[rustpad](https://github.com/ekzhang/rustpad), with document lifecycle
management, read-only share links, and an admin console on top.

- **Real-time collaboration** via operational transformation (OT), with
  remote cursors/selections, an online user list, and synchronized syntax
  highlighting (CodeMirror 6).
- **Zero-friction sharing**: open the site, get a random document URL, send
  it to anyone. No accounts. A separate read-only link can be shared with
  viewers who must not edit.
- **Persistence**: documents are snapshotted to SQLite and survive restarts.
  Each document expires after a rolling TTL since its last edit — 24h by
  default, adjustable per document from 1 minute up to 100 years.
- **Admin console** at `/admin` (HTTP Basic Auth): paginated listing of all
  documents with live connection counts, and deletion that kicks connected
  editors.

## Quick start

The frontend build output is vendored in `internal/server/dist`, so building
the server needs only a Go toolchain — no node/npm:

```bash
make build   # go build with the vendored frontend embedded
./gopad
```

Open http://localhost:3030 — a new document id is generated for you.

### Docker

```bash
docker build -t gopad .
docker run -p 3030:3030 -v gopad-data:/data \
  -e ADMIN_USER=admin -e ADMIN_PASSWORD=change-me gopad
```

## Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3030` | HTTP listen port |
| `SQLITE_PATH` | `gopad.db` | SQLite file; `off` disables persistence |
| `ADMIN_USER` / `ADMIN_PASSWORD` | _(empty)_ | Admin console credentials; console is disabled unless both are set |
| `DEFAULT_TTL` | `24h` | TTL for new documents (Go duration) |
| `MAX_DOC_SIZE` | `1048576` | Per-document size limit in bytes |
| `BASE_PATH` | _(empty)_ | URL prefix to mount the app under, e.g. `/gopad` (see below) |

## Behind a reverse proxy

At the root of a domain (or a subdomain) nothing special is needed — just
proxy everything through, with WebSocket upgrades enabled.

To serve the app under a path instead, set `BASE_PATH` to that prefix and
pass the requests through **unmodified** — the app expects to see the prefix
and rewrites its own asset, API, WebSocket and share URLs accordingly. It
serves nothing outside the prefix.

```bash
BASE_PATH=/gopad ./gopad
```

```nginx
# In the http {} block, once:
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name your-domain.com;
    # ssl_certificate ...;

    location /gopad {
        proxy_pass http://127.0.0.1:3030;   # no trailing slash: keep the prefix
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;        # required: the WebSocket
                                            # handshake checks Origin
                                            # against the Host header
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_read_timeout 7d;              # sockets are idle between edits
        proxy_send_timeout 7d;
        proxy_buffering off;
    }
}
```

`BASE_PATH` accepts `gopad`, `/gopad` or `/gopad/` alike, and nested prefixes
(`/tools/gopad`) work too. `https://your-domain.com/gopad` redirects to
`/gopad/`.

## HTTP API

| Route | Description |
|---|---|
| `GET /#<id>` | Editor page (id lives in the URL hash) |
| `GET /#view/<roId>` | Read-only page |
| `WS /api/socket/{id}` | Writable collaboration socket (creates the doc) |
| `WS /api/readonly/{roId}` | Read-only socket (404 for unknown ids) |
| `GET /api/text/{id}` | Raw document text |
| `GET /api/readonlyid/{id}` | Read-only share id for a document |
| `GET /admin` | Admin console (Basic Auth) |
| `GET /api/admin/documents?page=&size=` | Paginated document listing (Basic Auth) |
| `DELETE /api/admin/documents/{id}` | Delete a document (Basic Auth) |

All routes sit under `BASE_PATH` when it is set.

## Development

```bash
make test          # Go tests + frontend vitest
cd frontend && npm run dev   # vite dev server proxying /api to :3030
```

Only frontend work needs node/npm. After changing anything under
`frontend/`, regenerate and commit the vendored artifacts:

```bash
make frontend      # npm install + vite build → internal/server/dist
```

(Without a local node, run it in a container:
`docker run --rm -v $PWD:/repo -w /repo/frontend node:24-alpine sh -c "npm install && npm run build"`.)

Layout: `internal/ot` (OT engine), `internal/document` (session state),
`internal/server` (HTTP/WS/admin), `internal/store` (SQLite),
`frontend/` (CodeMirror 6 + TypeScript). Design doc:
[docs/requirements-and-design.md](docs/requirements-and-design.md).
