# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go web application for tracking family "star" rewards. Uses standard library `net/http`, SQLite via `modernc.org/sqlite`, and Go HTML templates. Single-binary deployment with embedded templates and static assets.

## Build & Run Commands

```bash
make build          # Compile to ./star-app
make build-arm64    # Cross-compile for ARM64 Linux
make run            # Build and run
make clean          # Remove binaries and stars.db
```

Run flags: `-port 8080` (HTTP port), `-db stars.db` (SQLite path).

No test suite exists currently. No linter configuration.

## Architecture

**Single-package Go app with five source files:**

- `main.go` — Entry point, route registration, static file serving
- `models.go` — Data structs: User, Star, Reason, APIKey, SessionData
- `db.go` — SQLite schema init, all database queries, seed data (default users: dad, mom, kid1, kid2 with password "changeme")
- `handlers.go` — HTTP handlers for both web UI and REST API
- `middleware.go` — Three auth middlewares: `authWeb` (session cookie), `authAdmin` (admin role check), `authAPI` (X-API-Key header, SHA256 hashed)

**Routes:**

- Web UI: `/` (dashboard), `/login`, `/logout`, `/admin`, `/admin/star`, `/admin/apikey`
- REST API: `/api/stars`, `/api/users`, `/api/reasons` (authenticated via X-API-Key header)

**Templates** (`templates/`): layout.html, login.html, dashboard.html, admin.html
**Static assets** (`static/`): style.css, app.js
