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
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTableWriter(t *testing.T) {
	tests := []struct {
		name          string
		defaultWidths []int
		options       []Option
		wantNumCols   int
		wantWidths    []int
		wantSeparator string
		wantBuffer    int
	}{
		{
			name:          "with default widths",
			defaultWidths: []int{10, 15, 20},
			options:       nil,
			wantNumCols:   3,
			wantWidths:    []int{10, 15, 20},
			wantSeparator: "",
			wantBuffer:    3,
		},
		{
			name:          "with custom separator",
			defaultWidths: []int{5, 10},
			options:       []Option{WithSeparator(" - ")},
			wantNumCols:   2,
			wantWidths:    []int{5, 10},
			wantSeparator: " - ",
			wantBuffer:    3,
		},
		{
			name:          "with custom buffer",
			defaultWidths: []int{5, 10},
			options:       []Option{WithColumnBuffer(5)},
			wantNumCols:   2,
			wantWidths:    []int{5, 10},
			wantSeparator: "",
			wantBuffer:    5,
		},
		{
			name:          "with empty default widths",
			defaultWidths: []int{},
			options:       nil,
			wantNumCols:   0,
			wantWidths:    []int{},
			wantSeparator: "",
			wantBuffer:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := NewTableWriter(&buf, tt.defaultWidths, tt.options...)

			assert.Equal(t, tt.wantNumCols, tw.numCols)
			assert.Equal(t, tt.wantWidths, tw.colWidths)
			assert.Equal(t, tt.wantSeparator, tw.separatorStr)
			assert.Equal(t, tt.wantBuffer, tw.columnBuffer)
		})
	}
}

func TestWithOptions(t *testing.T) {
	var buf bytes.Buffer
	alignments := []Alignment{AlignLeft, AlignCenter, AlignRight}

	tw := NewTableWriter(&buf, []int{10, 10, 10},
		WithHeaderStyle(true),
		WithSeparator(" | "),
		WithAlignments(alignments),
		WithColumnBuffer(2),
	)

	assert.True(t, tw.headerStyle)
	assert.Equal(t, " | ", tw.separatorStr)
	assert.Equal(t, alignments, tw.alignments)
	assert.Equal(t, 2, tw.columnBuffer)

	// Verify that alignments are copied, not referenced
	alignments[0] = AlignRight
	assert.NotEqual(t, AlignRight, tw.alignments[0])
}

func TestEnsureColumnCapacity(t *testing.T) {
	tests := []struct {
		name          string
		initialWidths []int
		initialAligns []Alignment
		inputRow      []string
		wantWidths    []int
		wantNumCols   int
		wantRow       []string
	}{
		{
			name:          "initialize from first row",
			initialWidths: []int{},
			initialAligns: []Alignment{},
			inputRow:      []string{"a", "bb", "ccc"},
			wantWidths:    []int{1, 2, 0}, // Last column width should be 0 (not set)
			wantNumCols:   3,
			wantRow:       []string{"a", "bb", "ccc"},
		},
		{
			name:          "expand row with empty cells",
			initialWidths: []int{10, 10, 10},
			initialAligns: []Alignment{AlignLeft, AlignCenter, AlignRight},
			inputRow:      []string{"a"},
			wantWidths:    []int{10, 10, 10}, // No change to existing widths
			wantNumCols:   3,
			wantRow:       []string{"a", "", ""},
		},
		{
			name:          "expand capacity for wider row",
			initialWidths: []int{5, 5},
			initialAligns: []Alignment{AlignLeft, AlignCenter},
			inputRow:      []string{"a", "bb", "ccc", "dddd"},
			wantWidths:    []int{5, 5, 3, 0}, // Last column width should be 0 (not set)
			wantNumCols:   4,
			wantRow:       []string{"a", "bb", "ccc", "dddd"},
		},
		{
			name:          "update widths based on content",
			initialWidths: []int{1, 1, 1},
			initialAligns: []Alignment{AlignLeft, AlignCenter, AlignRight},
			inputRow:      []string{"abc", "defg", "hi"},
			wantWidths:    []int{3, 4, 1}, // Last column width should not be updated
			wantNumCols:   3,
			wantRow:       []string{"abc", "defg", "hi"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			var options []Option
			if len(tt.initialAligns) > 0 {
				options = append(options, WithAlignments(tt.initialAligns))
			}

			tw := NewTableWriter(&buf, tt.initialWidths, options...)
			result := tw.ensureColumnCapacity(tt.inputRow)

			assert.Equal(t, tt.wantRow, result)
			assert.Equal(t, tt.wantNumCols, tw.numCols)
			assert.Equal(t, tt.wantWidths, tw.colWidths)
		})
	}
}

func TestFormatCell(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		width        int
		alignment    Alignment
		isLastColumn bool
		want         string
	}{
		{
			name:         "left align with padding",
			content:      "abc",
			width:        5,
			alignment:    AlignLeft,
			isLastColumn: false,
			want:         "abc  ",
		},
		{
			name:         "right align with padding",
			content:      "abc",
			width:        5,
			alignment:    AlignRight,
			isLastColumn: false,
			want:         "  abc",
		},
		{
			name:         "center align with even padding",
			content:      "abc",
			width:        7,
			alignment:    AlignCenter,
			isLastColumn: false,
			want:         "  abc  ",
		},
		{
			name:         "center align with odd padding",
			content:      "abc",
			width:        6,
			alignment:    AlignCenter,
			isLastColumn: false,
			want:         " abc  ",
		},
		{
			name:         "no padding needed",
			content:      "abcdef",
			width:        5,
			alignment:    AlignLeft,
			isLastColumn: false,
			want:         "abcdef",
		},
		{
			name:         "last column - no fixed width",
			content:      "abcdef",
			width:        3,
			alignment:    AlignLeft,
			isLastColumn: true,
			want:         "abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := NewTableWriter(&buf, []int{tt.width}, WithAlignments([]Alignment{tt.alignment}))

			got := tw.formatCell(tt.content, 0, tt.isLastColumn)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPrintHeader(t *testing.T) {
	tests := []struct {
		name         string
		header       []string
		widths       []int
		separator    string
		wantContains []string
	}{
		{
			name:         "basic header",
			header:       []string{"Name", "Age", "City"},
			widths:       []int{10, 5, 10},
			separator:    "",
			wantContains: []string{"Name", "Age", "City"},
		},
		{
			name:         "custom separator",
			header:       []string{"Col1", "Col2"},
			widths:       []int{5, 5},
			separator:    " - ",
			wantContains: []string{"Col1", "Col2", "-"},
		},
		{
			name:         "initialize from header",
			header:       []string{"Header1", "Header2", "Header3"},
			widths:       []int{},
			separator:    "",
			wantContains: []string{"Header1", "Header2", "Header3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := NewTableWriter(&buf, tt.widths, WithSeparator(tt.separator))

			err := tw.PrintHeader(tt.header)
			assert.NoError(t, err)

			got := buf.String()
			for _, wantStr := range tt.wantContains {
				assert.Contains(t, got, wantStr)
			}

			// Check that we have one line (header)
			lines := strings.Split(strings.TrimSpace(got), "\n")
			assert.Equal(t, 1, len(lines))
		})
	}
}

func TestWriteRow(t *testing.T) {
	tests := []struct {
		name         string
		row          []string
		widths       []int
		alignments   []Alignment
		separator    string
		wantContains []string
	}{
		{
			name:         "basic row",
			row:          []string{"John", "30", "New York"},
			widths:       []int{10, 5, 10},
			alignments:   []Alignment{AlignLeft, AlignRight, AlignCenter},
			separator:    "",
			wantContains: []string{"John", "30", "New York"},
		},
		{
			name:         "row with fewer cells",
			row:          []string{"Data1"},
			widths:       []int{10, 10, 10},
			alignments:   []Alignment{AlignLeft, AlignLeft, AlignLeft},
			separator:    "",
			wantContains: []string{"Data1"},
		},
		{
			name:         "row with more cells",
			row:          []string{"A", "B", "C", "D"},
			widths:       []int{1, 1},
			alignments:   []Alignment{AlignLeft, AlignLeft},
			separator:    "",
			wantContains: []string{"A", "B", "C", "D"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := NewTableWriter(&buf, tt.widths,
				WithSeparator(tt.separator),
				WithAlignments(tt.alignments),
			)

			err := tw.WriteRow(tt.row)
			assert.NoError(t, err)

			got := buf.String()
			for _, wantStr := range tt.wantContains {
				assert.Contains(t, got, wantStr)
			}
		})
	}
}

func TestWriteRows(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTableWriter(&buf, []int{10, 5, 10},
		WithSeparator(" | "),
		WithAlignments([]Alignment{AlignLeft, AlignRight, AlignCenter}),
	)

	rows := [][]string{
		{"John", "30", "New York"},
		{"Alice", "25", "Boston"},
		{"Bob", "40", "Chicago"},
	}

	err := tw.WriteRows(rows)
	assert.NoError(t, err)

	got := buf.String()
	assert.Contains(t, got, "John")
	assert.Contains(t, got, "Alice")
	assert.Contains(t, got, "Bob")
}

func TestPrintSeparator(t *testing.T) {
	tests := []struct {
		name         string
		widths       []int
		separator    string
		buffer       int
		wantContains []string
	}{
		{
			name:         "default separator",
			widths:       []int{10, 5, 10},
			separator:    "",
			buffer:       3,
			wantContains: []string{"----------", "-----", "-"}, // Last column has minimal separator
		},
		{
			name:         "custom separator",
			widths:       []int{5, 5},
			separator:    " - ",
			buffer:       3,
			wantContains: []string{"-----", "-", "---"}, // Last column has minimal separator, separator has dashes
		},
		{
			name:         "custom buffer",
			widths:       []int{5, 5},
			separator:    "",
			buffer:       5,
			wantContains: []string{"-----", "-"}, // Last column has minimal separator
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := NewTableWriter(&buf, tt.widths,
				WithSeparator(tt.separator),
				WithColumnBuffer(tt.buffer),
			)

			// Initialize columns
			tw.WriteRow(make([]string, len(tt.widths)))
			buf.Reset() // Clear the buffer

			err := tw.PrintSeparator()
			assert.NoError(t, err)

			got := buf.String()
			for _, wantStr := range tt.wantContains {
				assert.Contains(t, got, wantStr)
			}
			assert.True(t, strings.HasSuffix(got, "\n"))
		})
	}
}

func TestIntegration(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTableWriter(&buf, nil,
		WithSeparator(" | "),
		WithAlignments([]Alignment{AlignLeft, AlignRight, AlignCenter}),
		WithColumnBuffer(1),
	)

	// Print header
	err := tw.PrintHeader([]string{"Name", "Age", "City"})
	assert.NoError(t, err)

	// Write rows
	rows := [][]string{
		{"John", "30", "New York"},
		{"Alice", "25", "Boston"},
		{"Bob", "40", "Chicago"},
	}

	err = tw.WriteRows(rows)
	assert.NoError(t, err)

	// Print separator
	err = tw.PrintSeparator()
	assert.NoError(t, err)

	// Write a final row
	err = tw.WriteRow([]string{"Total", "95", ""})
	assert.NoError(t, err)

	got := buf.String()

	// Check for header content
	assert.Contains(t, got, "Name")
	assert.Contains(t, got, "Age")
	assert.Contains(t, got, "City")

	// Check for row data
	assert.Contains(t, got, "John")
	assert.Contains(t, got, "Alice")
	assert.Contains(t, got, "Bob")

	// Check for final row
	assert.Contains(t, got, "Total")
	assert.Contains(t, got, "95")
}
