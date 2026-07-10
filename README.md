# 🪐 Backend Engineer Challenge: Rockets 🚀

## Introduction 👋
Thank you for taking Lunar's code challenge for backend engineers! 

In the ZIP-file you have received, you will find a `README.md` (dah! of course) and folders 
containing executables for various operating systems and architectures.

> **Important:** If you cannot find an executable that works for you please reach out to us as soon as possible, 
> so we can get you one that works.

We hope you will enjoy this challenge - good luck.

## The Challenge 🧑‍💻
In this challenge you are going to build a service (or multiple) which consumes messages 
from a number of entities – i.e. a set of _rockets_ – and make the state of these 
available through a REST API. We imagine this API to be used by something like a dashboard.

As a minimum we expect endpoints which can:
1. Return the current state of a given rocket (type, speed, mission, etc.)
1. Return a list of all the rockets in the system; preferably with some kind of sorting.

The service should also expose an endpoint where the test program can post the messages to (see this [section](#running-the-test-program))

We are writing all our services in [Go](https://go.dev/) but there are no constrains on the programming language that you choose for solving the challenge. 
We prefer that you implement a great solution in a language that you feel comfortable in rather than trying to write in Go and implement a mediocre solution.

### The messages ✉️
Each rocket will be dispatching various messages (encoded as JSON) about its state changes through individual radio _channels_.
The channel is unique for each rocket and can therefore be treated as the ID of the rocket.

Apart from the channel each message also contains a _message number_ which expresses the order of the message within a channel, 
a _message time_ indicating when the message was sent and a _message type_ describing the event that occurred.

**Important:** Messages will arrive **out of order** and there is an **at-least-once guarantee** on messages 
meaning that you might receive the same message more than once.

Here is an example of a `RocketLaunch` message:

```json
{
    "metadata": {
        "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
        "messageNumber": 1,    
        "messageTime": "2022-02-02T19:39:05.86337+01:00",                                          
        "messageType": "RocketLaunched"                             
    },
    "message": {                                                    
        "type": "Falcon-9",
        "launchSpeed": 500,
        "mission": "ARTEMIS"  
    }
}
```

The possible message types are:

#### `RocketLaunched`
Sent out once: when a rocket is launched for the first time.
`launchSpeed` must be greater than zero.
```json
{
    "type": "Falcon-9",
    "launchSpeed": 500,
    "mission": "ARTEMIS"  
}
```

#### `RocketSpeedIncreased`
Continuously sent out: when the speed of a rocket is increased by a certain amount.
```json
{
    "by": 3000
}
```

#### `RocketSpeedDecreased`
Continuously sent out: when the speed of a rocket is decreased by a certain amount.
```json
{
    "by": 2500
}
```

#### `RocketExploded`
Sent out once: if a rocket explodes due to an accident/malfunction.
```json
{
    "reason": "PRESSURE_VESSEL_FAILURE"
}
```

#### `RocketMissionChanged`
Continuously sent out: when the mission for a rocket is changed.
```json
{
    "newMission":"SHUTTLE_MIR"
}
```

### Running the test program 💽
In the ZIP-file locate the executable that works for your system and run the following:

```bash
./rockets launch "http://localhost:8088/messages" --message-delay=500ms --concurrency-level=1
```

This launches the program which starts posting (request method: `POST`) messages to the URL provided with a delay of 500ms between each message.

To see all commands run `./rockets help` and for help on the `launch` command run `./rockets launch --help`.

> We are going to run the program against your solution with the default values.

### Your solution and our assessment 📝
Before submitting your solution please make sure that you have included all the necessary files/information for 
running and assessing your solution. You can either submit a ZIP-file or provide a link to an online version control provider like GitHub, GitLab or Bitbucket.

When reviewing your solution we are going to look at things such as:
- The documentation provided, i.e. is it clear how to run your service(s) and, perhaps, what considerations/shortcuts have you made.
- The overall design of your solution, e.g. how easy is the code to understand, can the service(s) scale and how maintainable your code is.
- The measures you have taken to verify that your code works, e.g. automated tests.

We do not expect you to spend more than **6 hours** on this challenge. 
If you do not succeed in completing everything submit what you have, so we have something to look at - that is much better than nothing! ☺️

---

## Solution

This repository includes a Go REST service that consumes the rocket messages, stores them in SQLite, materializes the current state for each rocket, and exposes that state through HTTP endpoints.

The implementation follows a clean architecture layout under `src/`:

- `src/cmd/rockets-api`: composition root and HTTP server startup.
- `src/internal/domain`: event types, canonical hashing, and rocket replay rules.
- `src/internal/service`: use cases for ingesting messages, querying rockets, and health checks.
- `src/internal/service/repository`: repository and unit-of-work interfaces used by services.
- `src/internal/repository/sqlite`: SQLite repository implementations, migrations, transactions, and row mapping.
- `src/internal/handler`: routes, request decoding, response envelopes, and HTTP status mapping.

The request flow is intentionally separated:

```text
handlers -> services -> repository interfaces -> SQLite repositories
```

### Running the API

Requirements:

- Go 1.25 or newer

Start the service:

```bash
go run ./src/cmd/rockets-api
```

By default the API listens on port `8088` and stores data in `rockets.db`.

Optional configuration:

- `PORT`: HTTP port, default `8088`
- `DB_PATH`: SQLite database path, default `rockets.db`

Example:

```bash
PORT=8088 DB_PATH=rockets.db go run ./src/cmd/rockets-api
```

Then run the challenge producer:

```bash
./darwin_arm64/rockets launch "http://localhost:8088/messages" --message-delay=500ms --concurrency-level=1
```

Run tests:

```bash
go test ./...
```

### API

Post a rocket event:

```bash
curl -i -X POST http://localhost:8088/messages \
  -H 'Content-Type: application/json' \
  -d '{
    "metadata": {
      "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
      "messageNumber": 1,
      "messageTime": "2022-02-02T19:39:05.86337+01:00",
      "messageType": "RocketLaunched"
    },
    "message": {
      "type": "Falcon-9",
      "launchSpeed": 500,
      "mission": "ARTEMIS"
    }
  }'
```

Responses:

- `201 Created`: new event stored and state rebuilt if the contiguous stream can materialize a rocket.
- `200 OK`: exact duplicate event.
- `200 OK`: conflicting duplicate ignored to avoid challenge-runner retry loops.
- `400 Bad Request`: malformed JSON or invalid message fields.

New event response:

```json
{
  "data": {
    "status": "created",
    "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
    "messageNumber": 1,
    "materialized": true
  }
}
```

Exact duplicate response:

```json
{
  "data": {
    "status": "duplicate",
    "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
    "messageNumber": 1,
    "materialized": false
  }
}
```

Conflicting duplicate response:

```json
{
  "data": {
    "status": "conflict_ignored",
    "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
    "messageNumber": 1,
    "materialized": false
  }
}
```

Error responses use a consistent envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "metadata.messageNumber must be positive"
  }
}
```

List rockets:

```bash
curl http://localhost:8088/rockets
curl 'http://localhost:8088/rockets?sort=speed&order=desc'
```

Supported sort fields are `channel`, `mission`, `speed`, `status`, and `lastMessageNumber`. Equal sort values use `channel` as a deterministic tie-breaker.

List response:

```json
{
  "data": [
    {
      "channel": "193270a9-c9cf-404a-8f83-838e71d9ae67",
      "type": "Falcon-9",
      "mission": "ARTEMIS",
      "speed": 500,
      "status": "launched",
      "explosionReason": null,
      "lastMessageNumber": 1,
      "lastMessageTime": "2022-02-02T19:39:05.86337+01:00",
      "pendingEvents": 0,
      "updatedAt": "2026-07-09T22:51:39Z"
    }
  ],
  "meta": {
    "count": 1,
    "sort": "channel",
    "order": "asc"
  }
}
```

Get one rocket:

```bash
curl http://localhost:8088/rockets/193270a9-c9cf-404a-8f83-838e71d9ae67
```

Health check:

```bash
curl http://localhost:8088/healthz
```

`GET /healthz` checks that the HTTP service is running and SQLite can execute a lightweight query.

Health response:

```json
{
  "status": "ok",
  "checks": {
    "sqlite": "ok"
  }
}
```

### Design Notes

The service uses SQLite-backed event sourcing. Every accepted event is written to the `events` table, and the current visible rocket state is stored in `rocket_states`.

The event table stores:

- `channel`: rocket/channel identifier
- `message_number`: per-channel ordering key
- `message_time`: timestamp sent by the rocket
- `message_type`: event type
- `payload_json`: canonical message payload JSON
- `event_hash`: hash used for duplicate conflict detection
- `received_at`: timestamp when this service accepted the event

The primary key is `(channel, message_number)`, which supports efficient ordered replay for one rocket:

```sql
SELECT *
FROM events
WHERE channel = ?
ORDER BY message_number;
```

On startup the service creates both tables if needed, enables SQLite WAL mode, and sets `busy_timeout`.

### Ordering And Visibility

State is ordered by `metadata.messageNumber`, not by `metadata.messageTime`.

Out-of-order future events are stored immediately, but materialized state applies only the contiguous sequence starting at message `1`. A rocket becomes visible only when message `1` exists and message `1` is `RocketLaunched`.

If message `1` is missing, later events are persisted but not materialized. If message `1` is not `RocketLaunched`, the stream is invalid and no rocket state is exposed. If replay cannot produce a valid state, any existing row in `rocket_states` for that channel is deleted.

`pendingEvents` means stored events for the channel that have not been applied because they are after a sequence gap. For example, if stored events are `1, 2, 4, 5`, the applied state uses `1, 2`, `lastMessageNumber` is `2`, and `pendingEvents` is `2`.

### Event Semantics

- `RocketLaunched` initializes the state only when it is message `1`.
- A later `RocketLaunched` event does not reset rocket state.
- `RocketSpeedIncreased` adds `by`.
- `RocketSpeedDecreased` subtracts `by` exactly; speed is not clamped to zero.
- `RocketMissionChanged` replaces mission.
- `RocketExploded` sets `status=exploded` and stores the reason.
- Later events after `RocketExploded` are still applied in message-number order unless the domain explicitly says explosion is terminal.

### Deduplication And Conflicts

Events are deduplicated by `(channel, messageNumber)`.

The service computes `event_hash` from the meaningful event content:

- `channel`
- `messageNumber`
- `messageTime`
- `messageType`
- canonical message payload

`received_at` is not part of the hash.

The payload is canonicalized before hashing, so equivalent JSON formatting produces the same hash. For example, `{"by":3000}` and a multi-line JSON object with the same value are treated as the same event.

If the same `(channel, messageNumber)` arrives with a different `event_hash`, this implementation keeps the first stored event and returns `200 OK` with `status=conflict_ignored`. This is intentional for the challenge runner because non-2xx responses are retried and can cause retry loops when rerunning deterministic producer data against an existing database. In production, this should be logged, metered, and routed to a dead-letter or data-integrity workflow.

### Transaction Boundary

`POST /messages` uses one database transaction:

1. Validate and canonicalize the event.
2. Try to insert into `events`.
3. If duplicate, compare `event_hash`.
4. If exact duplicate, commit the read-only transaction and return `200`.
5. If conflicting duplicate, keep the existing event, commit the read-only transaction, and return `200`.
6. If new event, load the current materialized state and fetch only stored events from `last_message_number + 1`.
7. Apply the contiguous event sequence, count unapplied events after the last applied message, and upsert or delete `rocket_states`.
8. Commit.
9. Return `201`.

This prevents storing an event without updating state, or updating state without storing the event.

### Trade-Offs And Limitations

The service advances materialized state incrementally from `last_message_number + 1` instead of replaying the full channel after every message. If there is a sequence gap, the state remains at the last contiguous message and `pendingEvents` is updated by counting stored events after that applied message.

Structured JSON logs are emitted for message ingestion outcomes, replay failures, and HTTP request latency.

SQLite is a good fit for this challenge and provides local restart recovery, but it is not ideal for horizontally scaled multi-instance writes. For multiple service instances, use a shared production database, a queue partitioned by channel, or both.

