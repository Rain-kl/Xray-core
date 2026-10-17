# Managed mixed users

Login names cannot be scoped billing emails: scoped identities contain a colon, which is invalid in HTTP Basic usernames. The managed config therefore separates user/pass from email. SOCKS and its delegated HTTP server share a single locked user table. Runtime RPCs index users by email; authentication indexes by username in constant lookup time. Authentication remains mandatory after removing the last user.

The SOCKS ServerConfig adds protocol users at field 7; generated Go code is regenerated with protoc. Existing accounts remain supported. Full MemoryUser propagation retains dispatcher limits and stats; accepted access messages carry the billing email for audit.

The panel must pin this core revision because it builds AddInbound protobufs in-process as well as invoking the external core. Both compile-time config builders and the runtime binary need this change. Runtime compatibility is v26.10.10-feat.4. Do not publish a panel update before the companion core module and binary are available.

Validation uses race-enabled credential/config/protocol tests, a real core driven by panel gRPC for HTTP/CONNECT/SOCKS5 authentication, billing, hot password edits, last-user revocation and source-IP routing before a broad direct rule. The companion panel also exercises temporary master/child processes.

The focused release gate passes (`common/log`, `common/protocol`, `app/dispatcher`, `testing/trafficlog`, mixedauth, SOCKS, HTTP and infra/conf); the new credential/config packages also pass with `-race`. An attempted full `go test ./...` hits existing external-network tests: DoH at 1.1.1.1 reports record-not-found, domain sniffing cannot fetch github.com, and TLS cannot reach cloudflare.com. These external failures are separate from the passing local mixed tests.
