package store

import (
	"database/sql"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

func readAllNotes(tx *sql.Tx) (map[string]AdminNotes, error) {
	out := map[string]AdminNotes{}
	for kind, table := range map[string]string{"Vendor": "vendor_admin_notes", "App": "application_admin_notes"} {
		rows, e := tx.Query(`SELECT entity_uid,text,revision FROM ` + table)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var uid string
			var n AdminNotes
			if e = rows.Scan(&uid, &n.Text, &n.Revision); e != nil {
				rows.Close()
				return nil, e
			}
			out[configKey(kind, uid)] = n
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
func writeAllNotes(tx *sql.Tx, old, next map[string]AdminNotes) error {
	for key, n := range next {
		if reflect.DeepEqual(old[key], n) {
			continue
		}
		kind, uid, _ := strings.Cut(key, ":")
		table := "application_admin_notes"
		if kind == "Vendor" {
			table = "vendor_admin_notes"
		}
		if _, e := tx.Exec(`INSERT INTO `+table+`(entity_uid,text,revision) VALUES(?,?,?) ON CONFLICT(entity_uid) DO UPDATE SET text=excluded.text,revision=excluded.revision`, uid, n.Text, n.Revision); e != nil {
			return e
		}
	}
	return nil
}
func validNotes(text string) bool {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 12000 {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
