# The connect service

`matinee-connect` (`cmd/matinee-connect`, `internal/connect`) lets the apps
find a home server whose public address changed while they were away. It
only ever handles addresses. No media, API calls or credentials pass through
it, and there is no relay: an app always connects straight to the server.

Yeix runs one at `https://connect.matinee.yeix.io`. Servers use it while
remote access is on, unless `MATINEE_CONNECT_URL=off`. Yeix's is a paid,
prepaid subscription per server (Matinee Connect); a service run without a
Stripe key is open to every server.

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
- With a port forwarded by hand, a signed `POST /v1/check` with
  `{"time", "port": 443}` returns the caller's public address and whether
  the service could open a TCP connection to it on that port. It only ever
  connects to the caller.
- On a paid service, registrations and checks also carry `"pass"`, and are
  turned away with 402 without a good one.

## The subscription

1. An admin picks a plan in Settings. The server asks the billing site
   (`MATINEE_BILLING_URL`, `https://yeix.io` with the default connect
   service) with `POST /api/matinee/connect/checkout {"server", "plan"}`,
   which opens a Stripe Checkout in subscription mode with metadata
   `product=matinee-connect` and `server=<id>`, and returns
   `{"url", "session_id", "expires"}`. `GET /api/matinee/connect/plans`
   lists `{"plans": [{"id", "price", "interval"}]}`.
2. The server keeps the session id and signs
   `POST /v1/subscription {"time", "session"}` every few seconds until it's
   paid. The service reads the session from Stripe (a restricted key that
   reads Checkout Sessions and Subscriptions), checks the metadata names
   this server, and returns `{"pass", "until"}`: an HMAC over the server id
   and the end of the paid period plus three days. A subscription past due
   only gets the three days from the start of its unpaid period.
3. Four days before `until`, the server trades the same session again for
   the next pass. 410 means the subscription ended.
4. **Manage** calls `POST /api/matinee/connect/portal {"checkout_session"}`
   for a Stripe customer portal link.

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

To charge for it, set `CONNECT_REQUIRE_SUBSCRIPTION=true`,
`CONNECT_STRIPE_KEY` (restricted, read-only) and `CONNECT_PASS_KEY` (32 or
more random characters, kept the same across restarts so passes stay good).
It refuses to start with the first and without the others.

It keeps everything in memory, so one small replica is enough; after a
restart, servers are back within ten minutes. Behind a proxy, set
`CONNECT_CLIENT_IP_HEADER` to the header that carries the client's address,
and make sure the proxy sees the real one (on a cloud load balancer, that
usually means PROXY protocol or `externalTrafficPolicy: Local`). Requests
are limited per address: 30 registrations, 10 checks, 20 subscription
requests and 60 lookups a minute.
