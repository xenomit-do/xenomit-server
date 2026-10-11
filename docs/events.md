# Event Handling

"Events" means three different things in these codebases, all documented here:

1. **The periodic simulation tick** that drives all time-based game logic.
2. **Scheduled in-game events** (Spaceball, Jackpot Battle, UBA, duels).
3. **The internal event/notification mechanisms** used to decouple subsystems
   (C# `event`s, and Kalaazu's Spring `ApplicationEvent` bus + `WorldAction`
   queue).

---

## 1. Darkobit 10.0 (C#)

### 1.1 The tick system — `Game/Ticks/`

```csharp
interface Tick { void Tick(); }

class TickManager {
    public static short TICKS_PER_SECOND = 84;          // actually used as ms
    public List<Tick> Ticks;

    public async void Tick() {
        while (true) {
            for (i...) Ticks[i].Tick();
            await Task.Delay(TICKS_PER_SECOND);         // ~12 ticks/sec
        }
    }
}
```

A single global loop on one async task. **Every ticking object self-registers**
in its constructor (`Program.TickManager.AddTick(this)`) and unregisters on
death/disconnect. Registered tickables:

| Tickable | What its Tick() does |
|---|---|
| `Spacemap` (per map) | `CharacterTicker()` — O(n²) in-range recomputation, per-player object visibility diff (≤1250 u), mine proximity detonation, radiation + secure-zone checks |
| `GameSession` | inactivity kick (>3 min idle, not in combat); executes pending `SOCKET_CLOSED` grace disconnect (2 min) |
| `Player` | position interpolation, combat upkeep, logout timer (5 s premium / 10 s), shield regen, pet, cooldowns, group/battle-station checks |
| `Npc` | `Movement.ActualPosition` interpolation, `NpcAI.TickAI()`, shield regen, `RefreshAttackers`, `Attack()` if attacking |
| `Pet` | follow/attack logic |
| `Duel`, `JackpotBattle`, `Uba`, `Objects.Spaceball` | event state machines (below) |

There is no fixed timestep, no delta-time passed into `Tick()` — every object
compares `DateTime.Now` against stored timestamps (`lastAttackTime`,
`MovementStartTime`, …). Timing precision therefore depends on wall-clock math,
not tick counts.

### 1.2 C# domain events

A light `EventHandler<CharacterArgs>` pair exists on `Spacemap`
(`CharacterAdded`/`CharacterRemoved`) and on `Character`
(`InRangeCharacterAdded`/`InRangeCharacterRemoved`). These are synchronous
in-process notifications — e.g. removing a character triggers each remaining
character's `RemoveInRangeCharacter`, which in turn emits `ShipRemoveCommand`
to observing players. Beyond these, "events" are just direct method calls on
shared state.

### 1.3 Scheduled in-game events — `Game/Events/` + `EventManager`

`Managers/EventManager.cs` is a static holder for singleton event objects,
initialized at boot (`InitiateEvents()`):

```csharp
public static JackpotBattle JackpotBattle;
public static Spaceball Spaceball;
public static UltimateBattleArena UltimateBattleArena;
```

Events are **started by admin chat commands** (`/start_spaceball`,
`/start_jpb`, …) or console input — there is no scheduler/calendar.

#### Spaceball (`Game/Events/Spaceball.cs`)

- `Start()`: broadcast `0|A|STD|Spaceball event started!`, spawn the ball
  `Character` on its map, create 3 faction-colored portals, refresh every
  player's menu bar, `AddTick(Character)`.
- The ball is a `Character` (`Objects/Spaceball.cs`) — it ticks, gets carried
  between faction portals, and the scoreboard is fed through legacy
  `0|n|ssi|mmo|eic|vru` packets (also sent on login, see `SendPlayer`).
- `Stop()`: broadcast end message, remove portals/ball, reset score limit.

#### JackpotBattle (`Game/Events/JackpotBattle.cs`)

- `Start()`: 120-second countdown via `SendPacketToAll` each second, then mass-
  `Jump()` every eligible player onto map 42 at faction spawn points (cloak
  disabled, HP/shield topped up); registers itself on the tick loop.
- `Tick()`: `CheckRadiation()` shrinks a `RADIATION` `POI` circle on a schedule
  (100 units/step, slowing at ring milestones), tracks finalists, and calls
  `SendRewardAndStop` when one player remains.
- Reward: +25k uridium / +2.5M EP / +25k honor, winner title swap, row insert
  into `log_event_jpb`, then teleport the winner home and reset state.

#### Duel (`Game/Events/Duel.cs`)

- Created out of the **chat** flow: `/duel name` creates a private chat-room
  invite; joining it calls `new Duel(players)` when both are in an equip zone.
- Constructor teleports both players to map 101 positions, applies the CAMERA
  visual modifier, disables cloak; `Start()` broadcasts a 25-second peace
  countdown (`jp_no_attack_n_seconds`) then arms the tick loop.
- `Tick()` ends the duel when one player remains → `SendRewardAndStop` removes
  the loser's mines, clears camera modifier, jumps winner home.
- `Duel.InDuel(player)` is consulted all over the engine (range checks, mine
  damage scoping) — event state leaks into core logic via `player.Storage.Duel`.

#### UltimateBattleArena (`Game/Events/UltimateBattleArena.cs`)

- `UltimateBattleArena` is a persistent matchmaking **queue**: its `Tick()`
  pairs up `WaitingPlayers` (≥2) into `Uba` instances.
- `Uba` is the match: page-3 countdown UI (`UbaWindowInitializationCommand` with
  `Uba047Module` timers), peace phase, POI removal — largely stubbed/commented.
- Client entry: `UbaMatchmakingRequest/Accept/Cancel` handlers (packet ids
  11301/65/659).

### 1.4 Related tick-driven subsystems

- **Movement** is dead-reckoning: `Movement.Move` computes `MovementTime =
  distance/speed*1000`, stamps `MovementStartTime`, and broadcasts
  `MoveCommand(id, x, y, timeToTarget)`. Positions are derived lazily via
  `ActualPosition` whenever needed — there is no per-tick position integration.
- **NPC AI** (`Objects/AI/NpcAI.cs`) is a 3-state FSM evaluated once per second
  inside `Npc.Tick`: `SEARCH_FOR_ENEMIES` (scan `InRangeCharacters`, wander to a
  random point) → `FLY_TO_ENEMY` (move to a random point on a 300-radius circle
  around the target) → `WAIT_PLAYER_MOVE` (re-approach when the target moves).
  Aggression is gated by `Ship.Aggressive`, demilitarized zones, cloak, range.
- **Attacker bookkeeping**: `RefreshAttackers()` drops attackers idle >10 s and
  toggles the LSH/USH "fade to gray" packets vs the `MainAttacker`.

---

## 2. Kalaazu (Java / Spring Boot)

Kalaazu replaces all of the above with two explicit mechanisms.

### 2.1 Application event bus (Spring `ApplicationEvent`)

Cross-component communication is publish/subscribe over the Spring context —
this is how the whole "send a packet" axis works:

```
game code ──publish──▶ ApplicationContext
                          │
        ┌─────────────────┼──────────────────────┐
        ▼                 ▼                      ▼
  ChannelManager    Server lifecycle        any other @EventListener
  sends packets     (StartServer → map init + listeners
                    Netty bind)              (audit, chat relay, …)
```

Event classes (`server/event/`): `SendCommand`, `SendCommands`,
`SendCommandToSessions`, `SendCommandsToSessions`, `BroadcastCommand`,
`BroadcastCommands`, `EndGameSession`, `EndGameSessionIf`,
`GameSessionStarted`, `GameSessionStopped`; plus app-level `StartServer`,
`StopServer`, `ServerStarted`, `ServerStopped` (published by the JavaFX
`Launcher` UI).

Consequences:

- **Outbound packets are events**, so the ECS systems stay transport-agnostic
  (`publisher.publishEvent(new SendCommands(session, commands))`).
- Serialization is centralized once per command in `ChannelManager`.
- `@Async` handlers keep net I/O off Netty event loops.

### 2.2 World-mutation queue (`WorldAction`)

Second mechanism: the per-map `GameLoop` owns a
`ConcurrentLinkedQueue<WorldAction>` drained at the top of each tick — the
single-writer pattern that makes Artemis `World` mutation thread-safe while
Netty/`@Async` handlers run concurrently:

```java
gameLoopService.addAction(mapId, world -> {
    world.edit(entityId).create(MovementIntentComponent.class)
         .setDestination(dest);
});
```

So an inbound packet's path is:

```
Netty event loop → ChannelManager.processPacket → Handler.handle (@Async)
   → publish/addAction(...)                (cross-thread boundary)
        → next GameLoop.run() drains queue → world.process()
             → systems observe component changes
             → systems publish SendCommand events
                  → ChannelManager writes to channels
```

### 2.3 The game loop itself

`GameLoopService.initializeMap` schedules each map's `GameLoop` at
`config.game.tickRate` (≈50 ms / 20 Hz). Each `run()`:

1. drains `actions` (the `WorldAction` queue),
2. computes `deltaTime` from `System.nanoTime`,
3. `world.setDelta(dt); world.process()` → all Artemis systems run in order.

Unlike Darkobit's wall-clock `DateTime.Now` diffs, Kalaazu systems consume the
world's delta — deterministic, pausable, and testable.

### 2.4 Interval and conditional systems

- `VisibilitySystem` extends `IntervalIteratingSystem` (0.25 s) — visibility is
  deliberately slower than the sim tick.
- `NpcAiSystem` "freezes" NPCs no player can see (skips AI work entirely) — the
  ECS answer to Darkobit's every-NPC-every-tick cost.
- Targeting/visibility coupling: `TargetComponent`/`TargetedByComponent` keep
  combat-relevant entities visible across distances, mirroring Darkobit's
  "don't despawn my selected target" rule.

### 2.5 Scheduled events

Not implemented yet — the design intent is that JPB/UBA-style flows become
either dedicated ECS systems operating on a per-event map/world, or scheduled
`WorldAction`s; the `endGameSession`/broadcast events already provide the
notification plumbing.

---

## 3. Comparison

| Aspect | Darkobit 10.0 | Kalaazu |
|---|---|---|
| Loop | one global `Task.Delay(84)` loop, `Tick` interface | one `ScheduledExecutor` task **per map**, Artemis `world.process()` |
| Time base | `DateTime.Now` diffs everywhere | `world.getDelta()` nanoTime delta |
| World mutation | uncontrolled — any thread mutates dictionaries | `WorldAction` queue, single writer per map |
| Outbound | direct `Player.SendX` calls | `ApplicationEvent`s → `ChannelManager` |
| Game events | static singletons driven by tick + admin commands | not built; designed to slot into systems/actions |
| NPC AI | per-NPC FSM in `Npc.Tick`, always on | `NpcAiSystem` + `NpcAiComponent`, frozen when unobserved |
| Visibility | O(n²) per map tick, dictionary of in-range sets | interval system + `View`/`VisibleBy` bags, diffed |
