package history

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"math"
	"strings"
	"time"
)

type History struct {
	db   *sql.DB
	boot string
}

func Open(db *store.Store) (*History, error) {
	var boot [16]byte
	if _, err := rand.Read(boot[:]); err != nil {
		return nil, err
	}
	return &History{db: db.DB, boot: hex.EncodeToString(boot[:])}, nil
}

// Record and maintenance share a transaction. A failure cannot delete raw
// observations whose closed-hour aggregates have not been committed.
// Observation is one bounded global or application metric snapshot.
type Observation struct {
	Scope   string
	AppID   string
	Metrics []Metric
}

func (h *History) Record(at time.Time, metrics []Metric) error {
	return h.RecordScoped(at, []Observation{{Scope: "global", Metrics: metrics}})
}
func (h *History) RecordScoped(at time.Time, observations []Observation) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	minute := at.UTC().Truncate(time.Minute).Unix()
	observed := at.Unix()
	var watermark int64
	if err = tx.QueryRow("SELECT aggregated_before_s FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
		return err
	}
	if minute < watermark {
		return errors.New("Metrics clock moved behind committed hourly aggregates")
	}
	for _, observation := range observations {
		if observation.Scope != "global" && observation.Scope != "app" || observation.Scope == "global" && observation.AppID != "" || observation.Scope == "app" && !store.ValidAppID(observation.AppID) {
			return errors.New("Invalid metric scope")
		}
		for _, metric := range observation.Metrics {
			definition, ok := definitionFor(observation.Scope, metric.Key)
			if !ok || definition.Retired {
				return errors.New("Unknown or retired metric for this scope")
			}
			if metric.Value == nil {
				continue
			}
			value := *metric.Value
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return errors.New("Invalid metric observation")
			}
			duration := 0.0
			var delta *float64
			if definition.Kind == "counter" {
				var previous float64
				var previousAt, previousMinute int64
				var previousBoot string
				err = tx.QueryRow("SELECT value,observed_at_s,boot,t_s FROM metric_samples WHERE scope=? AND app_id=? AND metric=? ORDER BY t_s DESC LIMIT 1", observation.Scope, observation.AppID, metric.Key).Scan(&previous, &previousAt, &previousBoot, &previousMinute)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				elapsed := observed - previousAt
				if err == nil && previousBoot == h.boot && minute-previousMinute == 60 && value >= previous && elapsed > 0 && elapsed <= 90 {
					difference := value - previous
					delta = &difference
					duration = float64(elapsed)
				}
			} else if definition.Kind == "rate" {
				if metric.ObservedSeconds != 5 {
					return errors.New("Rate observations require a complete five-second window")
				}
				duration = 5
			}
			if _, err = tx.Exec("INSERT OR IGNORE INTO metric_samples VALUES(?,?,?,?,?,?,?,?,?)", observation.Scope, observation.AppID, metric.Key, minute, observed, h.boot, value, delta, duration); err != nil {
				return err
			}
		}
	}
	if err = h.maintain(tx, at); err != nil {
		return err
	}
	return tx.Commit()
}
func (h *History) Maintain(at time.Time) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = h.maintain(tx, at); err != nil {
		return err
	}
	return tx.Commit()
}
func (h *History) maintain(tx *sql.Tx, at time.Time) error {
	hour := at.UTC().Truncate(time.Hour).Unix()
	var watermark int64
	if err := tx.QueryRow("SELECT aggregated_before_s FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
		return err
	}
	if hour < watermark {
		return errors.New("Metrics clock moved behind committed hourly aggregates")
	}
	if hour > watermark {
		for _, d := range historicalDefinitions() {
			query := `INSERT INTO metric_hours(scope,app_id,metric,t_s,min,max,avg,last,count,delta,delta_count,duration_s) ` + hourlySelect(d.Kind, false) + `
 ON CONFLICT(scope,app_id,metric,t_s) DO UPDATE SET min=excluded.min,max=excluded.max,avg=excluded.avg,last=excluded.last,count=excluded.count,delta=excluded.delta,delta_count=excluded.delta_count,duration_s=excluded.duration_s`

			if _, err := tx.Exec(query, hour, d.Key, watermark, hour); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("UPDATE metric_history_state SET aggregated_before_s=? WHERE id=1", hour); err != nil {
			return err
		}
	}
	// UTC bucket-aligned retention includes the leading partial range bucket.
	if _, err := tx.Exec("DELETE FROM metric_samples WHERE t_s<?", at.UTC().Truncate(time.Minute).Add(-24*time.Hour).Unix()); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM metric_hours WHERE t_s<?", at.UTC().Truncate(time.Hour).Add(-30*24*time.Hour).Unix())
	return err
}

// hourlySelect also serves read-only queries before the minute sampler has
// committed the most recently closed hour. It never averages counters.
func hourlySelect(kind string, scoped bool) string {
	minExpr, maxExpr, avgExpr := "MIN(value)", "MAX(value)", "AVG(value)"
	if kind == "counter" {
		minExpr, maxExpr, avgExpr = "NULL", "NULL", "NULL"
	} else if kind == "rate" {
		avgExpr = "SUM(value*duration_s)/NULLIF(SUM(duration_s),0)"
	}
	scopeFilter := ""
	if scoped {
		scopeFilter = " AND scope=? AND app_id=?"
	}
	return `SELECT scope,app_id,metric,(t_s/3600)*3600,` + minExpr + `,` + maxExpr + `,` + avgExpr + `,
 (SELECT value FROM metric_samples tail WHERE tail.scope=s.scope AND tail.app_id=s.app_id AND tail.metric=s.metric AND tail.t_s>=(s.t_s/3600)*3600 AND tail.t_s<(s.t_s/3600)*3600+3600 AND tail.t_s<? ORDER BY tail.t_s DESC LIMIT 1),
 COUNT(*),SUM(delta),COUNT(delta),SUM(duration_s) FROM metric_samples s WHERE metric=? AND t_s>=? AND t_s<?` + scopeFilter + ` GROUP BY scope,app_id,metric,(t_s/3600)*3600`
}

// AppDefinitions contains only bounded metrics with real application ownership.
// Process/runtime, global filesystem capacity and public entry requests are not
// duplicated as synthetic per-application series.
func AppDefinitions() []Definition {
	out := []Definition{}
	for _, d := range Definitions() {
		if strings.HasPrefix(d.Key, "resources.") || d.Key == "versions.total" || strings.HasPrefix(d.Key, "counters.") && d.Key != "counters.requests" && d.Key != "counters.reuse_requests" {
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
func historicalDefinitions() []Definition { return Catalog() }

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

func pointer(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	n := v.Float64
	return &n
}
func (h *History) Query(key, window string, at time.Time) (Series, error) {
	return h.query("global", "", key, window, at)
}
func (h *History) QueryFor(app, key, window string, at time.Time) (Series, error) {
	if !store.ValidAppID(app) {
		return Series{}, errors.New("Canonical application identity is required")
	}
	return h.query("app", app, key, window, at)
}
func (h *History) query(scope, app, key, window string, at time.Time) (Series, error) {
	d, ok := definitionFor(scope, key)
	if !ok {
		return Series{}, errors.New("Unknown metric for this scope")
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
		return Series{}, errors.New("History range must be 24h, 7d, or 30d")
	}
	from := at.UTC().Add(-duration).Truncate(resolution).Unix()
	to := at.UTC().Truncate(resolution).Unix()
	step := int64(resolution / time.Second)
	series := Series{Definition: d, Scope: scope, AppID: app, Range: window, ResolutionSeconds: step, From: from, To: at.Unix(), Points: []Point{}}
	tx, err := h.db.Begin()
	if err != nil {
		return Series{}, err
	}
	defer tx.Rollback()
	values := map[int64]Point{}
	var rows *sql.Rows
	if resolution == time.Minute {
		rows, err = tx.Query("SELECT t_s,value,delta,duration_s FROM metric_samples WHERE scope=? AND app_id=? AND metric=? AND t_s>=? AND t_s<=? ORDER BY t_s", scope, app, key, from, to)
		if err != nil {
			return Series{}, err
		}
		for rows.Next() {
			var p Point
			var value, delta sql.NullFloat64
			if err = rows.Scan(&p.Time, &value, &delta, &p.ObservedSeconds); err != nil {
				rows.Close()
				return Series{}, err
			}
			p.Last = pointer(value)
			p.Value = p.Last
			p.Count = 1
			p.Delta = pointer(delta)
			if delta.Valid {
				p.DeltaCount = 1
			}
			if d.Kind != "counter" {
				p.Min = p.Last
				p.Max = p.Last
				p.Avg = p.Last
			}
			values[p.Time] = p
		}
	} else {
		rows, err = tx.Query("SELECT t_s,min,max,avg,last,count,delta,delta_count,duration_s FROM metric_hours WHERE scope=? AND app_id=? AND metric=? AND t_s>=? AND t_s<? ORDER BY t_s", scope, app, key, from, to)
		if err != nil {
			return Series{}, err
		}
		for rows.Next() {
			var p Point
			var min, max, avg, last, delta sql.NullFloat64
			if err = rows.Scan(&p.Time, &min, &max, &avg, &last, &p.Count, &delta, &p.DeltaCount, &p.ObservedSeconds); err != nil {
				rows.Close()
				return Series{}, err
			}
			p.Min = pointer(min)
			p.Max = pointer(max)
			p.Avg = pointer(avg)
			p.Last = pointer(last)
			p.Delta = pointer(delta)
			p.Value = p.Avg
			if d.Kind == "counter" {
				p.Value = p.Last
			}
			values[p.Time] = p
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return Series{}, err
	}
	rows.Close()
	if resolution == time.Hour {
		var watermark int64
		if err = tx.QueryRow("SELECT aggregated_before_s FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
			return Series{}, err
		}
		start := watermark
		if start < from {
			start = from
		}
		rows, err = tx.Query(hourlySelect(d.Kind, true), at.Unix()+1, key, start, at.Unix()+1, scope, app)
		if err != nil {
			return Series{}, err
		}
		for rows.Next() {
			var p Point
			var metric, metricScope, metricApp string
			var min, max, avg, last, delta sql.NullFloat64
			if err = rows.Scan(&metricScope, &metricApp, &metric, &p.Time, &min, &max, &avg, &last, &p.Count, &delta, &p.DeltaCount, &p.ObservedSeconds); err != nil {
				rows.Close()
				return Series{}, err
			}
			p.Min = pointer(min)
			p.Max = pointer(max)
			p.Avg = pointer(avg)
			p.Last = pointer(last)
			p.Delta = pointer(delta)
			p.Value = p.Avg
			if d.Kind == "counter" {
				p.Value = p.Last
			}
			values[p.Time] = p
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return Series{}, err
		}
		rows.Close()
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
	if err = tx.Commit(); err != nil {
		return Series{}, err
	}
	return series, nil
}
