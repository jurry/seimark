# End-to-end verification against a real WHIP server

The Playwright loopback test proves `attach` and `reader` agree inside one
browser. This proves the rest of the claim: that a marker written by a browser
survives a real media server and a recording, and that the Go CLI reads it back.

Any WHIP server works. MediaMTX is used here because it implements WHIP as
specified, sends permissive CORS headers so the demo can be served from its own
origin, and records to MP4 without extra configuration.

## Set the server up

```sh
docker compose -f e2e/docker-compose.yml up -d
```

That is all `e2e/whip-verify.mjs` needs, and it does this itself when nothing is
already answering. `e2e/mediamtx.yml` is the configuration; recordings land in
`e2e/recordings/`, which is git-ignored.

To run MediaMTX without Docker, download it and use the same `e2e/mediamtx.yml`,
changing `recordPath` from `/recordings` to a directory you can write.

The WHIP endpoint is then `http://127.0.0.1:8889/seimark/whip`.

Two settings matter and are the usual reason another server fails here:

- **`webrtcAllowOrigin`**. The demo is served from its own port, so the WHIP POST
  is cross-origin. A server that sends no `Access-Control-Allow-Origin` blocks it
  in the browser with `TypeError: Failed to fetch`, no matter that `curl` works.
- **A reachable ICE candidate.** `webrtcAdditionalHosts: [127.0.0.1]` with the UDP
  port actually listening on the host. When the candidate is unreachable, ICE
  stays in `checking`, the encoder never starts, and the page shows
  `frames seen 0 / 0` with `errors 0` — the marker code is never called at all.

## Run the check

```sh
cd browser
npm run build
go build -o /tmp/seimark ../cmd/seimark
SEIMARK_CLI=/tmp/seimark node e2e/whip-verify.mjs
```

It publishes from the demo page for fifteen seconds, stops, finds the recording
MediaMTX just wrote, runs `seimark dump` over it, and fails unless every marker
survived: sequences continuous from zero, no duplicates, one stream id, and the
wall-clock span the markers carry within a second of the container's own
timeline.

A passing run:

```
page:        frames seen / marked 414 / 414 | sequence 413 | errors 0
markers:     417  seq 0..416
stream ids:  612872c7ad616c31
gaps:        0   duplicates: 0
wall clock:  14.95s   container: 14.95s   drift: 0 ms
PASS: every frame the browser stamped survived to the recording, in order.
```

Environment overrides: `WHIP_URL`, `RECORDINGS`, `PUBLISH_SECONDS`, `SEIMARK_CLI`.
