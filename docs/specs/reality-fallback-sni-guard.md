# REALITY Fallback SNI & ECH Guard

## Background

When a REALITY inbound uses a multi-tenant CDN or reverse proxy (such as Cloudflare) as `dest`/`target`, the upstream edge server routes connections based on the TLS ClientHello SNI rather than the server IP alone. In upstream `xtls/reality`, `Server()` dials `config.Dest` immediately upon accepting a TCP connection and mirrors incoming client bytes before verifying `serverNames`. An attacker can send an arbitrary SNI (or forge an allowed outer SNI with an encrypted inner SNI via TLS ECH) to tunnel unauthenticated traffic through the server to arbitrary domains hosted behind the same CDN.

## Rules & State Ownership

All enforcement lives in `Xray-core/third_party/reality` with no new panel or core configuration fields:

1. **Buffer ClientHello before dialing `dest`**:
   - `Server()` reads and records the client's first flight (`ClientHello`) via a bounded recorder wrapper (`maxRecordedClientHelloBytes = maxHandshake + 4096`) before calling `config.DialContext`.
   - Non-TLS or malformed ClientHello packets, or payloads exceeding `maxRecordedClientHelloBytes`, cause `conn` to close silently without dialing `config.Dest` and without sending a TLS alert.
2. **SNI matching**:
   - `serverNames` matching is case-insensitive (`strings.EqualFold`) with no trailing-dot normalization.
   - If the SNI is non-empty and does not match any non-empty entry in `config.ServerNames`, `Server()` sends a fatal TLS `unrecognized_name` (112) alert, closes `conn`, and never dials `config.Dest`.
   - If the SNI is empty (`""`) and `config.ServerNames[""]` is not configured, `Server()` sends a fatal TLS `handshake_failure` (40) alert, closes `conn`, and never dials `config.Dest`.
   - When `config.ServerNames[""]` is configured, an empty SNI may proceed strictly into REALITY client authentication; it must never be relayed on unauthenticated fallback.
3. **Authenticate REALITY before rejecting ECH**:
   - ClientHello messages matching the SNI gate undergo REALITY authentication first (preserving classical X25519-only and `X25519MLKEM768` keyShare support).
   - Authenticated REALITY clients are permitted even if they carry GREASE ECH (`0xfe0d`) extensions (e.g., Chrome uTLS fingerprints).
   - Unauthenticated fallback connections (`hs.c.conn != conn`) are rejected if either SNI is empty or the ClientHello includes any `encrypted_client_hello` extension (`len(hs.clientHello.encryptedClientHello) > 0`). On rejection, `Server()` sends a fatal TLS `handshake_failure` (40) alert, closes `conn`, and never dials `config.Dest`.
4. **Deferred dial & replay**:
   - Only after passing either REALITY authentication or the unauthenticated fallback gate does `Server()` dial `config.Dest`, optionally emit the PROXY protocol header (`xver` 1 or 2), and replay the buffered ClientHello bytes to `target`.
   - Authenticated handshakes look up `GlobalPostHandshakeRecordsLens` and `GlobalMaxCSSMsgCount` using the canonical matched `serverNames` key from `config.ServerNames` so mixed-case client SNIs match the pre-probed cache key.

## Tradeoffs

- Because GREASE ECH and real ECH share the TLS extension wire format (`0xfe0d`) and the proxy lacks Cloudflare's private ECH key, blocking ECH on unauthenticated fallback also closes direct browser visits (such as Chrome) that send GREASE ECH to the camouflage address. Authenticated REALITY clients remain completely unaffected.

## Acceptance Scenarios

1. Unauthenticated connection with disallowed SNI -> fatal `unrecognized_name` (112) alert, `dialCount == 0`, connection closed.
2. Unauthenticated connection with empty SNI -> fatal `handshake_failure` (40) alert, `dialCount == 0`, connection closed.
3. Non-TLS payload (e.g., plain HTTP) -> closed with 0 response bytes, `dialCount == 0`.
4. Unauthenticated connection with allowed SNI and ECH (`0xfe0d`) extension -> fatal `handshake_failure` (40) alert, `dialCount == 0`, connection closed.
5. Unauthenticated connection with allowed SNI (exact or case-insensitive) and no ECH -> dials `dest` once, completes real TLS 1.3 fallback with `dest`.
6. Authenticated REALITY connection with GREASE ECH (`0xfe0d`), mixed-case SNI, and classical X25519-only share -> dials `dest` once, returns `(*Conn, nil)`, exchanges encrypted proxy data.
7. Oversized fragmented ClientHello exceeding `maxRecordedClientHelloBytes` -> closed, `dialCount == 0`.
