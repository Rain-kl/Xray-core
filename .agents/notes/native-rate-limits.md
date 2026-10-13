# Native authenticated-user rate limits

## Decision

Carry limits on `common.protocol.User`, the existing authenticated-user boundary. Values are bytes per second; zero disables that direction. Per-user buckets aggregate concurrent sessions. A group key identifies a shared downlink bucket, and the panel scopes keys to an inbound to prevent accidental cross-inbound sharing.

The upstream PR #6050 implementation was cherry-picked from `da56e92` as `4e99a70`. The fork adds prompt cancellation, bounded waits, splice bypass for limited traffic, and registry reference cleanup. This keeps the common user model authoritative and prevents optimized paths from evading accounting.

## Operational tradeoff

Limits account application payload bytes independent of IP/NAT, but limited transfers cannot use Vision's splice/zero-copy fast path. Panel edits restart current sessions so the new settings apply immediately. Minimum panel-compatible core is `v26.10.10-feat.2`; release tag suffixes are stamped while preserving the byte version.

## Verification

Core behavior is covered by limiter cancellation/sharing tests in `common/protocol/ratelimit_test.go`, transfer interruption tests in `app/dispatcher/rate_limit_test.go`, and concurrent user/group traffic tests in `testing/trafficlog/rate_limit_test.go`. See [the feature specification](../../docs/specs/native-rate-limits.md) for test names and support boundaries.

## Synchronization and release

Merged fork main through `83b3bd3` without rewriting history; the concurrent changes only added guidelines and a custom release workflow. Native edits have fork markers; `user.pb.go` is generated from the marked proto source and is not hand-annotated. `release-feat.yml` is the sole publisher in Rain-kl/Xray-core, so overlapping upstream/custom jobs cannot race over the same assets. Tag builds stamp their actual feat suffix. The Go-only core has no pnpm architecture/removal commands; verification uses the documented Go packages and fork-marker inspection.

Repeated traffic-log validation exposed an existing abnormal-close fixture race: local socket writes do not guarantee completed proxy forwarding. The fixture now waits for target receipt before intentional reset, preserving its exact upload assertion. Ten repeated runs and the package passed.
