# The connect service

`matinee-connect` (`cmd/matinee-connect`, `internal/connect`) lets the apps
find a home server whose public address changed while they were away. It
only ever handles addresses. No media, API calls or credentials pass through
it, and there is no relay: an app always connects straight to the server.

The project runs one at `https://connect.matinee.yeix.io`. Servers use it
while remote access is on, unless `MATINEE_CONNECT_URL=off`.

## How a server uses it

- The server keeps an Ed25519 key in `<data>/remote/server.key`. Its id is
  the first 120 bits of the key's SHA-256 in lowercase base32 (24
  characters), so ids can't be guessed and only the key's holder can
  register under one.
- Every ten minutes while remote access is ready, it signs
  `{"time": <unix seconds>, "port": <router port>}` and sends
  `POST /v1/register` with `{"key", "payload", "sig"}` (base64). The service
  registers `https://<address the request came from>:<port>`, never an
  address it's told, and forgets it after 30 minutes without another
  registration. Port 0 takes the registration down.
- With a port forwarded by hand, `POST /v1/check {"port": 443}` returns the
  caller's public address and whether the service could open a TCP
  connection to it on that port. It only ever connects to the caller.

## How an app uses it

`GET /api/v1/server` on the home server includes `serverId` and `lookupURL`
once remote access works. When an app is away and the saved remote address
doesn't answer, it calls `GET <lookupURL>`:

```json
{ "id": "…", "urls": ["https://203.0.113.7:7443"], "updated": 1793145600 }
```

and tries each URL, checking (as always) that the server there is the one it
paired with. A 404 means the server hasn't registered in the last half hour.

## Running it

```sh
docker run -p 8080:8080 -e CONNECT_CLIENT_IP_HEADER=X-Real-IP ghcr.io/yeixio/matinee-connect:main
```

It keeps everything in memory, so one small replica is enough; after a
restart, servers are back within ten minutes. Behind a proxy, set
`CONNECT_CLIENT_IP_HEADER` to the header that carries the client's address,
and make sure the proxy sees the real one (on a cloud load balancer, that
usually means PROXY protocol or `externalTrafficPolicy: Local`). Requests
are limited per address: 30 registrations, 10 checks and 60 lookups a minute.
