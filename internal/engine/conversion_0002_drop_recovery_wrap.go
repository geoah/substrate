package engine

// Conversion step 2: the recovery wrap leaves history (decision 0264).
//
// Registration wrote the repository's data-encryption key a second time,
// wrapped to the owner's age recipient, as the `sealedKey` of the
// `core/recoverykey` record, and that history line travels in every copy of
// the directory. Whoever holds the matching identity opens every secret of
// every copy with it, so the wrap leaves the line: `sealedKey` is dropped
// from what each line of that kind sets. The rest of the line stays, the
// property's name in `properties` and its manager item included, so the
// line still says what was written and by whom; repository migration 8
// purges the record the line folds into.

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/geoah/substrate/internal/changelogfile"
)

// recoveryWrapProperty is the recoverykey property that held the wrap.
const recoveryWrapProperty = "sealedKey"

func convertDropRecoveryWrap(e *changelogfile.Entry) error {
	if e.Kind != kindRecoveryKey || len(e.Payload) == 0 {
		return nil
	}
	// Numbers decode as written, so every other value re-encodes as the line
	// spelled it.
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return fmt.Errorf("the payload of seq %d: %w", e.Seq, err)
	}
	changed := false
	forEachRecordDeltaSet(payload, func(kindRef, _ string, set map[string]any) {
		if _, held := set[recoveryWrapProperty]; held && kindRef == kindRecoveryKey {
			delete(set, recoveryWrapProperty)
			changed = true
		}
	})
	if !changed {
		return nil
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e.Payload = out
	return nil
}
