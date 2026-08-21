// Package bulk generates FHIR Bulk Data (newline-delimited JSON, NDJSON)
// streams from generated Datasets. In NDJSON bulk data each line is a single
// JSON-encoded FHIR resource, matching the format produced by the FHIR Bulk
// Data Access API ($export).
package bulk

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// Writer serialises a collection of generated resources as NDJSON bulk data.
// It is safe to write instances from multiple datasets; a single Writer
// produces a single concatenated NDJSON stream. A Writer is safe for
// concurrent use: writes are serialised by an internal mutex.
type Writer struct {
	mu     sync.Mutex
	w      *bufio.Writer
	closed bool
}

// NewWriter returns a Writer that writes NDJSON lines to w. The caller must
// call Close to flush buffered output.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: bufio.NewWriter(w)}
}

// WriteInstance writes a single resource as one NDJSON line.
func (wr *Writer) WriteInstance(inst *model.ResourceInstance) error {
	if inst == nil || inst.Resource == nil {
		return errors.New("bulk: resource instance is required")
	}
	line, err := json.Marshal(inst.Resource)
	if err != nil {
		return fmt.Errorf("bulk: marshal resource %s/%s: %w", inst.ResourceType, inst.LocalID, err)
	}
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if _, err := wr.w.Write(line); err != nil {
		return err
	}
	return wr.w.WriteByte('\n')
}

// WriteInstances writes the given resource instances as NDJSON lines, in
// order.
func (wr *Writer) WriteInstances(instances []*model.ResourceInstance) error {
	for _, inst := range instances {
		if err := wr.WriteInstance(inst); err != nil {
			return err
		}
	}
	return nil
}

// Close flushes pending output to the underlying writer. It is safe to call
// multiple times; subsequent calls are no-ops.
func (wr *Writer) Close() error {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if wr.closed {
		return nil
	}
	wr.closed = true
	return wr.w.Flush()
}
