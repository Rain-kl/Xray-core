# mixed authenticated clients

The SOCKS/mixed inbound accepts an explicit clients array of protocol users with independent user/pass credentials, email, level and native rate limits. The existing accounts/users forms remain compatible; clients cannot be combined with legacy account sources. An explicitly empty clients array is password authenticated and denies all requests, including the HTTP branch.

Mixed uses one thread-safe credential store for SOCKS and HTTP. HTTP Basic usernames remain independent of the internal email used for routing, audit, traffic and caps. UserManager Add/Remove/Get/List/Count uses email while credential lookup uses the case-sensitive username. Duplicate usernames or identities are rejected; removal of the final user never disables authentication. Authentication lookups do not scan the user list. No credentials appear in errors.

SOCKS5 TCP, its associated UDP session and HTTP requests carry the full authenticated MemoryUser. Static legacy auth behavior and standalone HTTP remain compatible. Inbound account names and passwords obey SOCKS5 byte limits; usernames also prohibit colons for HTTP Basic. Runtime dynamic user changes use the same validation as startup. Native caps use the existing dispatcher user limiter. The minimum panel-compatible core is v26.10.10-feat.4; use this feature suffix for local builds and subsequent release.

Verify empty credentials, duplicate rejection, shared HTTP/SOCKS dynamic changes, per-user/per-inbound stats, authenticated UDP, limits and concurrent access. Connection-level source blocking remains a routing/firewall concern, independent of successful proxy protocol negotiation.
