// Copyright 2024-2025 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tablewriter

import (
	"fmt"
	"io"
	"strings"
)

// Alignment represents text alignment within a table cell.
type Alignment int

const (
	// AlignLeft aligns text to the left of the cell.
	AlignLeft Alignment = iota
	// AlignCenter centers text within the cell.
	AlignCenter
	// AlignRight aligns text to the right of the cell.
	AlignRight
)

// TableWriter writes table rows with dynamic column widths.
// It writes each row immediately to the underlying writer.
type TableWriter struct {
	writer       io.Writer
	colWidths    []int
	numCols      int
	alignments   []Alignment
	headerStyle  bool
	separatorStr string
	columnBuffer int
}

// Option represents a functional option for configuring TableWriter.
type Option func(*TableWriter)

// WithHeaderStyle configures whether to print headers in a distinct style.
func WithHeaderStyle(enabled bool) Option {
	return func(tw *TableWriter) {
		tw.headerStyle = enabled
	}
}

// WithSeparator sets the separator string used between columns.
func WithSeparator(sep string) Option {
	return func(tw *TableWriter) {
		tw.separatorStr = sep
	}
}

// WithColumnBuffer sets the number of spaces to buffer between columns.
func WithColumnBuffer(spaces int) Option {
	return func(tw *TableWriter) {
		if spaces >= 0 {
			tw.columnBuffer = spaces
		}
	}
}

// WithAlignments sets the alignment for each column.
func WithAlignments(alignments []Alignment) Option {
	return func(tw *TableWriter) {
		// Create a copy so we don't modify the caller's slice
		alignsCopy := make([]Alignment, len(alignments))
		copy(alignsCopy, alignments)
		tw.alignments = alignsCopy
	}
}

// NewTableWriter creates a new TableWriter.
// If defaultWidths is nil or empty, the writer will initialize its columns based on the first row/header.
// Options can be provided to customize the table writer's behavior.
func NewTableWriter(w io.Writer, defaultWidths []int, options ...Option) *TableWriter {
	// Create a copy so we don't modify the caller's slice
	widthsCopy := make([]int, len(defaultWidths))
	copy(widthsCopy, defaultWidths)

	tw := &TableWriter{
		writer:       w,
		colWidths:    widthsCopy,
		numCols:      len(widthsCopy),
		separatorStr: "",
		columnBuffer: 3,
	}

	// Apply options
	for _, option := range options {
		option(tw)
	}

	return tw
}

// ensureColumnCapacity ensures that the TableWriter has enough capacity for the given row.
// It expands the internal slices if necessary and returns the updated row.
func (tw *TableWriter) ensureColumnCapacity(row []string) []string {
	// Initialize if no columns have been set
	if tw.numCols == 0 {
		tw.numCols = len(row)
		tw.colWidths = make([]int, tw.numCols)
		if len(tw.alignments) == 0 {
			tw.alignments = make([]Alignment, tw.numCols)
		}
	}

	// Expand row if it has fewer cells than expected
	if len(row) < tw.numCols {
		missing := tw.numCols - len(row)
		newRow := make([]string, len(row), tw.numCols)
		copy(newRow, row)
		for i := 0; i < missing; i++ {
			newRow = append(newRow, "")
		}
		row = newRow
	}

	// If row has more cells than expected, expand internal slices
	if len(row) > tw.numCols {
		additional := len(row) - tw.numCols
		tw.colWidths = append(tw.colWidths, make([]int, additional)...)

		// Expand alignments if they exist
		if len(tw.alignments) > 0 {
			tw.alignments = append(tw.alignments, make([]Alignment, additional)...)
		}

		tw.numCols = len(row)
	}

	// Update column widths based on cell content, except for the last column
	for i, cell := range row {
		if i < tw.numCols-1 && len(cell) > tw.colWidths[i] {
			tw.colWidths[i] = len(cell)
		}
	}

	return row
}

// formatCell formats a cell's content according to its width and alignment.
// If isLastColumn is true, the content is returned as-is without fixed width formatting.
func (tw *TableWriter) formatCell(content string, colIndex int, isLastColumn bool) string {
	// For the last column, return content as-is without fixed width
	if isLastColumn {
		return content
	}

	width := tw.colWidths[colIndex]

	// If no alignments specified or index out of range, default to left alignment
	alignment := AlignLeft
	if colIndex < len(tw.alignments) {
		alignment = tw.alignments[colIndex]
	}

	contentLen := len(content)
	padding := width - contentLen

	if padding <= 0 {
		return content // No padding needed
	}

	switch alignment {
	case AlignRight:
		return strings.Repeat(" ", padding) + content
	case AlignCenter:
		leftPad := padding / 2
		rightPad := padding - leftPad
		return strings.Repeat(" ", leftPad) + content + strings.Repeat(" ", rightPad)
	default: // AlignLeft
		return content + strings.Repeat(" ", padding)
	}
}

// PrintHeader prints a header row with a separator line below it.
// It initializes column configuration if not already set.
func (tw *TableWriter) PrintHeader(header []string) error {
	header = tw.ensureColumnCapacity(header)

	// Build the header row
	var parts []string
	for i, cell := range header {
		isLastColumn := (i == tw.numCols-1)
		formattedCell := tw.formatCell(cell, i, isLastColumn)
		// Add column buffer if this isn't the last column
		if i < tw.numCols-1 && tw.columnBuffer > 0 {
			formattedCell += strings.Repeat(" ", tw.columnBuffer)
		}
		parts = append(parts, formattedCell)
	}
	headerLine := strings.Join(parts, tw.separatorStr) + "\n"

	// Write header row
	if _, err := tw.writer.Write([]byte(headerLine)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	return nil
}

// WriteRow writes a single row to the writer.
// It initializes the column configuration if needed and expands columns if the row has more cells.
func (tw *TableWriter) WriteRow(row []string) error {
	row = tw.ensureColumnCapacity(row)

	// Build the formatted row
	var parts []string
	for i, cell := range row {
		isLastColumn := (i == tw.numCols-1)
		formattedCell := tw.formatCell(cell, i, isLastColumn)
		// Add column buffer if this isn't the last column
		if i < tw.numCols-1 && tw.columnBuffer > 0 {
			formattedCell += strings.Repeat(" ", tw.columnBuffer)
		}
		parts = append(parts, formattedCell)
	}
	line := strings.Join(parts, tw.separatorStr) + "\n"

	// Write the line immediately
	_, err := tw.writer.Write([]byte(line))
	if err != nil {
		return fmt.Errorf("failed to write row: %w", err)
	}

	return nil
}

// WriteRows writes multiple rows to the writer.
// This is a convenience method that calls WriteRow for each row.
func (tw *TableWriter) WriteRows(rows [][]string) error {
	for _, row := range rows {
		if err := tw.WriteRow(row); err != nil {
			return err
		}
	}
	return nil
}

// PrintSeparator prints a separator line.
func (tw *TableWriter) PrintSeparator() error {
	var sepParts []string
	for i := 0; i < tw.numCols; i++ {
		width := tw.colWidths[i]
		// Add column buffer width to the separator if this isn't the last column
		if i < tw.numCols-1 && tw.columnBuffer > 0 {
			width += tw.columnBuffer
		}
		
		// For the last column, use a minimal separator (just one dash)
		// This allows the last column to have variable width
		if i == tw.numCols-1 {
			sep := "-"
			sepParts = append(sepParts, sep)
		} else {
			sep := strings.Repeat("-", width)
			sepParts = append(sepParts, sep)
		}
	}

	// Create separator that matches the column separator
	colSepReplacement := ""
	if len(tw.separatorStr) > 0 {
		colSepReplacement = strings.Repeat("-", len(tw.separatorStr))
	}

	sepLine := strings.Join(sepParts, colSepReplacement) + "\n"

	_, err := tw.writer.Write([]byte(sepLine))
	if err != nil {
		return fmt.Errorf("failed to write separator: %w", err)
	}

	return nil
}
