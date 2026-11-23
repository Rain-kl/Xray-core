# REALITY fallback SNI & ECH guard

## Decision

Enforce strict pre-dial ClientHello inspection in `third_party/reality` with zero new Xray config knobs:

1. Read and record the incoming `ClientHello` in a bounded wrapper (`clientHelloRecorderConn`, capped at `maxHandshake + 4096`) before calling `config.DialContext`. This eliminates premature dials to multi-tenant CDNs, `MirrorConn` leaks, and the `copying` race between C2S and S2C goroutines.
2. Match SNI case-insensitively (`strings.EqualFold`) against `config.ServerNames` without stripping trailing dots, returning the canonical configured key so `GlobalPostHandshakeRecordsLens` and `GlobalMaxCSSMsgCount` cache lookups still match when a client sends mixed-case SNI.
3. Run REALITY client authentication before rejecting `encrypted_client_hello` (`0xfe0d`) extensions. Authenticated REALITY clients (including Chrome uTLS fingerprints that emit GREASE ECH) succeed normally; unauthenticated fallback connections carrying any ECH extension or an empty SNI are rejected with fatal TLS `handshake_failure` (40) before dialing `dest`. Disallowed non-empty SNIs receive fatal `unrecognized_name` (112), while non-TLS or oversized inputs close silently with zero outbound dial.

## Tradeoffs

- Because GREASE ECH and real ECH share wire extension `0xfe0d` and the REALITY server has no CDN ECH private key, rejecting ECH on unauthenticated fallback prevents Cloudflare ECH inner-SNI tunneling at the cost of closing direct unauthenticated browser visits (e.g. Chrome) that emit GREASE ECH to the camouflage port.

## Verification

- `cd Xray-core/third_party/reality && go test -v -race ./...` covers disallowed SNI, empty SNI (with and without `""` in `serverNames`), plain HTTP non-TLS, unauthenticated ECH and Chrome GREASE ECH rejection, case-insensitive allowed fallback without ECH, authenticated REALITY with Chrome GREASE ECH + mixed-case SNI + classical X25519-only, and oversized fragmented ClientHello rejection.
- `cd Xray-core && go test ./transport/internet/reality/... ./transport/internet/tcp/... ./transport/internet/grpc/... ./transport/internet/splithttp/...` and `go build ./main` confirm integration with Xray-core transports and binary build.
