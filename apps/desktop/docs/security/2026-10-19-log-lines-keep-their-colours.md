# Security record — Log lines keep their colours, 19 October 2026

**Subject:** the sidecar keeps a log line's SGR sequences (colour and text style) and the viewer
draws them. `keepColor` (`services/logs/clean.go`) is what the log file and the wire to the viewer
hold; `stripControl` is still what the model, grep and a refusal's message read. The viewer turns
the sequences into styled runs with fancy-ansi (`components/widgets/ansi-text.tsx`). The living
model is [security-model.md](../security-model.md).

## What widens

**An escape sequence reaches the webview.** One form of control now leaves the sidecar in a log
line: `ESC [`, parameters of at most four digits each separated by semicolons, `m`. The 16 KiB
cut counts the kept sequences, so a heavily coloured line keeps less of its text.

**The webview parses HTML.** fancy-ansi's output is an HTML string. `ansiRuns` parses it in an inert
`DOMParser` document and reads back each span's text and inline style; nothing is inserted as HTML.

**Colour can hide text.** A foreground on a matching background, or dim text, hides what a line
says from the eye, though not from copy, grep or the model.

## What holds it

- **Only the SGR form fancy-ansi both draws and strips is kept.** Every other CSI, OSC, C1 and C0
  control but tab is stripped as before, and the line is still cut at 16 KiB
  (`TestKeepColorKeepsOnlySGR`, `TestKeepColorCutsBeforeASequence`, `TestALineIsCleaned`). A
  parameter longer than four digits is dropped: fancy-ansi's parser would draw it, but its
  `stripAnsi` would leave it in copied text (`ansi-text.test.tsx`: `reads the same text stripAnsi
  leaves`).
- **The model never sees a sequence.** `ForModel` strips and redacts as before
  (`TestForModelStripsControlSequences`).
- **Grep matches the text, not the sequences** (`TestGrepMatchesAKeptLinesText`,
  `TestAGrepMatchesTheStoredText`).
- **Text never becomes an element.** fancy-ansi escapes the text before the document parses it, so
  the document holds flat spans whose styles come from fancy-ansi's table and the sequence's
  digits. Each run's text is drawn through `VisibleText` (`ansi-text.test.tsx`: `reads markup in
  the text as text`, `draws no element from markup in the text`, `spells out what cluster text
  could hide inside a colour`; `row.test.tsx`: `draws a message with its colours`).
- **Concealed text (SGR 8) is drawn.** `visibility` is dropped (`never hides text`).
- **Copy is plain text.** A row's, a cell's and *Copy message* write the line without its
  sequences, so nothing pasted into a terminal carries one (`selection.test.tsx`: `returns the
  message without its colours`).

## Residuals

- **Colour can still hide text from the eye**, a foreground on a matching background or dim text.
  Dropping the background would not close it: a 24-bit foreground can match the theme's own
  background. Open in [TODO](../TODO.md#security).
- **fancy-ansi's parser is on the path of cluster text.**
