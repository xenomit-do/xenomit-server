# Network Handling

How bytes move in the two reference implementations: listeners, wire formats,
packet framing, dispatch, and session lifecycle.

---

## 1. Darkobit (DarkOrbit 10.0, C#)

### 1.1 Listeners

Three raw `System.Net.Sockets` listeners on the old APM pattern
(`BeginAccept`/`BeginReceive` + `ManualResetEvent`):

| Listener | Port | Clients | Protocol |
|---|---|---|---|
| `Net/GameServer.cs` → `GameClient` | **8080** | Flash game client (`spacemap/main.swf`) | binary packets + embedded legacy strings |
| `Net/ChatServer.cs` → `Chat/ChatClient` | **9338** | Flash chat widget | `% @ | } #` delimited text |
| `Net/SocketServer.cs` | **4301** | PHP CMS (`Socket.php`) | one-shot JSON `{Action, Parameters}` |

`GameServer.AcceptCallback` just wraps the accepted socket in `new GameClient(handler)`,
which starts an async read loop with a 1 KB buffer.

### 1.2 Flash policy handshake

Before anything else, `GameClient.ReadCallback` sniffs for the Flash
cross-domain request and answers inline:

```
<policy-file-request/>  →
<?xml version="1.0"?>
<cross-domain-policy>
  <allow-access-from domain="*" to-ports="*" />
</cross-domain-policy> + '\0'
```

### 1.3 Binary packet format ("netty" namespace)

Framing (`Utils/Bytes.cs`):

```
outbound:  [2-byte payload length][2-byte command id][payload]
inbound:   [2-byte length][2-byte command id][payload]    (ByteParser skips len, reads ID)
```

All integers are **big-endian** (`readInt`/`readShort` shift bytes
`<<24|<<16|<<8`). `writeUTF`/`readUTF` = 2-byte length + UTF-8 bytes; booleans
1 byte; doubles/longs 8 bytes.

**Field obfuscation**: the client scrambles every field with a per-field bit
rotation, which the emulator must reproduce exactly:

```csharp
// MoveRequest.readCommand — note different rotation per field
targetY   = readInt(); targetY   = (targetY   << 8 ) | (targetY   >>> 24);
positionX = readInt(); positionX = (positionX << 15) | (positionX >>> 17);
readShort();
targetX   = readInt(); targetX   = (targetX   >> 4 ) | (targetX   << 28);
positionY = readInt(); positionY = (positionY << 5 ) | (positionY >>> 27);
```

```csharp
// MoveCommand.write — the matching write-side rotations
param1.writeShort(6455);
param1.writeInt(y >> 13 | y << 19);
param1.writeInt(userId >> 12 | userId << 20);
param1.writeInt(x >> 4 | x << 28);
param1.writeInt(timeToTarget >> 13 | timeToTarget << 19);
```

This makes the codec table-driven in spirit but hand-written in practice: every
`requests/*.cs` has `const short ID` + `readCommand(byte[])`, every
`commands/*.cs` has `const short ID` + `static byte[] write(...)`. There are
~170 outbound command classes (plus `class_*`/obfuscated leftovers) and ~45
inbound request classes.

### 1.4 Two wire protocols on one socket

The v10-era client mixes two protocols:

1. **Binary commands** (described above).
2. **Legacy pipe protocol**: strings like `0|A|STD|message` or
   `0|S|CFG|1`, carried inside the binary wrapper command **4224**
   (`LegacyModule.write(string)` → `writeUTF`); inbound they arrive as
   `LegacyModuleRequest` whose `message` field is re-split on `|` and switched
   on (`LegacyModuleHandler`).

Server→client code therefore has two send paths on `Player`:
`SendCommand(byte[])` for typed commands and `SendPacket(string)` which wraps
legacy strings in `LegacyModule`. `ServerCommands.cs`/`ClientCommands.cs` hold
the ~300 three-letter mnemonics (`STD`, `LM`, `ps`, `PNG`, …) of the old
protocol.

### 1.5 Dispatch

`Net/netty/Handler.cs`:

```csharp
private static Dictionary<short, IHandler> Commands;
Handler.Execute(bytes, client):
    parser = new ByteParser(bytes);          // reads packet ID
    if (parser.ID == LoginRequest.ID /*10996*/):
        // auth path: sessionId validated against DB before any session exists
        if (QueryManager.CheckSessionId(read.userID, read.sessionID)
            && !QueryManager.Banned(read.userID))
            new LoginRequestHandler(client, read.userID);
        return;
    gameSession = GameManager.GetGameSession(client.UserId);
    if (gameSession == null) return;          // drop packets pre-login
    Commands[parser.ID]?.execute(gameSession, bytes);
    gameSession.LastActiveTime = now;
```

Everything after login is dictionary lookup → `IHandler.execute(session, bytes)`
→ the handler re-parses the request class and mutates `gameSession.Player`.

### 1.6 Login & session lifecycle

```
client connects (8080)
 → policy-file-request? answer + keep socket
 → LoginRequest { instanceId, userID, factionID, sessionID, version }
 → CheckSessionId(userID, sessionID)  // string written by PHP CMS at "play" click
 → LoginRequestHandler:
      if another GameSession exists → Disconnect(old)  (session takeover)
      Player = QueryManager.GetPlayer(userId)          // accounts+settings+equipment
      new GameSession(player){ Client = client }
      GameManager.GameSessions[player.Id] = session
      resolve map/position; TickManager.AddTick(player)
      SendSettings(player): keybindings, settings, menubars, slotbar, windows
      SendPlayer(player):   ShipInitialization, drones+formation, config, spaceball
                            scoreboard, speed, map objects, boosters, PET init,
                            UpdateStatus, beacon
      Spacemap.AddCharacter(player)
```

Disconnect (`GameSession.Disconnect`): saves settings + player info + boosters
to MySQL, clears range sets, notifies group, removes from tick manager and map,
closes socket, removes from `GameSessions`. `SOCKET_CLOSED` is a 2-minute grace
period so a reconnect can resume; `INACTIVITY` fires after 3 idle minutes when
not in combat.

### 1.7 Chat protocol (port 9338)

Text protocol, message `key%params#`, params split on `%`, `@`, `|`:

- `bu%…@userId@sessionId#` login (re-validated via `CheckSessionId`)
- server replies `bv%userId#`, then `by%<room list>#` (`Room.ToString()` =
  `Id|Name|TabOrder|CompanyId|0|0}`)
- `a%roomId@text#` room message; `bz%roomId#` join
- whisper `/w name msg` → `cv%from@msg#` / `cw%to@msg#`
- admin slash commands (`/kick`, `/ban`, `/start_spaceball`, `/jump`,
  `/give_booster`, …) gated by `Permissions` from `chat_permissions` table
- duel invites are private "rooms" created over chat (`cj%duelId@Duel@0@name#`),
  accepted via a `bz` join of that id → `Duel` event creation.

`ChatConstants.cs` lists ~190 single/double-letter command ids covering rooms,
whispers, moderation, badwords, logs, ban lists — a full stand-alone chat
product protocol.

### 1.8 CMS bridge (port 4301)

One request per TCP connection. PHP `Socket::Get/Send` writes a JSON object;
server replies with a bare scalar/string and closes:

```json
{"Action":"IsOnline","Parameters":{"UserId":123,"Return":false}}
```

Actions include presence queries, `ChangeShip`, `ChangeCompany`, `BuyItem`,
clan create/join/leave/diplomacy, skill tree ops, `BanUser`, `KickPlayer`. These
mutate the live in-memory player (e.g. re-`SELECT` equipment rows, rebuild drone
stats, push `0|LM|ST|…` currency log lines, send `ClanChangedCommand` to the
player and everyone in range).

### 1.9 Failure handling

Almost every socket path swallows exceptions into `Logger.Log("error_log", …)`.
`GameClient.ReadCallback` closes on `bytesRead == 0` or any exception. There is
no framing guard: a `BeginReceive` buffer of 1024 bytes is treated as a whole
packet — **no reassembly for TCP segmentation** — which works because Flash
packets are small.

---

## 2. Kalaazu (Java / Spring Boot / Netty)

### 2.1 Listeners

| Listener | Port | Protocol |
|---|---|---|
| `game.v10.GameServer` (or `v4`, per `config.game.version`) | `port.server` | length-prefixed binary packets |
| `game.PolicyServer` | `port.policy` | NUL-delimited Flash policy request/response |

Both are Netty `ServerBootstrap`s (`NioServerSocketChannel`, `NioIoHandler`,
boss+worker `MultiThreadIoEventLoopGroup`s, `SO_BACKLOG=128`, `SO_KEEPALIVE`).

v10 pipeline per channel:

```
LengthFieldPrepender(2)            // out: prepend length
LengthFieldBasedFrameDecoder(…,0,2)// in: 2-byte length framing
PacketSerializer                   // ByteToMessageCodec<Packet>
InboundHandler                     // SimpleChannelInboundHandler<Packet>, @Sharable
```

`PolicyServer` pipeline: `DelimiterBasedFrameDecoder(1024, nulDelimiter)` +
String codecs + a handler that replies with the cross-domain XML and closes.

### 2.2 Packet abstraction

`game.Packet` is an interface (`writeString/readString/writeInt/readInt/…`,
`reset()`, `jump(idx)`, `getBytes()`); `v10.Packet` implements it over
`DataInputStream`/`DataOutputStream` (big-endian, same wire format as Darkobit —
minus per-field rotations, which Kalaazu has not yet ported).
`Packet.empty()` is a static factory delegating to the running `GameServer`'s
`getEmptyPacket()` — the active version decides the concrete class.

### 2.3 Session & dispatch

- `GameSession` is a **`prototype`-scoped Spring bean**: a new one is fetched
  from the context per connection in `InboundHandler.channelRegistered`, stamped
  with the `ChannelId`, holds `AccountsEntity`/`AccountsShipsEntity`/
  `AccountsConfigurationsEntity`/`AccountsHangarsEntity`, `mapId`, `entityId`.
- `ChannelManager` keeps `ChannelGroup channels` + `Map<ChannelId,GameSession>`.

```
channelRead0(packet)
 → ChannelManager.processPacket(packet, channelId)
    session = sessions.get(channelId)          // or drop
    packetHandlers.stream()
        .filter(h -> h.getVersion() == config.game.version)
        .filter(h -> h.canHandle(packet))      // e.g. packet.readShort()==ID
        .findFirst()
        .ifPresentOrElse(h -> h.handle(packet, session),
                         () -> log("no handler"))
```

`Handler<T extends InCommand>` is an abstract class: `handle(packet, session)`
is `@Async` (Spring), reflectively instantiates `T`, calls `command.read(packet)`,
then the concrete `handle(T, session)`. Handlers are ordinary `@Component`s,
so the `List<Handler<?>>` is auto-wired — adding a handler is dropping a file in.

Login flow: `v10.LoginRequestHandler` → `SessionInitializationService.initialize
(userId, sessionId, session)` loads account/ship/config/hangar, sets
`session.mapId`, then `MapService.initializePlayer` creates the ECS entity and
pushes the entity-creation burst + `MAP_READY_HANDSHAKE` settings command.

### 2.4 Outbound path = Spring events

Nothing writes to sockets directly. Game code publishes `ApplicationEvent`s and
`ChannelManager` translates them:

| Event | Effect |
|---|---|
| `SendCommand(session, cmd)` | `channel.writeAndFlush(packet)` |
| `SendCommands(session, list)` | write all, one flush |
| `SendCommandToSessions(sessions, cmd)` | serialize once, write to each, `channels.flush()` |
| `SendCommandsToSessions(sessions, list)` | batch variant |
| `BroadcastCommand(s)` | `channels.writeAndFlush` to the whole group |
| `EndGameSession(session)` / `EndGameSessionIf(pred)` | kick/close |
| `GameSessionStarted` / `GameSessionStopped` | lifecycle notifications |

Commands serialize themselves into a `Packet` (`command.write(p)`), then the
`PacketSerializer` prepends the 2-byte length. Serialization happens **once per
command** even for fan-out — a deliberate efficiency point versus Darkobit,
which rebuilds each per-recipient call (but then again, Darkobit's writes are
idempotent `byte[]` anyway).

### 2.5 Lifecycle events

`channelUnregistered`/`exceptionCaught`("Connection reset") →
`channelManager.endGameSession(channelId)` → `session.destroy()` (prototype
bean cleanup, `entityId=-1`), remove from maps, publish `GameSessionStopped`
(world cleanup is left to systems listening for it).

### 2.6 v4 (legacy) implementation

`game/v4/` mirrors the same shape for the old string protocol: own `Packet`,
`PacketSerializer`, `GameServer`, `InCommand`/`OutCommand`, `LegacyPacket`
wrapping `0|`-strings, and handlers (`LoginRequestHandler`, `MoveHeroHandler`,
`SelectShipHandler`, `AdmiCliCommandHandler`, `SpacemapLoadedHandler`) plus
command builders emitting `ShipInitialization`, `CreateShip`, `ShipMovement`,
settings and tech commands — the same trick as Darkobit's `LegacyModule`,
structured as a first-class version instead of a wrapper packet.

---

## 3. Side-by-side summary

```
Darkobit 10.0                                   Kalaazu
─────────────────────────────────────────────  ────────────────────────────────────────────
Socket.BeginReceive, 1KB buffer,               Netty NIO, LengthFieldBasedFrameDecoder,
treats each read as one packet                 proper reassembly, ByteToMessageCodec
Dictionary<packetId, IHandler>                 List<Handler> + canHandle + version filter
IHandler.execute(session, bytes)               @Async handle(T extends InCommand, session)
Player.SendCommand(byte[]) / SendPacket(str)   ctx.publishEvent(SendCommand[...]) →
direct socket write                            ChannelManager → channel.write
Flash policy sniffed in game read loop         dedicated PolicyServer port
JSON bridge on :4301 for CMS                   CMS is pure REST + shared DB
sessions: GameManager.GameSessions             sessions: ConcurrentHashMap in
static ConcurrentDictionary                    ChannelManager + prototype GameSession
```

Key invariants a reimplementation must keep:

- 2-byte big-endian length prefix, then 2-byte command id.
- v10 field-level bit-rotation obfuscation (per command, hand-specified).
- Legacy `0|`-string protocol tunneled in command 4224 (`LegacyModule`).
- Flash cross-domain policy response before login.
- Session token = `sessionId` column in `player_accounts`, written by the CMS.
- Chat is a separate socket/protocol entirely (if in scope).
