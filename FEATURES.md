# Product features

## Native authenticated-user rate limits (native-rate-limits)

- **Background:** The panel needs aggregate per-user upload/download caps and an optional shared download cap that remain correct across concurrent connections, NAT, and IPv4/IPv6.
- **Change:** Adds byte-per-second user limits and a named shared group-downlink bucket to native authenticated user configuration. Panel group identity is inbound-scoped. Limited transfers bypass splice fast paths; current sessions restart when the panel changes limits. Unsupported anonymous and non-user configurations must reject supplied limits.
- **Files:** See the implementation and support details in [FEATURE.md](FEATURE.md), and the behavior contract in [the specification](docs/specs/native-rate-limits.md). Core implementation spans `common/protocol`, `infra/conf`, `app/dispatcher`, and protocol adapters; the panel integration is maintained in Rain-kl/3x-ui.
- **Upstream fork markers:** Product code uses `FORK(native-rate-limits)` / `FORK-BEGIN(native-rate-limits)` markers where upstream files are modified.
- **Provenance:** Upstream [PR #6050](https://github.com/XTLS/Xray-core/pull/6050), original commit `da56e92`, cherry-picked as `4e99a70`; fork corrections and verification are documented in the [Agent Note](.agents/notes/native-rate-limits.md).

## Sniffed domains in audit (sniffed-audit-domains)

- Preserve original IP destinations and export observed HTTP/TLS/QUIC/FakeDNS domains separately for website audit, including route-only sniffing.
- Core integration: common/log/access.go and both app/dispatcher dispatch paths; panel: internal/audit/spool.go, audit ingestion and audit event display.
- Design: [specification](docs/specs/sniffed-audit-domains.md). Implementation decision: [Agent Note](.agents/notes/sniffed-audit-domains.md). Upstream markers: FORK(sniffed-audit-domains). Legacy logs remain compatible; no retroactive IP-to-domain inference.

## Managed mixed clients (mixed-clients)

- **Background:** A mixed port must share the panel's client identity, billing, native limits and permission lifecycle across HTTP and SOCKS5.
- **Change:** Adds a managed `clients` array with independent login and billing identity, a shared concurrent credential store and UserManager operations. Empty password-authenticated user lists deny access. Legacy accounts and standalone HTTP remain compatible. HTTP, SOCKS5 TCP and associated UDP carry the full user identity; successful audit records include that identity.
- **Compatibility:** Requires `v26.10.10-feat.4` and the corresponding panel update. The panel excludes mixed from automatic subscriptions and offers manual credentials on its subscription info page.
- **Files and markers:** `proxy/mixedauth`, `proxy/socks`, `proxy/http/server.go`, `infra/conf/socks.go`, `core/core.go`; upstream changes marked `FORK(mixed-clients)`.
- **Design and implementation:** [Specification](docs/specs/mixed-clients.md), [Agent Note](.agents/notes/mixed-clients.md).
