package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// runLcov writes the lcov report in to out without the records on lines a
// range marker exempts. A report's SF paths are read from disk, as cargo
// llvm-cov writes them.
func runLcov(in, out string) error {
	r, err := os.Open(in)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := filterLcov(r, f, rustExemptLines); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// filterLcov copies the report from r to w, dropping the FN, FNDA, BRDA and DA
// records on the lines exempt returns for the record's file, and recounting
// the totals (FNF/FNH, BRF/BRH, LF/LH) that follow each file's records.
func filterLcov(r io.Reader, w io.Writer, exempt func(path string) (map[int]bool, error)) error {
	bw := bufio.NewWriter(w)
	sc := bufio.NewScanner(r)

	var lines map[int]bool       // the current file's exempt lines
	dropped := map[string]bool{} // its dropped functions, whose FNDA records go too
	totals := map[string]int{}   // its totals so far, by record kind
	for sc.Scan() {
		line := sc.Text()
		kind, value, _ := strings.Cut(line, ":")
		keep := true
		switch kind {
		case "SF":
			var err error
			if lines, err = exempt(value); err != nil {
				return err
			}
			dropped = map[string]bool{}
			totals = map[string]int{}
		case "FN": // FN:<line>,<name>
			at, name, err := record(line, value)
			if err != nil {
				return err
			}
			keep = !lines[at]
			if keep {
				totals["FNF"]++
			} else {
				dropped[name] = true
			}
		case "FNDA": // FNDA:<count>,<name>
			count, name, err := record(line, value)
			if err != nil {
				return err
			}
			keep = !dropped[name]
			if keep && count > 0 {
				totals["FNH"]++
			}
		case "BRDA": // BRDA:<line>,<block>,<branch>,<taken>; taken is "-" when the line never ran
			at, rest, err := record(line, value)
			if err != nil {
				return err
			}
			taken := rest[strings.LastIndex(rest, ",")+1:]
			keep = !lines[at]
			if keep {
				totals["BRF"]++
				if taken != "-" && taken != "0" {
					totals["BRH"]++
				}
			}
		case "DA": // DA:<line>,<count>[,<checksum>]
			at, rest, err := record(line, value)
			if err != nil {
				return err
			}
			count, _, _ := strings.Cut(rest, ",")
			keep = !lines[at]
			if keep {
				totals["LF"]++
				if count != "0" {
					totals["LH"]++
				}
			}
		case "FNF", "FNH", "BRF", "BRH", "LF", "LH":
			line = fmt.Sprintf("%s:%d", kind, totals[kind])
		}
		if keep {
			fmt.Fprintln(bw, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return bw.Flush()
}

// record splits a value of the form <number>,<rest>.
func record(line, value string) (int, string, error) {
	first, rest, _ := strings.Cut(value, ",")
	n, err := strconv.Atoi(first)
	if err != nil {
		return 0, "", fmt.Errorf("%q: %w", line, err)
	}
	return n, rest, nil
}

// rustExemptLines is the set of lines the Rust file's range markers exempt.
// A marker is read on a line of its own, whatever its indentation.
func rustExemptLines(path string) (map[int]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	comments := make([]comment, len(lines))
	for i, text := range lines {
		comments[i] = comment{i + 1, strings.TrimSpace(text)}
	}
	return rangeLines(comments, len(lines)), nil
}
