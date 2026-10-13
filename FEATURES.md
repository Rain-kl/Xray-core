# Product features

## Native authenticated-user rate limits (native-rate-limits)

- **Background:** The panel needs aggregate per-user upload/download caps and an optional shared download cap that remain correct across concurrent connections, NAT, and IPv4/IPv6.
- **Change:** Adds byte-per-second user limits and a named shared group-downlink bucket to native authenticated user configuration. Panel group identity is inbound-scoped. Limited transfers bypass splice fast paths; current sessions restart when the panel changes limits. Unsupported anonymous and non-user configurations must reject supplied limits.
- **Files:** See the implementation and support details in [FEATURE.md](FEATURE.md), and the behavior contract in [the specification](docs/specs/native-rate-limits.md). Core implementation spans `common/protocol`, `infra/conf`, `app/dispatcher`, and protocol adapters; the panel integration is maintained in Rain-kl/3x-ui.
- **Upstream fork markers:** Product code uses `FORK(native-rate-limits)` / `FORK-BEGIN(native-rate-limits)` markers where upstream files are modified.
- **Provenance:** Upstream [PR #6050](https://github.com/XTLS/Xray-core/pull/6050), original commit `da56e92`, cherry-picked as `4e99a70`; fork corrections and verification are documented in the [Agent Note](.agents/notes/native-rate-limits.md).
