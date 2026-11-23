package migration

import (
	"bytes"
	"fmt"

	"github.com/philippgille/gokv"
)

// Copied counts the key-value pairs that a copy operation processed.
type Copied struct {
	// Keys is the number of keys that were read from the source.
	Keys int
	// Written is the number of values written to the target.
	Written int
}

// CopyAll copies every key-value pair currently held by the source store into
// the target store, byte for byte. It is meant to run while the DualStore is
// in the DualWrite phase, so any write that arrives during or after the copy
// is still mirrored to the target by the regular Set/Delete path.
//
// Because concurrent writes can change a key while it is being copied, CopyAll
// is not expected to produce a consistent snapshot on its own; callers must
// follow it with Verify (Commit does this automatically) and may repeat the
// pair until verification succeeds.
func (d *DualStore) CopyAll() (Copied, error) {
	d.lock.RLock()
	if d.phase != DualWrite && d.phase != RolledBack {
		d.lock.RUnlock()
		return Copied{}, fmt.Errorf("%w: copy is only allowed before the switch (phase was %s)", ErrPhase, d.phase)
	}
	source := d.source
	target := d.target
	d.lock.RUnlock()

	srcIter := source.(gokv.IterStore)
	srcBytes := source.(gokv.ByteStore)
	dstBytes := target.(gokv.ByteStore)

	keys, err := srcIter.Keys()
	if err != nil {
		return Copied{}, fmt.Errorf("listing source keys: %w", err)
	}

	result := Copied{Keys: len(keys)}
	for _, key := range keys {
		data, found, err := srcBytes.GetBytes(key)
		if err != nil {
			return result, fmt.Errorf("reading source key %q: %w", key, err)
		}
		if !found {
			// The key was deleted between Keys() and GetBytes(). The regular
			// Delete path already removed it from the target, so just skip it.
			continue
		}
		if err := dstBytes.SetBytes(key, data); err != nil {
			return result, fmt.Errorf("writing target key %q: %w", key, err)
		}
		result.Written++
	}
	return result, nil
}

// Verify compares every key of the source and target store and returns a
// *VerificationError describing any difference. A nil result means both stores
// hold exactly the same keys with identical raw bytes, i.e. the target has
// fully caught up and the switch is safe.
//
// Verify is read-only and can be called repeatedly while writes continue.
func (d *DualStore) Verify() error {
	d.lock.RLock()
	source := d.source
	target := d.target
	d.lock.RUnlock()

	srcIter := source.(gokv.IterStore)
	srcBytes := source.(gokv.ByteStore)
	dstBytes := target.(gokv.ByteStore)

	sourceKeys, err := srcIter.Keys()
	if err != nil {
		return fmt.Errorf("listing source keys: %w", err)
	}

	verr := &VerificationError{}
	sourceSet := make(map[string]struct{}, len(sourceKeys))
	for _, key := range sourceKeys {
		sourceSet[key] = struct{}{}

		srcData, srcFound, err := srcBytes.GetBytes(key)
		if err != nil {
			return fmt.Errorf("reading source key %q: %w", key, err)
		}
		dstData, dstFound, err := dstBytes.GetBytes(key)
		if err != nil {
			return fmt.Errorf("reading target key %q: %w", key, err)
		}

		switch {
		case srcFound && !dstFound:
			verr.Missing = append(verr.Missing, key)
		case srcFound && dstFound && !bytes.Equal(srcData, dstData):
			verr.Different = append(verr.Different, key)
		}
	}

	// Detect keys that only exist in the target. If the target can enumerate
	// its keys we do a full check; otherwise extra keys can't occur through the
	// DualStore write path, so this best-effort check is skipped.
	if dstIter, ok := target.(gokv.IterStore); ok {
		targetKeys, err := dstIter.Keys()
		if err != nil {
			return fmt.Errorf("listing target keys: %w", err)
		}
		for _, key := range targetKeys {
			if _, exists := sourceSet[key]; !exists {
				verr.Extra = append(verr.Extra, key)
			}
		}
	}

	if len(verr.Missing) == 0 && len(verr.Different) == 0 && len(verr.Extra) == 0 {
		return nil
	}
	return verr
}

// Commit switches the read/write primary from the source to the target.
//
// It runs Verify first and refuses to switch while the stores differ, which is
// the guard that previously was missing: the backend must contain every
// source key with identical bytes before traffic is cut over. On success the
// phase becomes Switched. The source is intentionally kept open so Rollback
// stays available until the caller closes it explicitly.
func (d *DualStore) Commit() error {
	if err := d.Verify(); err != nil {
		return fmt.Errorf("refusing to switch before migration is complete: %w", err)
	}

	d.lock.Lock()
	defer d.lock.Unlock()
	if d.phase == Switched {
		return nil
	}
	d.phase = Switched
	return nil
}

// Rollback reverses a Commit (or a not-yet-committed migration) by making the
// source the read/write primary again. It is safe to call at any time because
// the source store is never closed or cleared by the migration.
//
// After a rollback the target still receives every write, so once the problem
// is fixed the migration can be retried with CopyAll/Verify/Commit.
func (d *DualStore) Rollback() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	if d.phase == RolledBack {
		return nil
	}
	d.phase = RolledBack
	return nil
}
