package history

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

type History struct {
	db   *store.Store
	boot string
}

func Open(db *store.Store) (*History, error) {
	var boot [16]byte
	if _, err := rand.Read(boot[:]); err != nil {
		return nil, err
	}
	return &History{db: db, boot: hex.EncodeToString(boot[:])}, nil
}

// Observation is one bounded global or application metric snapshot.
type Observation struct {
	Scope   string
	AppID   string
	Metrics []Metric
}

func (h *History) Record(at time.Time, metrics []Metric) error {
	return h.RecordScoped(at, []Observation{{Scope: "global", Metrics: metrics}})
}

// RecordScoped validates every observation before storing any of them.
// Recording and hourly maintenance share one store transaction.
func (h *History) RecordScoped(at time.Time, observations []Observation) error {
	samples := []store.MetricSample{}
	for _, observation := range observations {
		if observation.Scope != "global" && observation.Scope != "app" || observation.Scope == "global" && observation.AppID != "" || observation.Scope == "app" && !store.ValidAppID(observation.AppID) {
			return errors.New("invalid metric scope")
		}
		for _, metric := range observation.Metrics {
			definition, ok := definitionFor(observation.Scope, metric.Key)
			if !ok {
				return errors.New("unknown metric for this scope")
			}
			if metric.Value == nil {
				continue
			}
			value := *metric.Value
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return errors.New("invalid metric observation")
			}
			if definition.Kind == "rate" && metric.ObservedSeconds != 5 {
				return errors.New("rate observations require a complete five-second window")
			}
			samples = append(samples, store.MetricSample{Scope: observation.Scope, AppID: observation.AppID, Key: metric.Key, Kind: store.MetricKind(definition.Kind), Value: value})
		}
	}
	return h.db.RecordMetrics(at, h.boot, samples, storageCatalog())
}

func (h *History) Maintain(at time.Time) error {
	return h.db.MaintainMetrics(at, storageCatalog())
}

func storageCatalog() []store.MetricDefinition {
	out := []store.MetricDefinition{}
	for _, d := range Definitions() {
		out = append(out, store.MetricDefinition{Key: d.Key, Kind: store.MetricKind(d.Kind)})
	}
	return out
}

// AppDefinitions contains only bounded metrics with real application ownership.
// Process/runtime, global filesystem capacity and public entry requests are not
// duplicated as synthetic per-application series.
func AppDefinitions() []Definition {
	out := []Definition{}
	for _, d := range Definitions() {
		if strings.HasPrefix(d.Key, "resources.") || d.Key == "versions.total" || strings.HasPrefix(d.Key, "counters.") && d.Key != "counters.requests" {
			out = append(out, d)
		}
	}
	return out
}
func definitionFor(scope, key string) (Definition, bool) {
	if scope == "global" {
		return Find(key)
	}
	if strings.HasPrefix(key, "resources.") || key == "versions.total" || strings.HasPrefix(key, "counters.") && key != "counters.requests" {
		return Find(key)
	}
	return Definition{}, false
}

type Point struct {
	Time            int64    `json:"time"`
	Value           *float64 `json:"value"`
	Min             *float64 `json:"min"`
	Max             *float64 `json:"max"`
	Avg             *float64 `json:"avg"`
	Last            *float64 `json:"last"`
	Count           int      `json:"count"`
	Delta           *float64 `json:"delta"`
	DeltaCount      int      `json:"delta_count"`
	ObservedSeconds float64  `json:"observed_seconds"`
	Partial         bool     `json:"partial"`
	Incomplete      bool     `json:"incomplete"`
}
type Series struct {
	Definition
	Scope             string  `json:"scope"`
	AppID             string  `json:"app_id,omitempty"`
	Range             string  `json:"range"`
	ResolutionSeconds int64   `json:"resolution_seconds"`
	From              int64   `json:"from"`
	To                int64   `json:"to"`
	Points            []Point `json:"points"`
}

func (h *History) Query(key, window string, at time.Time) (Series, error) {
	return h.query("global", "", key, window, at)
}
func (h *History) QueryFor(app, key, window string, at time.Time) (Series, error) {
	if !store.ValidAppID(app) {
		return Series{}, errors.New("canonical application identity is required")
	}
	return h.query("app", app, key, window, at)
}
func (h *History) query(scope, app, key, window string, at time.Time) (Series, error) {
	d, ok := definitionFor(scope, key)
	if !ok {
		return Series{}, errors.New("unknown metric for this scope")
	}
	duration := time.Duration(0)
	resolution := time.Hour
	switch window {
	case "24h":
		duration = 24 * time.Hour
		resolution = time.Minute
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		return Series{}, errors.New("history range must be 24h, 7d, or 30d")
	}
	from := at.UTC().Add(-duration).Truncate(resolution).Unix()
	to := at.UTC().Truncate(resolution).Unix()
	step := int64(resolution / time.Second)
	series := Series{Definition: d, Scope: scope, AppID: app, Range: window, ResolutionSeconds: step, From: from, To: at.Unix(), Points: []Point{}}
	values := map[int64]Point{}
	var buckets []store.MetricBucket
	var err error
	if resolution == time.Minute {
		buckets, err = h.db.MetricMinutes(scope, app, key, from, to)
	} else {
		buckets, err = h.db.MetricHours(scope, app, key, store.MetricKind(d.Kind), from, to, at)
	}
	if err != nil {
		return Series{}, err
	}
	for _, b := range buckets {
		p := Point{Time: b.Time, Min: b.Min, Max: b.Max, Avg: b.Avg, Last: b.Last, Count: b.Count, Delta: b.Delta, DeltaCount: b.DeltaCount, ObservedSeconds: b.ObservedSeconds}
		if resolution == time.Minute {
			p.Value = p.Last
			if d.Kind != "counter" {
				p.Min, p.Max, p.Avg = p.Last, p.Last, p.Last
			}
		} else {
			p.Value = p.Avg
			if d.Kind == "counter" {
				p.Value = p.Last
			}
		}
		values[p.Time] = p
	}

	for t := from; t <= to; t += step {
		p, ok := values[t]
		if !ok {
			p.Time = t
		}
		p.Partial = t == to
		expected := 1
		if resolution == time.Hour {
			expected = 60
		}
		p.Incomplete = p.Count < expected
		series.Points = append(series.Points, p)
	}
	return series, nil
}
