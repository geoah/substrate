package engine

// Repository migration 13: every person becomes an admin (decision 0275).
//
// Before roles, a person of the repository could do everything, and the role
// that keeps that is `admin`. Each live person actor that holds no role is
// given it, as the system's ordinary write, before the stored vocabulary
// loads and so before the upgrade that declares `roles` on the actor and the
// role kind itself; the same open writes the role records after that upgrade
// (ensureShippedRoles), so the reference finds its record by the time
// anything reads it. A person whose actor is disabled or tombstoned was taken
// out and is given nothing. A second run finds every person holding a role.

import (
	"encoding/json"
	"fmt"

	"github.com/geoah/substrate/internal/scope"
)

func migrateGiveEveryPersonAdmin(t *txn) (string, error) {
	rows, err := t.query(`SELECT id, props FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'type' = 'person'
		AND COALESCE(props->>'disabled', '') <> 'true' ORDER BY id`, kindActor)
	if err != nil {
		return "", err
	}
	type person struct {
		id    string
		props map[string]any
	}
	var people []person
	for rows.Next() {
		var p person
		var raw []byte
		if err := rows.Scan(&p.id, &raw); err != nil {
			_ = rows.Close()
			return "", err
		}
		if err := json.Unmarshal(raw, &p.props); err != nil {
			_ = rows.Close()
			return "", err
		}
		people = append(people, p)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	given := 0
	for _, p := range people {
		if held, err := json.Marshal(p.props["roles"]); err == nil && len(roleIDs(held)) > 0 {
			continue
		}
		p.props["roles"] = roleRefs(scope.RoleAdmin)
		if err := t.migrateRow(eref{Kind: kindActor, ID: p.id}, p.props, false); err != nil {
			return "", err
		}
		given++
	}
	return fmt.Sprintf("gave %d person(s) the admin role", given), nil
}
