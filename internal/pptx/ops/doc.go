// Package ops applies plans: ordered operations run against an in-memory
// working copy, all or nothing. Each plan returns the changed objects, the
// hazards (stale-connector, dangling-glue, group-frame, inset-overflow) and the parts touched.
// Hazards are reported, never fixed.
package ops
