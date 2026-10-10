package httpserver

import (
	"net/http"
	"strconv"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

type eventDTO struct {
	ID           int64     `json:"id"`
	Time         time.Time `json:"time"`
	Category     string    `json:"category"`
	Code         string    `json:"code"`
	Message      string    `json:"message"`
	StatusCode   *int      `json:"status_code"`
	AppKey       *string   `json:"app_key"`
	Version      *string   `json:"version"`
	ResourceKey  *string   `json:"resource_key"`
	GenerationID *string   `json:"generation_id"`
}

// eventDocument maps a stored event. Internal application scopes become the
// public application key, or null once the application no longer exists.
func eventDocument(e store.ListedEvent, scopes map[string]string) eventDTO {
	out := eventDTO{ID: e.ID, Time: e.Time.UTC(), Category: e.Category, Code: e.Code, Message: displayText(e.Message), AppKey: optionalText(publicScope(e.AppID, scopes)), Version: optionalText(e.Version), ResourceKey: optionalText(e.ResourceKey), GenerationID: optionalText(e.GenerationID)}
	if e.StatusCode >= 100 && e.StatusCode <= 599 {
		status := e.StatusCode
		out.StatusCode = &status
	}
	return out
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, e := queryInt(r, "limit", 50, 1, 100)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	last, e := decodeAfterCursor(r, "listEvents", cursorScope())
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var before int64
	if last != "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != last {
			s.writeError(w, r, invalidCursor())
			return
		}
		before = n
	}
	rows, err := s.store.EventPage("", before, limit+1)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	page := cursorPage[eventDTO]{Items: []eventDTO{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = afterCursor("listEvents", cursorScope(), strconv.FormatInt(rows[limit-1].ID, 10))
	}
	scopes := s.publicScopes()
	for _, row := range rows {
		page.Items = append(page.Items, eventDocument(row, scopes))
	}
	writeOK(w, page)
}
