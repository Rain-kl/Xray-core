# Audit domain attribution correction

Sniffing changed session routing targets but access messages retained their original To field. Record the observed domain separately rather than overwrite the original target, which administrators need for investigation. Extend the existing log/event boundary and reuse stored Host/Site columns, avoiding a new schema or sniffing subsystem. Capture before shouldOverride so routeOnly and override exclusions cannot suppress observed domains.

Maintenance: preserve optional log field escaping, both dispatcher paths, spool serialization and legacy fallback; verify actual HTTP traffic and unchanged original targets.

Regression proof: an overlay restoring the pre-fix dispatcher makes the route-only and override-suppressed real HTTP cases fail specifically for missing domain metadata. Disabled sniffing remains unchanged.
