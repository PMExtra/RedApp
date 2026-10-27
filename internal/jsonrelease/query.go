package jsonrelease

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
	"github.com/itchyny/gojq"
)

// Runner embeds gojq for trusted administrator configuration. It is NOT a
// memory sandbox: intermediate allocations can exceed the input/output limits.
// No module loader, input iterator, environment or custom I/O functions are
// installed. All HTTP work remains in Discoverer.
type Runner struct{ slots chan struct{} }

func NewRunner() *Runner { return &Runner{slots: make(chan struct{}, 2)} }

func (r *Runner) Run(parent context.Context, expression string, input json.RawMessage) (json.RawMessage, error) {
	if len(expression) == 0 || len(expression) > releaseconfig.MaxQueryBytes || len(input) > releaseconfig.MaxMetadataBytes {
		return nil, ErrLimit
	}
	if !utf8.ValidString(expression) || !utf8.Valid(input) || jsoncheck.Unique(input) != nil {
		return nil, errors.New("invalid query input")
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r.slots <- struct{}{}:
	}
	defer func() { <-r.slots }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, err
	}
	code, err := gojq.Compile(query, gojq.WithEnvironLoader(func() []string { return nil }))
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err = decoder.Decode(&value); err != nil {
		return nil, err
	}
	iterator := code.RunWithContext(ctx, value)
	result, ok := iterator.Next()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("query must produce exactly one release object")
	}
	if err, ok := result.(error); ok {
		return nil, err
	}
	output := &limitedJSONWriter{limit: releaseconfig.MaxOutputBytes, ctx: ctx}
	if err = json.NewEncoder(output).Encode(result); err != nil {
		return nil, err
	}
	if next, ok := iterator.Next(); ok {
		if err, ok := next.(error); ok {
			return nil, err
		}
		return nil, errors.New("query produced multiple results")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(output.Bytes()), nil
}

type limitedJSONWriter struct {
	bytes.Buffer
	limit int
	ctx   context.Context
}

func (w *limitedJSONWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > w.limit-w.Len() {
		return 0, ErrLimit
	}
	return w.Buffer.Write(p)
}
