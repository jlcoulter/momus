// Package bulk generates FHIR Bulk Data (newline-delimited JSON, NDJSON)
// streams from generated Datasets. In NDJSON bulk data each line is a single
// JSON-encoded FHIR resource, matching the format produced by the FHIR Bulk
// Data Access API ($export).
package bulk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

// Writer serialises a collection of generated resources as NDJSON bulk data.
// It is safe to write instances from multiple datasets; a single Writer
// produces a single concatenated NDJSON stream.
type Writer struct {
	w *bufio.Writer
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
	if _, err := wr.w.Write(line); err != nil {
		return err
	}
	return wr.w.WriteByte('\n')
}

// WriteDataset writes every resource in ds as an NDJSON line. Ordering is
// deterministic (by local ID) so the same dataset always yields the same byte
// stream regardless of map iteration order.
func (wr *Writer) WriteDataset(ds *model.Dataset) error {
	if ds == nil {
		return errors.New("bulk: dataset is required")
	}
	ids := make([]string, 0, len(ds.Resources))
	for id := range ds.Resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := wr.WriteInstance(ds.Resources[id]); err != nil {
			return err
		}
	}
	return nil
}

// WriteDatasets writes every resource across all datasets in order.
func (wr *Writer) WriteDatasets(datasets []*model.Dataset) error {
	for _, ds := range datasets {
		if err := wr.WriteDataset(ds); err != nil {
			return err
		}
	}
	return nil
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

// Close flushes pending output to the underlying writer.
func (wr *Writer) Close() error {
	return wr.w.Flush()
}

// EncodeDataset serialises ds to NDJSON bytes using the same deterministic
// ordering as WriteDataset. It is a convenience for callers that want an
// in-memory buffer rather than streaming.
func EncodeDataset(ds *model.Dataset) ([]byte, error) {
	if ds == nil {
		return nil, errors.New("bulk: dataset is required")
	}
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteDataset(ds); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Count reports the total number of resources across datasets.
func Count(datasets []*model.Dataset) int {
	total := 0
	for _, ds := range datasets {
		if ds != nil {
			total += len(ds.Resources)
		}
	}
	return total
}
