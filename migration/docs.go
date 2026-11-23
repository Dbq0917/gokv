/*
Package migration contains a zero-downtime, reversible store-to-store migration
helper built on top of the gokv.Store abstraction.

The typical workflow to move from a volatile, in-memory backend to a persistent
one without taking the service offline looks like this:

	// 1. Keep the old store open and bring the new store up.
	source := syncmap.NewStore(syncmap.DefaultOptions)
	target, _ := bbolt.NewStore(bbolt.Options{Path: path})

	// 2. Route traffic through the DualStore. Writes now hit both backends.
	dual, _ := migration.NewDualStore(source, target)

	// 3. Copy the data that existed before the dual-write phase started,
	//    repeating until verification catches up with concurrent writes.
	for {
	    if _, err := dual.CopyAll(); err != nil { return err }
	    if err := dual.Verify(); err == nil { break }
	}

	// 4. Commit only after every key is confirmed present and identical.
	//    Commit itself verifies again and refuses to switch otherwise.
	if err := dual.Commit(); err != nil {
	    // Fix the gap (or Rollback) instead of cutting over with missing data.
	    _ = dual.Rollback()
	}

	// 5. After a stable observation window, close the source to finish.

Rollback can restore the source as primary at any point before it is closed.
*/
package migration
