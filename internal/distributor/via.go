package distributor

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ViaPseudonym names the sender of an upstream request in its Via header: a
// RedApp instance, and the application whose source the request reads. The
// application appears only as a digest. An instance recognizes a request that
// an application sent to itself, so a source leading back to the same
// application is refused instead of waiting on itself; other applications of
// the same instance may still use each other as sources.
func ViaPseudonym(instanceUID, appUID string) string {
	if appUID == "" {
		return "redapp-" + instanceUID
	}
	sum := sha256.Sum256([]byte(instanceUID + "\x00" + appUID))
	return "redapp-" + instanceUID + "." + hex.EncodeToString(sum[:8])
}

// SetInstance makes every upstream request of the pool's clients carry
// "Via: 1.1 <pseudonym>".
func (p *Pool) SetInstance(uid string) {
	if p != nil {
		p.instance.Store(uid)
	}
}

func (c *Client) viaValue() string {
	if c.pool == nil {
		return ""
	}
	if uid, ok := c.pool.instance.Load().(string); ok && uid != "" {
		return "1.1 " + ViaPseudonym(uid, c.appUID)
	}
	return ""
}

// ViaContains reports whether a Via header names the pseudonym as one of the
// intermediaries (received-by, ignoring protocol and comments).
func ViaContains(values []string, pseudonym string) bool {
	for _, value := range values {
		for _, hop := range strings.Split(value, ",") {
			fields := strings.Fields(hop)
			if len(fields) >= 2 && fields[1] == pseudonym {
				return true
			}
		}
	}
	return false
}
