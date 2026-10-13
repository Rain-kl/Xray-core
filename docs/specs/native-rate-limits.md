# Native authenticated-user rate limits

## Goal

Allow native Xray inbound users to have aggregate upload and download caps, with an optional shared download cap for a configured group. Limits apply to authenticated users independently of source IP, NAT, or address family.

## Configuration and units

`common.protocol.User` carries `uplink_speed_limit`, `downlink_speed_limit`, `rate_limit_group`, and `downlink_group_speed_limit`. Speed values are unsigned bytes per second; zero means unlimited. The group name is an opaque shared-bucket identity. Panel-generated identities are scoped to an inbound, so matching users in different inbounds do not share a bucket.

Individual user buckets are shared by concurrent sessions for the same user and direction. Group buckets are shared by users with the same group identity and group download cap. Buckets account application payload bytes at the proxy; they do not count transport overhead or depend on the peer IP.

Native configuration carries these fields on authenticated users. Supported adapters are VLESS, VMess, Trojan, multi-user Shadowsocks, Hysteria, and MASQUE. Anonymous SOCKS/HTTP and protocols without an authenticated user mapping cannot accept these limits. Shadowsocks 2022 single-user and relay forms must reject nonzero rate-limit settings instead of silently dropping them.

## Runtime and lifecycle

Rate-limit state is shared across the user's sessions and is canceled promptly when a waiting transfer is interrupted. Limited transfers wait in bounded chunks (at most 64 KiB) and avoid splice fast paths that would bypass accounting. Limiter registry references follow active users and are released when user/group settings are updated or removed.

The cap is enforced in the proxy data path, so limited sessions do not use Vision's splice/zero-copy fast path. This trades some throughput efficiency for correct accounting. Panel changes take effect for existing sessions by restarting them; the core's limiter applies to newly configured users through its normal user lifecycle.

## Compatibility and verification

The custom core retains the upstream byte-version `26.10.10` and uses `-feat.2` as the default feature suffix. Release tags of the form `v26.10.10-feat.*` stamp their suffix into the build. The panel requires `v26.10.10-feat.2` or newer for native limits.

This implementation is based on upstream [PR #6050](https://github.com/XTLS/Xray-core/pull/6050), original commit `da56e92`, cherry-picked as `4e99a70`, with cancellation, bounded-wait, splice, and registry-lifecycle corrections.

Relevant tests include `TestRateLimiterWaitLargerThanBurst`, `TestRateLimiterWaitCancellation`, `TestLimiterRegistryAcquireSharesAndReleasesReferences`, `TestMemoryUserGetLimiterMethodsUseContextAndGroups`, `TestRateLimitedWriterCloseCancelsWaitAndClosesWriter`, `TestRateLimitedReaderInterruptCancelsWaitAndForwards`, `TestRateLimitConcurrentSameUserAggregateDownlink`, `TestRateLimitDistinctUsersAtSameIPHaveIndependentCaps`, and `TestRateLimitGroupSharesDownlinkCap`.
