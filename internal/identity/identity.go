// Package identity defines public names and private storage namespaces without
// depending on a provider, HTTP client, or database.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var uidPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidSlug(id string) bool { return len(id) <= 63 && slugPattern.MatchString(id) }

func ValidVendor(id string) bool {
	if !ValidSlug(id) {
		return false
	}
	switch id {
	case "admin", "api", "assets", "health", "all":
		return false
	}
	return true
}

func ValidKey(key string) bool {
	vendor, app, ok := strings.Cut(key, "/")
	return ok && ValidVendor(vendor) && ValidSlug(app)
}

func ValidUID(uid string) bool { return uidPattern.MatchString(uid) }

func NewUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func MetricsID(uid string) string { return "app/" + uid }
func StorageID(uid string, epoch int64) string {
	return MetricsID(uid) + "-e" + strconv.FormatInt(epoch, 10)
}

func ParseStorageID(id string) (uid string, epoch int64, ok bool) {
	value, found := strings.CutPrefix(id, "app/")
	if !found {
		return "", 0, false
	}
	uid, raw, found := strings.Cut(value, "-e")
	if !found || !ValidUID(uid) {
		return "", 0, false
	}
	epoch, err := strconv.ParseInt(raw, 10, 64)
	return uid, epoch, err == nil && epoch > 0 && raw == strconv.FormatInt(epoch, 10)
}
