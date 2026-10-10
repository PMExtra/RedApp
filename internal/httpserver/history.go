package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/logging"
)

// SampleHistory records immediately, then once per minute. The same owner
// performs closed-hour aggregation and retention, independent of admin traffic.
// Failures are logged with component=history; the next sample retries.
func (s *Server) SampleHistory(ctx context.Context) {
	log := logging.For(s.log, "history")
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	sample := func() {
		at, metrics, err := s.globalMetrics()
		if err != nil {
			log.Error("metric sampling failed", logging.Error(err))
			if maintenanceErr := s.history.Maintain(time.Now().UTC()); maintenanceErr != nil {
				log.Error("metric history maintenance failed", logging.Error(maintenanceErr))
			}
			return
		}
		observations := []history.Observation{{Scope: "global", Metrics: metrics}}
		for _, entry := range s.registry.Entries() {
			if entry.Provider == application.Info || entry.Provider == application.Hosted {
				continue
			}
			appMetrics, err := s.appMetrics(entry)
			if err != nil {
				log.Error("metric sampling failed", slog.String("app", entry.Descriptor.ID), logging.Error(err))
				return
			}
			observations = append(observations, history.Observation{Scope: "app", AppID: entry.MetricsID(), Metrics: appMetrics})
		}
		if err = s.history.RecordScoped(at, observations); err != nil {
			log.Error("metric history write failed", logging.Error(err))
		}
	}
	sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			sample()
		}
	}
}

type historyPointDTO struct {
	Time            time.Time `json:"time"`
	Value           *float64  `json:"value"`
	Min             *float64  `json:"min"`
	Max             *float64  `json:"max"`
	Avg             *float64  `json:"avg"`
	Last            *float64  `json:"last"`
	Count           int       `json:"count"`
	Delta           *float64  `json:"delta"`
	DeltaCount      int       `json:"delta_count"`
	ObservedSeconds float64   `json:"observed_seconds"`
	Partial         bool      `json:"partial"`
	Incomplete      bool      `json:"incomplete"`
}

type historySeriesDTO struct {
	Key               string            `json:"key"`
	Label             string            `json:"label"`
	Kind              string            `json:"kind"`
	Unit              string            `json:"unit"`
	Group             string            `json:"group"`
	Scope             string            `json:"scope"`
	AppKey            *string           `json:"app_key"`
	Range             string            `json:"range"`
	ResolutionSeconds int64             `json:"resolution_seconds"`
	From              time.Time         `json:"from"`
	To                time.Time         `json:"to"`
	Points            []historyPointDTO `json:"points"`
}

func historyDocument(series history.Series, appKey *string) historySeriesDTO {
	out := historySeriesDTO{Key: series.Key, Label: series.Label, Kind: series.Kind, Unit: series.Unit, Group: series.Group, Scope: series.Scope, AppKey: appKey, Range: series.Range, ResolutionSeconds: series.ResolutionSeconds, From: time.Unix(series.From, 0).UTC(), To: time.Unix(series.To, 0).UTC(), Points: make([]historyPointDTO, 0, len(series.Points))}
	for _, p := range series.Points {
		if p.Count == 0 {
			continue // Buckets without samples are gaps, not points.
		}
		out.Points = append(out.Points, historyPointDTO{Time: time.Unix(p.Time, 0).UTC(), Value: p.Value, Min: p.Min, Max: p.Max, Avg: p.Avg, Last: p.Last, Count: p.Count, Delta: p.Delta, DeltaCount: p.DeltaCount, ObservedSeconds: p.ObservedSeconds, Partial: p.Partial, Incomplete: p.Incomplete})
	}
	return out
}

// historyQuery reads the required metric and range parameters; metric must be
// one of definitions.
func historyQuery(r *http.Request, definitions []history.Definition) (string, string, *apiError) {
	q := r.URL.Query()
	metric, window := q.Get("metric"), q.Get("range")
	if window != "24h" && window != "7d" && window != "30d" {
		return "", "", newError(codeInvalidQuery, nil, "range must be 24h, 7d or 30d")
	}
	for _, d := range definitions {
		if d.Key == metric {
			return metric, window, nil
		}
	}
	return "", "", newError(codeInvalidQuery, nil, "metric is not a metric of this scope")
}

func (s *Server) getHistory(w http.ResponseWriter, r *http.Request) {
	metric, window, e := historyQuery(r, history.Definitions())
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	series, err := s.history.Query(metric, window, time.Now().UTC())
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeOK(w, historyDocument(series, nil))
}

func (s *Server) getAppHistory(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.metricsApp(w, r)
	if !ok {
		return
	}
	metric, window, e := historyQuery(r, history.AppDefinitions())
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	series, err := s.history.QueryFor(entry.MetricsID(), metric, window, time.Now().UTC())
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	key := entry.Descriptor.ID
	writeOK(w, historyDocument(series, &key))
}
