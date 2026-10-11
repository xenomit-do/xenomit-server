# xenomit-server

Design and architecture documentation for a DarkOrbit-compatible game server,
reverse-engineered from two reference codebases:

| Source | Description |
|---|---|
| `C:\Users\Yagiz\Downloads\Darkobit-Files\Darkobit` | The "Darkobit" private-server stack: `darkorbit-emulators-master` (DarkOrbit 7.5.3 / 9.0 / 10.0 emulators), `darkorbit-cms-master` (PHP web portal + Flash assets + MySQL schema), `emulator-helper-master` (watchdog process) |
| [manulaiko/Kalaazu](https://github.com/manulaiko/Kalaazu) | "The next generation of DarkOrbit Private Servers" — a Spring Boot multi-module project with Netty networking and an Artemis-ODB ECS game core |

## Documentation

| Document | Contents |
|---|---|
| [docs/architecture.md](docs/architecture.md) | High-level architecture of the Darkobit stack and Kalaazu: modules, game loop, world model, persistence, administration paths |
| [docs/networking.md](docs/networking.md) | Wire protocols, ports, packet formats, dispatch, session lifecycle for both codebases |
| [docs/events.md](docs/events.md) | Event handling: tick loops, in-game events (Spaceball/JPB/UBA/Duel), Spring event bus, ECS WorldActions |
| [docs/go-microservice-plan.md](docs/go-microservice-plan.md) | Target design: a Go microservices architecture implementing a DarkOrbit-compatible game server (2026 SWE standards) |
| [docs/client.md](docs/client.md) | What a client implements to load preloader.swf, main.swf, and open the game and chat sockets |

## Quick reference

| Component | Darkobit 10.0 | Kalaazu |
|---|---|---|
| Language | C# (.NET Framework 4.8) | Java 25, Spring Boot 4, Gradle |
| Game port | TCP 8080 | TCP (config `port.server`) |
| Chat port | TCP 9338 | — (not implemented) |
| CMS bridge | TCP 4301 (JSON `{Action,Parameters}`) | REST in CMS module |
| Flash policy | inline in game socket | dedicated PolicyServer |
| Concurrency | APM sockets + global `TickManager` | Netty event loops + per-map ECS `GameLoop` @ 20 Hz |
| World model | OOP `Character`/`Npc`/`Player` + `Spacemap` | Artemis-ODB ECS, one `World` per map |
| Dispatch | `Dictionary<short, IHandler>` | `List<Handler>` filtered by version + `canHandle` |
| DB | MySQL, JSON blobs in columns | MariaDB, fully normalized (~80 tables), JPA |
| Events | `static EventManager` + `Tick` interface | Spring `ApplicationEvent` bus + `WorldAction` queue |
