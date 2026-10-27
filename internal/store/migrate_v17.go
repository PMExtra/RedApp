package store

import (
	"database/sql"
	"encoding/json"
)

// Preserve the old recursive JSON-release overlay's effective block before
// switching to atomic replacement. Provider identity comes from the instance,
// never from a newly associated template. No source/cache identities change.
func migrate16To17(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT c.entity_uid,c.overrides_json,t.spec_json FROM application_config c LEFT JOIN template_snapshots t ON t.kind='App' AND t.canonical_key=c.template_ref WHERE c.template_ref IS NOT NULL`)
	if err != nil {
		return err
	}
	type update struct {
		uid  string
		body []byte
	}
	var updates []update
	for rows.Next() {
		var uid string
		var raw, base []byte
		if err = rows.Scan(&uid, &raw, &base); err != nil {
			rows.Close()
			return err
		}
		var over, template map[string]any
		if err = json.Unmarshal(raw, &over); err != nil {
			rows.Close()
			return err
		}
		if len(base) > 0 {
			if err = json.Unmarshal(base, &template); err != nil {
				rows.Close()
				return err
			}
		}
		if block, ok := over["json_release"].(map[string]any); ok {
			old, _ := template["json_release"].(map[string]any)
			over["json_release"] = legacyOverlay17(old, block)
			body, e := json.Marshal(over)
			if e != nil {
				rows.Close()
				return e
			}
			updates = append(updates, update{uid, body})
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE TABLE application_config_v17(
 entity_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
 template_ref TEXT,
 provider_key TEXT NOT NULL CHECK(provider_key IN ('info','hosted','http-cache','codex','claude-code','json-release')),
 overrides_json BLOB NOT NULL,
 spec_json BLOB,
 CHECK((template_ref IS NOT NULL AND spec_json IS NULL) OR (template_ref IS NULL AND spec_json IS NOT NULL))
 );
 INSERT INTO application_config_v17(entity_uid,template_ref,provider_key,overrides_json,spec_json)
 SELECT c.entity_uid,c.template_ref,a.provider,c.overrides_json,c.spec_json FROM application_config c JOIN applications a ON a.uid=c.entity_uid;
 DROP TABLE application_config;
 ALTER TABLE application_config_v17 RENAME TO application_config;`); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err = tx.Exec(`UPDATE application_config SET overrides_json=? WHERE entity_uid=?`, u.body, u.uid); err != nil {
			return err
		}
	}
	return nil
}

// Frozen legacy semantics for the schema-16 data format, not a runtime resolver.
func legacyOverlay17(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if child, ok := v.(map[string]any); ok {
			old, _ := out[k].(map[string]any)
			out[k] = legacyOverlay17(old, child)
		} else {
			out[k] = v
		}
	}
	return out
}
