# bluesky-feeds

A BlueSky Feed Generator built in Go. Consumes posts from the [Jetstream](https://github.com/bluesky-social/jetstream) firehose, filters them by configurable keywords, and serves custom feeds via the [AT Protocol Feed Generator](https://docs.bsky.app/docs/advanced-guides/feed-generators) spec.

## Architecture

This project follows **hexagonal (ports and adapters)** architecture to keep business logic independent of infrastructure concerns:

- **Domain layer** (`internal/domain/`) — Core business logic and port interfaces. No external dependencies. Defines `FeedService` (keyword matching, post persistence, feed skeletons) and repository interfaces (`PostRepository`, `CursorRepository`).

- **Adapters** (`internal/*/`) — Infrastructure implementations that plug into domain ports:
  - `postgres` — Postgres-backed repository implementing `PostRepository` and `CursorRepository`
  - `firehose` — Jetstream WebSocket subscriber that feeds posts to `FeedService`
  - `httpserver` — HTTP server exposing XRPC endpoints and DID document
  - `bluesky` — BlueSky API client for publishing feed generator records
  - `config` — Environment-based configuration

- **Composition root** (`cmd/server/main.go`) — Wires adapters together and injects them into the domain service

This design makes each component independently testable and allows swapping implementations (e.g., swap Postgres for DynamoDB) without touching business logic.

## Prerequisites

- Go 1.21+
- Docker with Compose plugin (for local Postgres)
- golang-migrate (`brew install golang-migrate`)
- A BlueSky account with an [App Password](https://bsky.app/settings/app-passwords) (for publishing feeds)

## Quick Start

1. **Configure environment**

   Copy `.env.local` to `.env`.
   Edit `.env` and set `FEEDGEN_PUBLISHER_DID` to your BlueSky DID.

2. **Start Postgres and run migrations**

   ```bash
   make setup
   ```

3. **Run the server**

   ```bash
   make run-env
   ```

   The server connects to Jetstream, indexes matching posts, and serves feed endpoints on port 3000.

4. **Publish your feed to BlueSky**

   ```bash
   make publish ARGS='--rkey my-feed --name "My Feed" --description "Posts about Go and AI"'
   ```

## Useful Commands

```bash
make help       # Show all available targets
make setup      # Start Postgres + run migrations
make run-env    # Run server with .env file loaded
```

## Local Testing

Once the server is running locally, test the endpoints:

```bash
# Health check
curl http://localhost:3000/health

# DID document (for did:web resolution)
curl http://localhost:3000/.well-known/did.json

# List available feeds
curl http://localhost:3000/xrpc/app.bsky.feed.describeFeedGenerator

# Get feed skeleton (replace DID and rkey with your values)
curl "http://localhost:3000/xrpc/app.bsky.feed.getFeedSkeleton?feed=at://did:plc:YOUR_DID/app.bsky.feed.generator/YOUR_RKEY&limit=20"

# Get feed skeleton with cursor-based pagination
curl "http://localhost:3000/xrpc/app.bsky.feed.getFeedSkeleton?feed=at://did:plc:YOUR_DID/app.bsky.feed.generator/YOUR_RKEY&limit=20&cursor=CURSOR_STRING"
```

## How It Works

1. **Firehose ingestion** — The server connects to Jetstream via WebSocket and subscribes to `app.bsky.feed.post` events. A cursor is saved periodically to resume from the last position after restarts.

2. **Filtering** — Incoming posts are matched against feed algorithms using keyword regex with word boundaries and optional language filters.

3. **Indexing** — Matching posts are stored in Postgres with `uri`, `cid`, and `indexed_at`. Deleted posts are removed. A background job enforces TTL (7 days) and row cap (500) limits.

4. **Serving** — When BlueSky's AppView requests a feed skeleton, the server queries Postgres for posts ordered by `indexed_at` and returns their AT-URIs. The AppView hydrates these into full post views.

5. **DID resolution** — The `/.well-known/did.json` endpoint returns a DID document so BlueSky can discover this feed generator's service endpoint.

## Publishing Feeds

Before publishing your feed generator you'll need to determine your Service DID. If your service is hosted 
`https://my-cool-server.hosting.com`, then your Service DID will be `did:web:my-cool-server.hosting.com`.

To publish a local feed, forward port 3000 to an external domain using a service like ngrok (`brew install ngrok`).
Then you can use the ngrok hostname for your Service DID.

Use the `cmd/publish` tool to register/update feed generator records in your BlueSky account:

```bash
# Publish a feed
make publish ARGS='--rkey my-feed --name "My Feed" --description "A custom feed" --service-did <your-service-did>'

# Unpublish a feed
make unpublish ARGS='--rkey my-feed'

# Run directly with flags (credentials via env vars or flags)
go run ./cmd/publish \
  --handle user.bsky.social \
  --password your-app-password \
  --service-did did:web:feed.example.com \
  --rkey my-feed \
  --name "My Feed" \
  --description "Posts about AI"
```

This will print out the Feed URI, which is a combination of your Account DID (otherwise known as the Publisher DID) and the record key. Configure the `FEEDGEN_PUBLISHER_DID` in `.env` to use your Account DID.

Once the feed record is published and your local server is configured, you can run `make run-env` to start the server. At this point you can verify the server is running via your browser or curl. If everything looks good, try searching for your feed on BlueSky!

