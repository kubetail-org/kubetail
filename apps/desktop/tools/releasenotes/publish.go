package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// publishCmd prints the notes, asks on the terminal and, on a yes, writes them
// to the tag's release. The answer is read from the terminal, never from
// standard input, so a program cannot pipe in a yes.
func (t tool) publishCmd(ctx context.Context, tag, path string, out io.Writer) error {
	md, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tty, err := t.terminal()
	if err != nil {
		return fmt.Errorf("publish asks on a terminal and found none (%v); run it in a terminal of your own", err)
	}
	defer tty.Close()

	fmt.Fprintf(out, "%s\n---\nWrite these notes to the %s release on %s? [y/N] ", md, tag, t.repo)
	answer, _ := bufio.NewReader(tty).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return errors.New("not confirmed; nothing written")
	}
	if err := t.gh(ctx, nil, "release", "edit", tag, "--notes-file", path); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Wrote the notes to %s. Read the draft on GitHub, then publish it.\n", tag)
	return err
}
