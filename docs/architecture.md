# Architecture

This document describes the overall architecture of the two reference
implementations: the **Darkobit stack** (local files under
`C:\Users\Yagiz\Downloads\Darkobit-Files\Darkobit`) and **Kalaazu**
(`manulaiko/Kalaazu`, `develop` branch).

---

## 1. The Darkobit stack

The directory contains three distinct deliverables:

```
Darkobit/
├── darkorbit-emulators-master/    # game servers
│   ├── DarkOrbit 7.5.3/           # C# emulator (older protocol era)
│   ├── DarkOrbit 9.0/             # Java emulator (SpaceBattles base, sources in src/)
│   └── DarkOrbit 10.0/            # C# emulator — most complete, references below use this
├── darkorbit-cms-master/          # PHP website + Flash client assets + MySQL schema
│   ├── index.php, files/api.php   # AJAX entry point (action=register|login|buy|...)
│   ├── files/classes/             # Database.php, Functions.php, SMTP.php, Socket.php
│   ├── spacemap/main.swf          # the actual game client (Flash)
│   └── server.sql                 # 16-table MySQL schema
└── emulator-helper-master/        # C# watchdog: respawns dark_orbit.exe, pings cron URL
```

Deployment shape (historical, XAMPP-era):

```
Browser ──HTTP──▶ Apache/PHP CMS ──MySQL──▶ server DB
   │                    │
   │ Flash main.swf     │ fsockopen JSON {Action, Parameters}
   │                    ▼
   ├──TCP 8080──▶ GameServer ──┐
   ├──TCP 9338──▶ ChatServer ──┤ single C# process
   │            SocketServer ◀─┘    (port 4301)
   │
   └── emulator-helper.exe: polls Process list every 1s,
       restarts the emulator, calls http://127.0.0.1/cronjobs/ranking hourly
```

### 1.1 Process topology (DarkOrbit 10.0)

One console process, three listeners, one global tick loop. From `Program.cs`:

```
Main()
 ├── CheckMySQLConnection()      // SqlDatabaseManager.Initialize() w/ 6 retries
 ├── LoadDatabase()              // QueryManager.LoadClans/LoadShips/LoadMaps → statics
 ├── InitiateServer()
 │    ├── JsonConvert defaults (ObjectCreationHandling.Replace)
 │    ├── Handler.AddCommands()  // ~45 packet-ID → handler registrations
 │    ├── Room.AddRooms()        // 5 hardcoded chat rooms
 │    ├── EventManager.InitiateEvents()  // JackpotBattle, Spaceball, UBA singletons
 │    └── StartListening()
 │         ├── GameServer.StartListening   :8080   (game client)
 │         ├── ChatServer.StartListening   :9338   (chat client)
 │         ├── SocketServer.StartListening :4301   (CMS bridge)
 │         └── TickManager.Tick            (global world loop, ~84 ms period)
 └── KeepAlive()                 // stdin admin commands: /restart, /list_players
```

### 1.2 Global state

`Managers/GameManager.cs` is a static registry — all state lives in
`ConcurrentDictionary` fields:

- `GameSessions : ConcurrentDictionary<int, GameSession>` — online players keyed by userId
- `ChatClients` — chat connections keyed by userId
- `Spacemaps` — every map instance
- `Ships`, `Clans`, `BattleStations`, `Groups`
- Broadcast helpers: `SendCommandToMap`, `SendPacketToMap`, `SendPacketToAll`,
  `SendPacketToClan`, `SendChatSystemMessage`

There is no abstraction layer: every subsystem reaches into `GameManager` statics
directly. The consequences are typical for this generation of emulators — simple
to follow, but the world is effectively single-shard and all logic is
synchronized through shared mutable dictionaries.

### 1.3 World model

```
Object (id, hash)
 └── Attackable                        // HP/shield/nanohull, InRangeCharacters,
     │                                 //   Attackers, Selected, RenderRange=2000
     ├── Character                     // Position, Spacemap, Ship, Movement fields
     │   ├── Player                    //   + Storage, managers, Equipment, Settings
     │   ├── Npc                       //   + NpcAI state machine, Respawn()
     │   ├── Pet                       //   + Owner, gears
     │   └── Objects.Spaceball         // event ball entity
     └── Activatable                   // clickable map assets
         ├── Portal                    // jump gates (target map+position)
         └── Stations                  // HomeStation, RepairStation,
                                      // BattleStation, Satellite, ...
```

`Spacemap` owns four collections: `Characters`, `Activatables`, `Objects`
(collectables/mines/assets), `POIs`. Map definitions (npcs/portals/stations/
options) are JSON columns in `server_maps`, loaded once at boot by
`QueryManager.LoadMaps()`, which constructs each `Spacemap` and spawns its NPCs,
portals, stations and bonus boxes.

Player state is a "managers" cluster under `Game/Objects/Players/Managers/`:
`AttackManager`, `AmmunitionManager`, `BoosterManager`, `CpuManager`,
`DroneManager`, `SettingsManager`, `SkillManager`, `TechManager`, `TimeManager` —
each owning one subsystem of player state plus the packets that replicate it.

### 1.4 Simulation loop

`Game/Ticks/TickManager.cs` keeps a `List<Tick>`; everything that needs periodic
work implements `void Tick()` and registers itself:

- `Spacemap.Tick` → `CharacterTicker()` — **O(n²) in-range recomputation** per
  map tick: every character vs every other character, plus per-player object
  proximity scans (≤1250 units), mine proximity detonation, radiation/secure-zone
  checks.
- `GameSession.Tick` → inactivity logout (3 min), delayed socket-close grace
  (2 min, allows reconnects to reclaim a session).
- `Npc.Tick` → movement interpolation, `NpcAI.TickAI()`, shield regen, attack.
- `Player.Tick` → heal/cooldowns/logout timers, pet upkeep, booster expiry.
- Event objects (`Duel`, `JackpotBattle`, `Uba`) tick their own state machines.

The nominal `TICKS_PER_SECOND = 84` is (mis)used as a **millisecond** delay —
the loop is `await Task.Delay(84)` → ~12 ticks/second effective.

### 1.5 Persistence

`Net/mysql/` is a Habbo-style DB pool (`SqlDatabaseManager` → pooled
`SqlDatabaseClient`s with `QueryAdapter`s). `Managers/QueryManager.cs` issues
synchronous, string-interpolated SQL directly from game code paths (login,
disconnect, shop actions, event logging).

Schema (`server.sql`, 16 tables) mixes columns + JSON blobs:
`player_accounts(sessionId, pilotName, factionId, shipId, rankID, clanID,
data JSON, destructions JSON, ...)`, `player_settings` (one JSON column per
settings group), `player_equipment` (items/configs/boosters/modules/skill_points
JSON), `server_maps`, `server_ships`, `server_battlestations`, `server_bans`,
clan tables, `log_*` tables. Login validation is a `sessionId` string written by
the PHP CMS when the player clicks "play".

### 1.6 The CMS bridge

`Socket.php` does `fsockopen(tcp://127.0.0.1:4301)`, writes one JSON document
`{"Action": "...", "Parameters": {...}}`, and (for `Get`) reads a 1 KB reply.
`SocketServer.Execute` is a giant `switch` over `Action`:

- Queries: `OnlineIds`, `OnlineCount`, `IsOnline`, `IsInEquipZone`,
  `GetPosition`, `AvailableToChangeShip`
- Mutations: `BanUser`, `KickPlayer`, `BuyItem`, `ChangeShip`, `ChangeCompany`,
  `UpdateStatus` (re-reads equipment from DB and pushes new stats),
  `UpgradeSkillTree`/`ResetSkillTree`
- Clan lifecycle: `CreateClan`, `DeleteClan`, `JoinToClan`, `LeaveFromClan`,
  `ChangeClanData`, `StartDiplomacy`, `EndDiplomacy`

So the "web → game" path is: PHP writes MySQL rows **and** pokes the live process
over 4301 so the online player's in-memory state is refreshed.

### 1.7 Version differences

| | 7.5.3 (C#) | 9.0 (Java) | 10.0 (C#) |
|---|---|---|---|
| Legacy `0|...` protocol | `ILegacyHandler`, more legacy handlers | `LegacyModuleRequest` + `CommandLookup` | `LegacyModule` wrapped in binary cmd 4224 |
| Servers | Game+Chat+Socket (+`ServerManager`) | `game_server`, `chat_server`, `policy_server`, `socket_server` packages | Game+Chat+Socket |
| AI | none | `AlienAI`, `ProtegitAI`, `CubikonAI`, `IceMeteoroidAI`, `SpaceballAI` | single `NpcAI` FSM |
| Battle stations | Satellite/BattleStation as Activatables | `ClanBattleStation`, turrets | full module/build/deflector model |
| Persistence | same MySQLManager port | `mysql/MySQLManager` + `QueryManager` | same as 7.5.3 |

---

## 2. Kalaazu

Multi-module **Gradle** project, **Spring Boot 4.0-M2**, **Java 25**. A monolith,
but structured as cleanly-separated modules wired by the Spring application-event
bus.

```
com.kalaazu
├── Launcher/      JavaFX dashboard (AtlantaFX); starts/stops the server via events
├── Server/        the game server
│   ├── Server.java                 @EventListener(StartServer/StopServer)
│   ├── game/
│   │   ├── GameServer.java         abstract Netty bootstrap (boss+worker NIO groups)
│   │   ├── PolicyServer.java       Flash cross-domain XML on its own port
│   │   ├── Packet.java             version-agnostic read/write interface
│   │   ├── netty/                  ChannelManager, GameSession (prototype bean),
│   │   │                           InboundHandler
│   │   ├── commands/               InCommand/OutCommand, CommandBuilder (Strategy)
│   │   ├── v4/                     legacy "0|..." string protocol implementation
│   │   └── v10/                    binary module-command protocol implementation
│   ├── ecs/                        Artemis-ODB world
│   │   ├── GameLoop.java           Runnable per map: drains WorldAction queue,
│   │   │                           sets delta, world.process()
│   │   ├── WorldAction.java        functional interface for deferred world edits
│   │   ├── component/              Position/Speed/Movement(+Intent)/Health/Target/
│   │   │                           TargetedBy/View/VisibleBy/NpcAi/Map/Portal/…
│   │   ├── system/                 VisibilitySystem, NpcAiSystem,
│   │   │   movement/{Intent,Update,Completion,Broadcast}System
│   ├── event/                      SendCommand(s), SendCommand(s)ToSessions,
│   │                               BroadcastCommand(s), GameSessionStarted/Stopped,
│   │                               EndGameSession(If)
│   └── service/                    MapService, GameLoopService,
│                                   SessionInitializationService
├── Persistence/   Spring Data JPA entities + services; MariaDB schema in
│                  database/ (one folder per table: structure/relations/dump.sql,
│                  ~80 tables, fully normalized)
├── CMS/           Spring REST controllers + Vue.js SPA (login/register/company)
└── Utils/         logging, math (Vector), shared types
```

### 2.1 Boot flow

`Launcher` → Spring context → JavaFX `MainLayout` publishes `StartServer` →
`Server.start` initializes `MapService` (loads `MapsEntity` rows, spawns
per-map `World`s and static entities), picks the `GameServer` bean matching the
configured `Version` (`v4` or `v10`), starts it and the `PolicyServer` on a
`virtualThreadTaskExecutor`, then publishes `ServerStarted`.

### 2.2 Per-map ECS worlds

`GameLoopService.initializeMap(MapsEntity)`:

1. Builds an Artemis `WorldConfiguration` containing **every `BaseSystem` bean**
   (systems are `@Scope("prototype")` so each world gets its own instances).
2. Creates `GameLoop(mapId, world)` and schedules it at `game.tickRate`
   (~50 ms → 20 ticks/sec) on a shared `TaskScheduler`.
3. `MapService.initializeMap` then populates entities: NPCs (random positions,
   negative ids), collectables, stations, portals — each `world.create()` +
   component assembly via `EntityEdit`.

All cross-thread mutation goes through `gameLoop.addAction(WorldAction)` — a
`ConcurrentLinkedQueue` drained at the top of each tick. This is the single
writer model that keeps the world lock-free.

Systems of note:

- **`VisibilitySystem`** (`IntervalIteratingSystem`, 0.25 s): per player, diffs
  the entity sets inside `renderDistance` against the `ViewComponent` bags from
  last pass, emits entity-create/remove `OutCommand`s, maintains the reverse
  `VisibleByComponent` index, and re-broadcasts a player's own
  appearance/disappearance to observers. Targeted/targeting entities are kept
  visible regardless of range.
- **`MovementIntentSystem`/`MovementUpdateSystem`**: a `MovementIntentComponent`
  is attached by handlers/AI; the update system converts intent →
  `MovementComponent{justStarted}`, integrates positions with `world.getDelta()`,
  `MovementCompletionSystem` finalizes, `MovementBroadcastSystem` sends a
  `MoveEntityCommand` to `VisibleBy` observers once per movement start.
- **`NpcAiSystem`**: wander AI — `ai.frozen` unless some player's `ViewComponent`
  sees the NPC (CPU-saving), then random destination every 5–10 s.

### 2.3 Command abstraction

`InCommand`/`OutCommand` + `CommandBuilder` = version-agnostic output:

```
systems call ──▶ commandBuilder.buildCommands(CommandType.EntityCreationCommand,
                                              world, entityId, entityType)
                    └─▶ Map<Version, Map<CommandType, List<CommandBuilderInterface>>>
                        built at startup from all Spring beans
```

So ECS systems never touch protocol details; adding a client version means
adding a `vN` package with its own `Packet`, `PacketSerializer`, `GameServer`,
handlers and command builders.

### 2.4 Persistence

Fully normalized MariaDB schema (~80 tables): `users`, `accounts` (character),
`accounts_hangars`, `accounts_ships`, `accounts_configurations`
(**pre-computed** damage/health/shield/speed per config — combat reads these),
`accounts_items` + join table, `items`, `ships`, `npcs`, `rewards*` loot tables,
`maps`, `maps_{portals,stations,npcs,collectables}`, `clans*` + roles +
diplomacies, `galaxygates*` (waves/spawns/spins/probabilities), `skilltree_*`,
`skylab_*`, `techfactory_*`. JPA entities + Spring services — the opposite of
Darkobit's JSON-blob columns.

### 2.5 CMS

`CMS` module = Spring controllers (`CompanyController`, `ExternalController`,
`LoginService`, `RegisterService`) + Vue SPA (Vuex store, `api` service layer).
Registration creates `users` + `accounts` + `accounts_hangars` +
`accounts_ships`; company choose sets `accounts.factions_id`. There is **no
chat server** and no live `SocketServer` equivalent — the CMS is pure REST/DB.

---

## 3. Architectural comparison

| Concern | Darkobit 10.0 | Kalaazu |
|---|---|---|
| Style | static-singleton monolith | DI monolith, event-driven |
| Concurrency | async sockets + one global tick thread; mutable shared dictionaries | Netty event loops; one scheduled loop **per map**; single-writer via `WorldAction` queue |
| World state | OOP objects in `ConcurrentDictionary`s | Artemis-ODB ECS (entities/components/systems) |
| Spatial queries | O(n²) distance scan each tick per map | interval system + per-player `View`/`VisibleBy` diffs |
| Protocol versions | one codebase per client version | pluggable `Version` (`v4`, `v10`) selected at runtime |
| Outbound traffic | `player.SendPacket/SendCommand` direct socket writes | publish `SendCommand`/`BroadcastCommand` events → `ChannelManager` writes |
| Persistence | synchronous SQL in game paths, JSON columns | JPA, normalized schema, pre-computed combat stats |
| Chat | in-process second listener + room model | absent |
| Admin/CMS ops | TCP 4301 JSON bridge | REST in CMS module |
| Ops | `emulator-helper` watchdog | JavaFX Launcher dashboard |

Key lesson carried into the Go design: **Darkobit defines the wire protocol and
feature scope; Kalaazu defines the internal quality bar** (ECS-style data flow,
event-driven outbound pipeline, versioned codecs, per-map loops with a
single-writer command queue).
