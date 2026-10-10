package store

import (
	"database/sql"
	"errors"
	"time"
)

// MetricKind selects how minute samples aggregate into hourly buckets.
type MetricKind string

const (
	MetricGauge   MetricKind = "gauge"
	MetricCounter MetricKind = "counter"
	MetricRate    MetricKind = "rate"
)

// MetricDefinition is one catalog metric as far as storage is concerned.
type MetricDefinition struct {
	Key  string
	Kind MetricKind
}

// MetricSample is one validated observation. Scope is "global" (AppID empty)
// or "app" (AppID is the application's metrics namespace).
type MetricSample struct {
	Scope string
	AppID string
	Key   string
	Kind  MetricKind
	Value float64
}

// MetricBucket is one minute sample or one hourly aggregate. Minute samples
// set Last, Count=1 and, for counters with a usable predecessor, Delta.
type MetricBucket struct {
	Time            int64
	Min, Max, Avg   *float64
	Last            *float64
	Count           int
	Delta           *float64
	DeltaCount      int
	ObservedSeconds float64
}

// ErrMetricClock rejects observations older than committed hourly aggregates.
var ErrMetricClock = errors.New("metrics clock moved behind committed hourly aggregates")

// rateWindowSeconds is the observation window of every rate sample.
const rateWindowSeconds = 5

// RecordMetrics stores samples taken at at by the process identified by boot
// and runs MaintainMetrics in the same transaction, so a failure cannot
// delete raw samples whose hourly aggregates were not committed. Samples of
// applications that no longer exist are dropped.
func (s *Store) RecordMetrics(at time.Time, boot string, samples []MetricSample, catalog []MetricDefinition) error {
	tx, err := s.db.Begin()
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
		return ErrMetricClock
	}
	exists := map[string]bool{}
	for _, sample := range samples {
		if sample.AppID != "" {
			live, ok := exists[sample.AppID]
			if !ok {
				if live, err = applicationNamespaceExists(tx, sample.AppID); err != nil {
					return err
				}
				exists[sample.AppID] = live
			}
			if !live {
				continue
			}
		}
		duration := 0.0
		var delta *float64
		switch sample.Kind {
		case MetricCounter:
			var previous float64
			var previousAt, previousMinute int64
			var previousBoot string
			err = tx.QueryRow("SELECT value,observed_at_s,boot,t_s FROM metric_samples WHERE scope=? AND app_id=? AND metric=? ORDER BY t_s DESC LIMIT 1", sample.Scope, sample.AppID, sample.Key).Scan(&previous, &previousAt, &previousBoot, &previousMinute)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			elapsed := observed - previousAt
			if err == nil && previousBoot == boot && minute-previousMinute == 60 && sample.Value >= previous && elapsed > 0 && elapsed <= 90 {
				difference := sample.Value - previous
				delta = &difference
				duration = float64(elapsed)
			}
		case MetricRate:
			duration = rateWindowSeconds
		}
		if _, err = tx.Exec("INSERT OR IGNORE INTO metric_samples(scope,app_id,metric,t_s,observed_at_s,boot,value,delta,duration_s) VALUES(?,?,?,?,?,?,?,?,?)", sample.Scope, sample.AppID, sample.Key, minute, observed, boot, sample.Value, delta, duration); err != nil {
			return err
		}
	}
	if err = maintainMetrics(tx, at, catalog); err != nil {
		return err
	}
	return tx.Commit()
}

// MaintainMetrics aggregates every closed hour before at and applies the
// retention of minute samples (24 hours) and hourly buckets (30 days).
func (s *Store) MaintainMetrics(at time.Time, catalog []MetricDefinition) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = maintainMetrics(tx, at, catalog); err != nil {
		return err
	}
	return tx.Commit()
}

func maintainMetrics(tx *sql.Tx, at time.Time, catalog []MetricDefinition) error {
	hour := at.UTC().Truncate(time.Hour).Unix()
	var watermark int64
	if err := tx.QueryRow("SELECT aggregated_before_s FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
		return err
	}
	if hour < watermark {
		return ErrMetricClock
	}
	if hour > watermark {
		for _, d := range catalog {
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
func hourlySelect(kind MetricKind, scoped bool) string {
	minExpr, maxExpr, avgExpr := "MIN(value)", "MAX(value)", "AVG(value)"
	if kind == MetricCounter {
		minExpr, maxExpr, avgExpr = "NULL", "NULL", "NULL"
	} else if kind == MetricRate {
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

func floatPointer(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	n := v.Float64
	return &n
}

// MetricMinutes returns the minute samples of one series with from<=t<=to.
func (s *Store) MetricMinutes(scope, app, key string, from, to int64) ([]MetricBucket, error) {
	rows, err := s.read.Query("SELECT t_s,value,delta,duration_s FROM metric_samples WHERE scope=? AND app_id=? AND metric=? AND t_s>=? AND t_s<=? ORDER BY t_s", scope, app, key, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricBucket{}
	for rows.Next() {
		b := MetricBucket{Count: 1}
		var value, delta sql.NullFloat64
		if err = rows.Scan(&b.Time, &value, &delta, &b.ObservedSeconds); err != nil {
			return nil, err
		}
		b.Last = floatPointer(value)
		b.Delta = floatPointer(delta)
		if delta.Valid {
			b.DeltaCount = 1
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MetricHours returns the hourly buckets of one series with from<=t<to. Hours
// the sampler has not aggregated yet are computed from the minute samples
// observed before at, in the same read snapshot.
func (s *Store) MetricHours(scope, app, key string, kind MetricKind, from, to int64, at time.Time) ([]MetricBucket, error) {
	tx, err := s.read.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := []MetricBucket{}
	scan := func(rows *sql.Rows, aggregate bool) error {
		defer rows.Close()
		for rows.Next() {
			var b MetricBucket
			var min, max, avg, last, delta sql.NullFloat64
			var err error
			if aggregate {
				var metricScope, metricApp, metric string
				err = rows.Scan(&metricScope, &metricApp, &metric, &b.Time, &min, &max, &avg, &last, &b.Count, &delta, &b.DeltaCount, &b.ObservedSeconds)
			} else {
				err = rows.Scan(&b.Time, &min, &max, &avg, &last, &b.Count, &delta, &b.DeltaCount, &b.ObservedSeconds)
			}
			if err != nil {
				return err
			}
			b.Min, b.Max, b.Avg, b.Last, b.Delta = floatPointer(min), floatPointer(max), floatPointer(avg), floatPointer(last), floatPointer(delta)
			out = append(out, b)
		}
		return rows.Err()
	}
	rows, err := tx.Query("SELECT t_s,min,max,avg,last,count,delta,delta_count,duration_s FROM metric_hours WHERE scope=? AND app_id=? AND metric=? AND t_s>=? AND t_s<? ORDER BY t_s", scope, app, key, from, to)
	if err != nil {
		return nil, err
	}
	if err = scan(rows, false); err != nil {
		return nil, err
	}
	var watermark int64
	if err = tx.QueryRow("SELECT aggregated_before_s FROM metric_history_state WHERE id=1").Scan(&watermark); err != nil {
		return nil, err
	}
	start := watermark
	if start < from {
		start = from
	}
	rows, err = tx.Query(hourlySelect(kind, true), at.Unix()+1, key, start, at.Unix()+1, scope, app)
	if err != nil {
		return nil, err
	}
	if err = scan(rows, true); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
