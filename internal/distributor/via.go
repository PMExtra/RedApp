package distributor

import "strings"

// ViaPseudonym names a RedApp instance in the Via header of its upstream
// requests, so that another RedApp, or the instance itself, can recognize a
// request that has already passed through it.
func ViaPseudonym(instanceUID string) string { return "redapp-" + instanceUID }

// SetInstance makes every upstream request of the pool carry
// "Via: 1.1 redapp-<uid>".
func (p *Pool) SetInstance(uid string) {
	if p != nil {
		p.via.Store(ViaPseudonym(uid))
	}
}

func (p *Pool) viaValue() string {
	if p == nil {
		return ""
	}
	if v, ok := p.via.Load().(string); ok && v != "" {
		return "1.1 " + v
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
