# Connecting a client

The process speaks the same bootstrap the original DarkOrbit client uses, then the v10 game socket. A browser (or Ruffle) loads `preloader.swf`. That movie loads `main.swf`. `main.swf` opens the game TCP socket. Chat is a second TCP socket.

Place the two movies in the client directory (`CLIENT_ROOT`, default `client/`):

```
client/preloader.swf    served at /spacemap/preloader.swf
client/main.swf         served at /spacemap/main.swf
```

Anything else in that directory is served under `/spacemap/` as well (`xml/resources.xml`, graphics, templates). The server does not ship those files. `?__cv=` query strings are ignored.

## 1. Session

`POST /login` with `{"username","password"}` returns `{"token","userId"}`. The seed account is `pilot` / `pilot`.

`POST /session` with `Authorization: Bearer <token>` returns `{"sessionId","userId","mapId"}` and sets a `dosid` cookie to that session id. The cookie is what the map page and `flashAPI/loadingScreen.php` read. It is the same string the game socket expects as `sessionID`.

Opening `http://<host>:8088/` does those two calls and then navigates to the map page.

## 2. Map page

`GET /indexInternal.es?action=internalMapRevolution` with the `dosid` cookie returns HTML that calls `flashembed`, the same shape as the original map page:

```javascript
flashembed("container", {
  "src": "http://<host>:8088/spacemap/preloader.swf",
  "width": "100%",
  "height": "100%"
}, {
  "userID": "1",
  "sessionID": "<dosid>",
  "factionID": "1",
  "pid": "1",
  "mapID": "1",
  "cdn": "http://<host>:8088/",
  "host": "http://<host>:8088/",
  "dynamicHost": "http://<host>:8088/",
  "chatHost": "<hostname>",
  "basePath": "spacemap",
  "gameclientPath": "spacemap",
  "gameclientHash": "xenomit",
  "display2d": "2",
  "autoStartEnabled": "1"
});
```

`POST /flashAPI/loadingScreen.php` answers the same identity as JSON (`userID`, `sessionID`, `factionID`, `mapID`, `lang`). The original page posts `action=setClientBrowserConfig` and ignores the body; either source is enough.

The preloader then requests:

```
{cdn}{gameclientPath}/main.swf?__cv={gameclientHash}
```

which is `http://<host>:8088/spacemap/main.swf?__cv=xenomit`.

## 3. Which game socket

`GET /spacemap/xml/maps.php` is the map list. Every map points at this process:

```xml
<maps>
  <map id="1" name="1-1"><gameserverIP>127.0.0.1:8080</gameserverIP></map>
</maps>
```

The host is the host you used to open the page. The port is the game listener (`GAME_ADDR`, default 8080). Set `GAME_PUBLIC_ADDR` when that guess is wrong, for example `127.0.0.1:8080`.

`GET /crossdomain.xml` is the Flash socket policy. The game socket also answers `<policy-file-request/>` in-band, which is the path the movie uses.

## 4. Game socket

Connect TCP to `gameserverIP`. If the first bytes are the policy request, write it and keep the socket:

```
<policy-file-request/>
```

The reply is the cross-domain XML plus a trailing NUL. The next bytes are game frames.

A frame is big-endian:

```
uint16 length   // byte length of the command id plus the payload
uint16 command  // command id
bytes  payload
```

`length` does not include itself. Nested modules inside a payload are `id + payload` with no extra length. Strings are `uint16` byte length plus UTF-8. Integers are big-endian.

Field values are rotated with `Mix(v, left, right) = (v<<left) | (v>>right)` on the 32-bit word. The client writes the inverse of the rotation the server reads.

Login is command **10996**:

| Field | Wire |
|---|---|
| instanceId | int32, client writes `Mix(v, 24, 8)` |
| userId | int32, client writes `Mix(v, 24, 8)` |
| factionId | int16, raw |
| sessionId | UTF, the `dosid` / `sessionID` |
| version | UTF, any string |

`instanceId` is the `pid` flashvar (`1`). `userId` is the account id. After a valid login the server sends ship initialization, command **7511**, then legacy strings wrapped in command **4224**.

Movement request is command **17063**. The server broadcasts movement as command **29819**. Other commands the server handles include ship select **18091**, collect box **6532**, laser **31106**, abort **1528**, rocket **22801**, portal jump **9253**, and logout cancel **10343**.

Legacy strings such as `0|A|STD|...` travel inside command 4224 as a single UTF field.

## 5. Chat socket

Connect TCP to `chatHost` port **9338** (`CHAT_ADDR`). Messages end with `#`. Fields are separated by `%`. `@` is treated as `%`.

Login, with the same ids as the game socket:

```
bu%<userId>%<sessionId>#
```

The server replies `bv%<userId>#`, then `by%` plus the room list (`Id|Name|Tab|Company|0|0` entries), then a `dq%` system line. Room chat is `a%<roomId>%<text>#`. Join a room with `bz%<roomId>#`.

## 6. Run it

On the host, from the repo:

```
go run ./cmd/xenomit
```

Then open `http://127.0.0.1:8088/`. Logs go to stdout.

In Docker, from the repo, after copying the two SWFs into `client/`:

```
docker compose -f deploy/docker-compose.yml up --build
docker compose -f deploy/docker-compose.yml logs -f xenomit
```

The launcher is `http://localhost:8088/`. `GET /healthz` returns `ok`. `GET /metrics` is Prometheus text. Admin calls are documented by `GET /admin/help` (the seed pilot is an admin, or send `X-Admin-Token` when `ADMIN_TOKEN` is set).

Postgres, Redis, NATS, and Kafka are not required. `docker compose -f deploy/docker-compose.yml --profile infra up` starts them; the process still keeps its own memory bus, log, and accounts.
