package httpcache

import (
	"container/list"
	"errors"
	"math/rand/v2"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
)

const sourceCursorLimit = 4096

type sourceAttempt struct {
	Client *distributor.Client
	Index  int
	// SourceURL is the canonical base identity, not an arbitrary request URL.
	SourceURL string
}
type sourceCursor struct {
	key   string
	epoch int64
	next  uint64
}

// sourceAttempts chooses an attempt order once per real upstream operation. It
// does not fetch, health-check, or rotate on cache hits. Callers decide which
// response classes may advance to the next source.
func (s *Service) sourceAttempts(entry application.Entry) ([]sourceAttempt, error) {
	if entry.Provider != application.HttpCache {
		return nil, errors.New("Multiple source selection requires GeneralHttp")
	}
	clients := entry.Upstreams
	if len(clients) == 0 && entry.Upstream != nil {
		clients = []*distributor.Client{entry.Upstream}
	}
	if len(clients) == 0 || len(clients) > 16 {
		return nil, errors.New("GeneralHttp requires between one and sixteen sources")
	}
	attempts := make([]sourceAttempt, len(clients))
	for i, client := range clients {
		if client == nil || client.Base == nil {
			return nil, errors.New("HTTP source client is unavailable")
		}
		attempts[i] = sourceAttempt{Client: client, Index: i, SourceURL: client.Base.String()}
	}
	switch entry.SourceStrategy {
	case "", "ordered":
		return attempts, nil
	case "round_robin":
		start := s.nextSource(entry, len(attempts))
		ordered := make([]sourceAttempt, 0, len(attempts))
		ordered = append(ordered, attempts[start:]...)
		ordered = append(ordered, attempts[:start]...)
		return ordered, nil
	case "random":
		rand.Shuffle(len(attempts), func(i, j int) { attempts[i], attempts[j] = attempts[j], attempts[i] })
		return attempts, nil
	default:
		return nil, errors.New("Unknown HTTP source strategy")
	}
}

func (s *Service) nextSource(entry application.Entry, count int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sourceCursors == nil {
		s.sourceCursors = map[string]*list.Element{}
	}
	key := entry.UID
	if key == "" {
		key = entry.Descriptor.ID
	}
	element := s.sourceCursors[key]
	if element == nil {
		element = s.sourceOrder.PushFront(&sourceCursor{key: key, epoch: entry.SourceEpoch})
		s.sourceCursors[key] = element
		if s.sourceOrder.Len() > sourceCursorLimit {
			oldest := s.sourceOrder.Back()
			delete(s.sourceCursors, oldest.Value.(*sourceCursor).key)
			s.sourceOrder.Remove(oldest)
		}
	} else {
		s.sourceOrder.MoveToFront(element)
	}
	cursor := element.Value.(*sourceCursor)
	// An already-admitted request from an older epoch can still finish, but must
	// not replace the current epoch's rotation state.
	if entry.SourceEpoch < cursor.epoch {
		return 0
	}
	if entry.SourceEpoch > cursor.epoch {
		cursor.epoch = entry.SourceEpoch
		cursor.next = 0
	}
	start := int(cursor.next % uint64(count))
	cursor.next++
	return start
}
