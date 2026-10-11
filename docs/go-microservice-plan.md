# xenomit-server — Go Microservices Architecture Plan

Target design for a DarkOrbit-compatible game server in Go, built to modern
(2026) backend standards. It must stay **wire-compatible** with the DarkOrbit
client protocol (binary command packets + legacy `0|` string protocol tunneled
through command 4224), support the feature set of Darkobit 10.0, and adopt the
structural qualities Kalaazu demonstrates (versioned codecs, per-map worlds,
single-writer loops, event-driven outbound pipeline).

Documented sources:

- [architecture.md](architecture.md) — module/topology analysis
- [networking.md](networking.md) — wire protocol, ports, dispatch
- [events.md](events.md) — tick model, game events, event buses

---

## 1. Goals & non-goals

**Goals**

- Accept the real client on the real ports (game TCP `8080`, chat TCP `9338`,
  Flash policy on `843`/in-band), bit-for-bit protocol compatible.
- Correctly simulate the observable game: movement dead-reckoning, range-based
  visibility, laser/rocket combat, mines, collectables, portals, stations,
  NPCs with AI, PETs, groups, clans, galaxy gates, duels, JPB/UBA/Spaceball.
- Reimplement the CMS-facing surface (registration, login/session issuance,
  hangar/shop/clan actions, admin ops, presence queries) as a REST/gRPC API —
  the modern replacement for Darkobit's `SocketServer:4301` + PHP `Functions.php`.
- Horizontally scale: map shards and gateways scale independently; state lives
  outside the sim processes where possible.
- Observable, deployable, and operable by one engineer: OTel everywhere,
  IaC, CI/CD, no `.exe` watchdog.

**Non-goals**

- Botting/anti-cheat beyond server-side authority and rate limits (separate effort).
- Unity/HTML5 client work — protocol compatibility is the contract.
- Byte-exact reproduction of every `class_*` obfuscated command; a spec-driven
  codec generator covers the catalog progressively.

---

## 2. System overview

```
                          ┌──────────────────────────┐
   Flash client :8080 ───▶│                          │
   Flash client :9338 ───▶│      gateway-service     │  stateless per-conn
   Flash policy   :843 ───▶│  (TCP edge, codecs,      │  protocol termination,
                          │   session router)        │  per-version codecs
                          └───────────┬──────────────┘
                                      │ NATS (core) — hot path, subjects:
                                      │   in.<mapId>        ingress commands
                                      │   out.<sessionId>   egress packets
                 ┌────────────────────┼───────────────────────────┐
                 ▼                    ▼                           ▼
       ┌──────────────────┐ ┌──────────────────┐        ┌──────────────────┐
       │  world-service   │ │  events-service  │        │   chat-service   │
       │ map shards, ECS- │ │ JPB/UBA/Spaceball│        │ rooms, whispers, │
       │ style data, tick │ │ duels, gates,    │        │ moderation, duel │
       │ loops, NPC AI    │ │ scheduled events │        │ invites          │
       └────────┬─────────┘ └────────┬─────────┘        └────────┬─────────┘
                │ domain events      │                           │
                ▼ (Kafka)            ▼                           ▼
       ┌────────────────────────────────────────────────────────────────┐
       │                         event backbone                        │
       │  topics: session.login, combat.*, economy.*, chat.*, admin.*,   │
       │          world.telemetry, ranking.deltas                       │
       └────────┬───────────────┬───────────────┬───────────────┬────────┘
                ▼               ▼               ▼               ▼
       ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
       │player-service│ │social-service│ │economy-svc   │ │ranking-svc   │
       │accounts, ship│ │clans, groups,│ │shop, boosters│ │leaderboards, │
       │configs, items│ │diplomacy,    │ │inventory tx  │ │cron recompute│
       │settings      │ │contacts      │ │              │ │              │
       └──────┬───────┘ └──────┬───────┘ └──────┬───────┘ └──────┬───────┘
              ▼                ▼                ▼               (reads stream)
       ┌──────────────────────────────────────────────────────────────┐
       │  PostgreSQL (system of record)  +  Redis (cache, presence,    │
       │  session routing, leaderboards, rate limits, pub/sub fanout)   │
       └──────────────────────────────────────────────────────────────┘
                                     ▲
                 ┌───────────────────┘
                 ▼
       ┌──────────────────┐    ┌──────────────────┐
       │ webapi-service   │───▶│  admin/backoffice│  REST for CMS/portal
       │ registration,    │    │  (replaces :4301 │  + session issuance
       │ login, hangar,   │    │   JSON bridge)   │  for the game client
       │ shop UI, clans   │    └──────────────────┘
       └──────────────────┘
```

---

## 3. Services

### 3.1 `gateway-service` — the edge

The only service that speaks the client wire protocol.

- **Listeners**: game TCP (`:8080`), chat TCP (`:9338` — or its own
  `chat-edge` if we want separate scaling), Flash policy server (`:843`) and
  in-band `<policy-file-request/>` handling exactly like `GameClient` does.
- **Codec layer** (`pkg/protocol`): versioned implementations mirroring
  Kalaazu's `v4`/`v10` split:
  - `v10`: `[u16 len][u16 cmdId][payload]`, big-endian, per-field bit-rotation
    tables ported from `requests/*.cs`/`commands/*.cs` (the `x >> 4 | x << 28`
    style obfuscation), `LegacyModule` (4224) wrapping `0|`-strings.
  - `v4`: the legacy pipe-delimited protocol.
  - Commands generated from a declarative spec (see §8) — not hand-written
    per command.
- **Session object**: connection ↔ `sessionId`/`userId`/`mapId`/`worldShard`.
  Unauthenticated sockets only accept `LoginRequest`; post-auth it publishes a
  `session.opened` event and starts forwarding.
- **Ingress path**: decode → envelope `{sessionId, playerId, mapId, cmdId,
  payload, receivedAt}` → publish to `in.<mapId>` (NATS) for world-owned
  commands, or to service-local subjects (`chat.msg`, `group.*`, …) for
  non-sim commands — the analogue of Darkobit's `Handler.Commands` routing,
  split by domain.
- **Egress path**: subscribe `out.<sessionId>` → raw write to the socket.
  Any service can reach any player through this subject — the distributed
  version of Kalaazu's `SendCommand`/`ChannelManager` and Darkobit's
  `SendPacketToAll/Map/Clan` helpers (`out.map.<id>`, `out.all` subjects cover
  those fan-out modes).
- **Why NATS core and not Kafka on this path**: µs-level brokerless fan-in,
  subject wildcards, no ordering/partition tax. Kafka is for *durable domain
  events*, not per-tick frame delivery.

### 3.2 `world-service` — simulation shards

Owns all map state and entity simulation. **Sharded by mapId**: one map is one
actor — a goroutine with a mailbox.

```go
type MapShard struct {
    world    *ecs.World            // internal/ecs — data-oriented component store
    inbox    chan WorldCmd          // single-writer queue == Kalaazu WorldAction
    tick     *time.Ticker          // 20 Hz (configurable; DO era = ~12 Hz)
    grid     *spatial.UniformGrid  // replaces Spacemap.CharacterTicker O(n²)
}
```

Per tick: drain `inbox` → advance movement/AI/combat/visibility systems → emit
outbound packets (`out.<sessionId>`, `out.map.<id>`) → emit domain events on
Kafka for durable matters (kills, loot, damage aggregates — not per-frame
positions).

Systems map 1:1 from the references:

| Reference logic | Shard system |
|---|---|
| `Spacemap.CharacterTicker` (in-range diff) | `VisibilitySystem` on a spatial grid, interval 250 ms (Kalaazu cadence) + `VisibleBy` reverse index |
| `Movement.ActualPosition`/`Move` | `MovementSystem`: intent → dead-reckoning params → `MoveCommand` broadcast to observers |
| `Npc.Tick` + `NpcAI` | `NpcAiSystem` FSM (search/fly-to/wait), frozen unless `VisibleBy` non-empty; spawner keeps `NpcsBase` populations topped up |
| `AttackManager`, mine detonation, rockets | `CombatSystem` (1 s laser cadence, `TargetedBy` bookkeeping, destruction → `combat.kill` event) |
| `CheckActivatables`/`CheckRadiation`, POIs | `ZoneSystem` (secure zones, equip zones, radiation, mine-blocked tiles) |
| `Portal` activation | `PortalSystem` → emits `session.jump` (cross-shard transfer) |
| PET, groups pings, boosters timed expiry | dedicated systems per subsystem |

**Cross-shard player transfer** (portal jump / event teleport): the shard
serializes a compact `PlayerSnapshot` into Redis (`handoff:<sessionId>`),
publishes `world.transfer{from,to}`, the destination shard claims it — the
modern version of `player.Jump(mapId, pos)` + `Spacemap.AddCharacter`.

NPC population and map topology come from `config.maps` (loaded from DB,
equivalent to `server_maps`/`maps_*` tables) — a `world-controller`
control loop handles dynamic spawning (replenish killed NPCs, event-driven
spawns).

### 3.3 `chat-service`

Implements the `% @ | } #` protocol semantics behind the gateway's chat edge:
rooms (`Room` model incl. faction-scoped rooms), whispers, badword filter,
moderation (`/kick`, `/ban` → writes `server_bans` via player-service),
dynamic/private rooms (duel invites keep working: `cj`/`bz` flow →
`events-service` duel creation), admin broadcast commands (`/msg`, `/restart`
→ control API). Chat history → Kafka `chat.messages` for audit; live delivery
via `out.<sessionId>`.

### 3.4 `social-service`

Clans (CRUD, members, ranks, diplomacy war/NAP/alliance), groups (invite
lifecycle, pings, follow, loot share), contacts/friends list, clan chat relay.
Owns `server_clans*`, `clans_*` tables; emits `clan.changed` etc. so the
world-service can push `ClanChangedCommand` to in-range players.

### 3.5 `economy-service`

Shop, `BuyItem`, boosters, currency ledger (credits/uridium/EP/honor), inventory
& equipment configs, skill tree, PET purchase, auction/trade stubs.
**Ledger-first**: every mutation is a transactional event on `economy.tx`
(idempotency-keyed), then cached stats published so `world-service` and
`webapi` stay coherent — replaces Darkobit's "PHP writes DB then pokes :4301".

### 3.6 `events-service`

Scheduled & instanced content: `JackpotBattle`, `Spaceball`, `UBA` matchmaking,
duels, galaxy-gate runs, invasion events. These are **ephemeral maps**: it asks
the world-controller for a shard slot running map 42/101/121 (or a gate map),
owns the event FSM (countdowns, radiation-POI shrink = `ZoneSystem` directives,
winner detection, rewards via `economy.tx`), then tears down. Replaces
`EventManager`'s static singletons with a scheduler (cron + admin API + Kafka
triggers).

### 3.7 `player-service`

Accounts, characters, hangar/ships, configurations (pre-computed stats —
Kalaazu's `accounts_configurations` model), settings, skill tree, titles,
session tokens. The **write path owner** for `player_*`/`accounts_*`; every
other service reads through its cache. `session.issue`/`session.validate` is
the modern `CheckSessionId`.

### 3.8 `webapi-service`

REST (+ optional GraphQL) replacing the PHP CMS: register/login (bcrypt/argon2),
`POST /session` (issues the `sessionId` the client presents on :8080),
company choose, hangar/equipment editing, clan pages, shop UI, admin ops
(`/admin/kick`, `/admin/ban`, `/admin/broadcast`, `/admin/events/start`),
presence (`/players/{id}/online`, `/stats/online`), ranking endpoints.
Everything the old `SocketServer` switch did is a gRPC call into the owning
service now.

### 3.9 `ranking-service`

Stream consumer rebuilding leaderboards (players/clans/event winners) into
Redis ZSETs + periodic Postgres snapshots — replaces the PHP `cronjobs/ranking`
endpoint the `emulator-helper` pinged hourly.

### 3.10 (optional) `replay/telemetry-service`

Kafka consumers for audit logs (`log_player_kills`, `log_event_jpb`
equivalents), packet-level telemetry, admin replay tooling.

---

## 4. Communication patterns

| Path | Transport | Notes |
|---|---|---|
| client ↔ gateway | TCP binary (v4/v10), `:8080`, chat `:9338`, policy `:843` | wire-compatible, untrusted input |
| gateway → world | NATS `in.<mapId>` | ordered per session inbox; map-sharded |
| world → client | NATS `out.<sessionId>` / `out.map.<id>` / `out.all` | egress fan-out |
| svc → svc sync | **gRPC** (+Connect for the webapi) | protobuf contracts in `proto/` |
| durable domain events | **Kafka** | `session.*`, `combat.kill`, `economy.tx`, `chat.messages`, `clan.*`, `admin.audit`, `world.telemetry` |
| presence/session routing | Redis | `sess:<id> → {node,map,shard}`, `online:set`, `pos:<id>` cache-aside |
| scheduled work | Kafka delayed topics / cron jobs | restart countdowns, event start, rank rebuild |
| cross-shard transfer | Redis handoff + `world.transfer` | portal jumps, event teleports |

Rules of thumb:

- **Hot path** (packets, visibility, movement) never touches Kafka or the DB.
- **Cold path** (loot, kills, purchases, bans, settings saves) always lands on
  Kafka first, then Postgres via the owning service — a lightweight outbox.
- Every service is **stateless or shard-scoped**; Redis holds the session
  routing table so senders never need to know where a session lives.

---

## 5. Data layer

**PostgreSQL** as system of record (keep Darkobit's table names where sensible
for drop-in CMS compatibility; optionally adopt Kalaazu's normalized
`accounts_*`/`items`/`rewards_*` model — preferred long-term since it makes
itemization queries cheap). Migrations via `goose`/`atlas`; schema as code.

**Redis** for: session registry, presence sets, `handoff:*`, leaderboards
(ZSET), per-map pub/sub fallback, cooldown/idempotency keys, config cache.

**Kafka** for durable events + the audit trail. Retention tuned per topic
(`combat.*` short, `economy.tx` long/compact).

DB access pattern in `world-service`: **read-through at login** (player
snapshot via `player-service` + Redis), **write-behind on disconnect &
checkpoint timer** (the `GameSession.Disconnect` save path plus periodic
dirty-flush — crash safety), never synchronous in the tick loop.

---

## 6. Concurrency model inside a shard

Go's channel model makes the Kalaazu `WorldAction` pattern idiomatic:

```go
for {
    select {
    case cmd := <-m.inbox:          // single-writer mutation
        m.world.Apply(cmd)
    case <-m.tick.C:                // 20 Hz
        m.systems.Step(m.world, m.delta())
    case <-m.shutdown:
        m.persistAndExit()
    }
}
```

- One goroutine owns one map's world; no locks on game state, ever.
- Heavy AI/pathfinding can fan out to a worker pool and re-enter through `inbox`.
- `VisibleBy` sets are maintained incrementally (grid cell enter/leave), not
  recomputed O(n²).
- Packets are encoded once per command and fanned out to session subjects —
  same optimization as Kalaazu's `send(Collection, commands)`.

---

## 7. Repository layout (Go monorepo)

```
xenomit-server/
├── go.work
├── proto/                       # protobuf: internal RPC + event schemas
│   ├── session/v1/  world/v1/  social/v1/  economy/v1/  admin/v1/
├── pkg/
│   ├── protocol/                # client wire protocol
│   │   ├── codec/               # length-prefixed framing, ByteArray/ByteParser ports
│   │   ├── legacy/              # 0| pipe protocol + LegacyModule tunneling
│   │   ├── v4/  v10/            # versioned command catalogs
│   │   └── gen/                 # generated from spec/commands.yaml
│   ├── ecs/                     # entity/component/store, systems runtime
│   ├── spatial/                 # uniform grid, AABB, range queries
│   ├── movement/                # dead-reckoning math (Movement port)
│   ├── events/                  # typed Kafka/NATS envelopes, publisher
│   ├── session/                 # session token issue/validate middleware
│   └── observability/           # OTel setup, metrics, log ctx
├── services/
│   ├── gateway/                 # edge: listeners, codec, session router
│   ├── world/                   # map shards + systems + world-controller
│   ├── chat/  social/  economy/ events/  player/  webapi/  ranking/
│   └── tools/                   # loadbot, packet replayer, map importer
├── deploy/
│   ├── docker-compose.yml       # Postgres+Redis+Kafka+NATS+services
│   ├── helm/                    # production charts
│   └── terraform/               # optional cloud provisioning
└── docs/
```

`pkg/protocol/gen` is important: the ~200 command classes with hand-specified
bit rotations are authored as `spec/commands.yaml` entries (`id`, `fields`,
per-field `rotate`/`shift` ops) and codegen'd into Go encoders/decoders —
hand-porting is where emulator rewrites historically die.

---

## 8. Compatibility contract

| External surface | Implementation |
|---|---|
| `:8080` binary protocol v10 (len,u16 id,big-endian,rotated fields) + 4224 `LegacyModule` for `0|` strings | `pkg/protocol/v10` + `legacy` |
| `:8080` v4-era `0|` string protocol | `pkg/protocol/v4` (Kalaazu-style versioned stack) |
| Flash cross-domain policy | gateway in-band + `:843` listener |
| `:9338` chat text protocol | gateway chat edge → chat-service |
| `sessionId` login (`player_accounts.sessionId`) | webapi `/session` issues; gateway validates via player-service |
| `:4301` JSON `{Action,Parameters}` CMS bridge | replaced by webapi REST + gRPC; a thin compat shim can expose the old JSON API during migration |
| `server.sql` schema | Postgres port; JSON columns → real columns where queried, kept where opaque |
| Admin commands (`/msg`, `/jump`, `/start_jpb`…) | chat-service command parser → admin gRPC |

---

## 9. Engineering standards (2026)

- **Runtime**: Go 1.24+; `GOMAXPROCS`-aware sharding; `go tool` / `go test -race`
  in CI; `golangci-lint`; `buf` for proto lint/breaking checks.
- **Observability**: OpenTelemetry traces on envelope flow (ingress → system →
  egress), Prometheus metrics (tick p99, packets/s, sessions, shard load),
  structured logs (`log/slog`), Grafana dashboards + alerts; Sentry-style error
  reporting for the edge.
- **Resilience**: gateway backpressure + per-session rate limits; world shard
  checkpointing & snapshot restore; graceful drain via `world.transfer` so a
  rolling deploy moves players between shards without disconnects.
- **Delivery**: Dockerfile per service (distroless), docker-compose for local,
  Helm for prod; HPA on CPU/custom session-count metrics; KEDA scaler on Kafka
  lag for consumers.
- **Testing**: protocol golden tests (captured packets ↔ codec round-trip),
  shard simulation tests (deterministic ticks), loadbot swarm tests
  (`services/tools/loadbot`), contract tests between services (`buf` + gRPC
  mocks), e2e with a scripted fake client.
- **Security**: all SQL parameterized (the original interpolates — don't copy
  that), session tokens rotated, admin actions audit-logged, packet size caps
  (that 1 KB buffer assumption is a real DoS surface in the original).

---

## 10. Migration / build order

1. **Foundations**: monorepo, `pkg/protocol` v10 codec + spec generator,
   Postgres schema port, docker-compose infra.
2. **Vertical slice**: gateway + world (one map, move+see+shoot) +
   player-service login — a client can log in, fly, and kill a Streuner.
3. **Core loop**: visibility diffing, NPC AI, collectables/mines/portals,
   configs/boosters, chat-service, social-service.
4. **Meta**: webapi (registration→play flow), economy, ranking, admin ops.
5. **Events & instances**: galaxy gates, duels, JPB/UBA/Spaceball via
   events-service + shard handoff.
6. **Hardening**: loadbots, packet fuzzing, observability dashboards, HA.

---

## 11. What we deliberately keep from each reference

| From Darkobit | From Kalaazu |
|---|---|
| The wire protocol, packet ids, and legacy tunneling (it's the contract) | Versioned codec stacks (`v4`/`v10` → `pkg/protocol/*`) |
| Feature scope & map/event content model (`server_maps` npc/portals/stations JSON) | Per-map world + single-writer action queue (`inbox chan WorldCmd`) |
| Chat protocol semantics & admin command surface | Event-driven outbound pipeline (events → egress fan-out) |
| Session-grace disconnect semantics (reconnect reclaim) | Visibility as diffed sets with `VisibleBy` reverse index |
| The `:4301` action catalog (as the webapi spec) | Pre-computed config stats (`accounts_configurations`) as a caching pattern |
