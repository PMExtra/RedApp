package history

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"math"
	"time"
)

type History struct {
	db   *sql.DB
	boot string
}

func Open(db *store.Store) (*History, error) {
	_, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS metric_samples(
 metric TEXT NOT NULL,t INTEGER NOT NULL,observed_at INTEGER NOT NULL,boot TEXT NOT NULL,value REAL NOT NULL,delta REAL,duration REAL NOT NULL,PRIMARY KEY(metric,t));
 CREATE INDEX IF NOT EXISTS metric_samples_time ON metric_samples(t);
 CREATE TABLE IF NOT EXISTS metric_hours(
 metric TEXT NOT NULL,t INTEGER NOT NULL,min REAL,max REAL,avg REAL,last REAL,count INTEGER NOT NULL,delta REAL,delta_count INTEGER NOT NULL,duration REAL NOT NULL,PRIMARY KEY(metric,t));
 CREATE INDEX IF NOT EXISTS metric_hours_time ON metric_hours(t);
 CREATE TABLE IF NOT EXISTS metric_history_state(id INTEGER PRIMARY KEY CHECK(id=1),aggregated_before INTEGER NOT NULL);
 INSERT OR IGNORE INTO metric_history_state VALUES(1,0);`)
	if err != nil {
		return nil, err
	}
	var boot [16]byte
	if _, err = rand.Read(boot[:]); err != nil {
		return nil, err
	}
	return &History{db: db.DB, boot: hex.EncodeToString(boot[:])}, nil
}

// Record and maintenance share a transaction. A failure cannot delete raw
// observations whose closed-hour aggregates have not been committed.
func (h *History) Record(at time.Time, metrics []Metric) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	minute := at.UTC().Truncate(time.Minute).Unix()
	observed := at.Unix()
	var watermark int64
	if err = tx.QueryRow("SELECT aggregated_before FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
		return err
	}
	if minute < watermark {
		return errors.New("Metrics clock moved behind committed hourly aggregates")
	}
	for _, metric := range metrics {
		definition, ok := Find(metric.Key)
		if !ok {
			return errors.New("Unknown global metric")
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
			err = tx.QueryRow("SELECT value,observed_at,boot,t FROM metric_samples WHERE metric=? ORDER BY t DESC LIMIT 1", metric.Key).Scan(&previous, &previousAt, &previousBoot, &previousMinute)
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
		if _, err = tx.Exec("INSERT OR IGNORE INTO metric_samples VALUES(?,?,?,?,?,?,?)", metric.Key, minute, observed, h.boot, value, delta, duration); err != nil {
			return err
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
	if err := tx.QueryRow("SELECT aggregated_before FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
		return err
	}
	if hour < watermark {
		return errors.New("Metrics clock moved behind committed hourly aggregates")
	}
	if hour > watermark {
		for _, d := range Definitions() {
			query := `INSERT INTO metric_hours(metric,t,min,max,avg,last,count,delta,delta_count,duration) ` + hourlySelect(d.Kind) + `
 ON CONFLICT(metric,t) DO UPDATE SET min=excluded.min,max=excluded.max,avg=excluded.avg,last=excluded.last,count=excluded.count,delta=excluded.delta,delta_count=excluded.delta_count,duration=excluded.duration`

			if _, err := tx.Exec(query, hour, d.Key, watermark, hour); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("UPDATE metric_history_state SET aggregated_before=? WHERE id=1", hour); err != nil {
			return err
		}
	}
	// UTC bucket-aligned retention includes the leading partial range bucket.
	if _, err := tx.Exec("DELETE FROM metric_samples WHERE t<?", at.UTC().Truncate(time.Minute).Add(-24*time.Hour).Unix()); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM metric_hours WHERE t<?", at.UTC().Truncate(time.Hour).Add(-30*24*time.Hour).Unix())
	return err
}

// hourlySelect also serves read-only queries before the minute sampler has
// committed the most recently closed hour. It never averages counters.
func hourlySelect(kind string) string {
	minExpr, maxExpr, avgExpr := "MIN(value)", "MAX(value)", "AVG(value)"
	if kind == "counter" {
		minExpr, maxExpr, avgExpr = "NULL", "NULL", "NULL"
	} else if kind == "rate" {
		avgExpr = "SUM(value*duration)/NULLIF(SUM(duration),0)"
	}
	return `SELECT metric,(t/3600)*3600,` + minExpr + `,` + maxExpr + `,` + avgExpr + `,
 (SELECT value FROM metric_samples tail WHERE tail.metric=s.metric AND tail.t>=(s.t/3600)*3600 AND tail.t<(s.t/3600)*3600+3600 AND tail.t<? ORDER BY tail.t DESC LIMIT 1),
 COUNT(*),SUM(delta),COUNT(delta),SUM(duration) FROM metric_samples s WHERE metric=? AND t>=? AND t<? GROUP BY metric,(t/3600)*3600`
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
	d, ok := Find(key)
	if !ok {
		return Series{}, errors.New("Unknown global metric")
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
	series := Series{Definition: d, Range: window, ResolutionSeconds: step, From: from, To: at.Unix(), Points: []Point{}}
	tx, err := h.db.Begin()
	if err != nil {
		return Series{}, err
	}
	defer tx.Rollback()
	values := map[int64]Point{}
	var rows *sql.Rows
	if resolution == time.Minute {
		rows, err = tx.Query("SELECT t,value,delta,duration FROM metric_samples WHERE metric=? AND t>=? AND t<=? ORDER BY t", key, from, to)
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
		rows, err = tx.Query("SELECT t,min,max,avg,last,count,delta,delta_count,duration FROM metric_hours WHERE metric=? AND t>=? AND t<? ORDER BY t", key, from, to)
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
		if err = tx.QueryRow("SELECT aggregated_before FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
			return Series{}, err
		}
		start := watermark
		if start < from {
			start = from
		}
		rows, err = tx.Query(hourlySelect(d.Kind), at.Unix()+1, key, start, at.Unix()+1)
		if err != nil {
			return Series{}, err
		}
		for rows.Next() {
			var p Point
			var metric string
			var min, max, avg, last, delta sql.NullFloat64
			if err = rows.Scan(&metric, &p.Time, &min, &max, &avg, &last, &p.Count, &delta, &p.DeltaCount, &p.ObservedSeconds); err != nil {
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
