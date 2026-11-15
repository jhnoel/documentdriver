// Package plan is the format-neutral transaction machinery both CLIs
// apply plans through: a Tx over one package's working copy, run
// operation by operation and all or nothing; the write set operations
// declare (WriteSet) and the commit-time verifier that holds the working
// copy to it (Verify, over a format's Units); commit through
// opc.Package.SaveAndClose, which saves atomically and writes nothing
// for a no-op; Finish, which snapshots the working copy, lets the format
// build the edit result from it and commits unless it is a dry run; and
// the engine side of the edit result (Result, Hazard).
//
// Formats keep their own operations, plan schema, units and hazard kinds
// on top. This package imports nothing format-specific.
package plan
