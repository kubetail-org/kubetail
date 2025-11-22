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

use std::collections::VecDeque;
use std::io::{self, BufRead, BufReader, Read, Seek, SeekFrom};

use memchr::memchr;
use tokio_util::sync::CancellationToken;

use crate::util::format::FileFormat;

const CHUNK_SIZE: usize = 64 * 1024; // 64KB
pub const TRUNCATION_SENTINEL: u8 = 0x1F;
const DOCKER_SENTINEL_ESCAPED: &[u8] = br"\u001F";
const DOCKER_STREAM_MARKER: &[u8] = b"\",\"stream\":\"";
const DOCKER_TAIL_LIMIT: usize = 64 * 1024;

#[derive(Debug)]
pub struct TermReader<R> {
    ctx: CancellationToken,
    inner: R,
}

impl<R: Read> TermReader<R> {
    pub const fn new(ctx: CancellationToken, inner: R) -> Self {
        Self { ctx, inner }
    }
}

impl<R: Read> Read for TermReader<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        // check for termination before each read
        if self.ctx.is_cancelled() {
            // Channel is closed or term signal was sent.
            return Ok(0);
        }
        self.inner.read(buf)
    }
}

#[derive(Debug)]
pub struct LogTrimmerReader<R> {
    input: BufReader<R>,
    // Renamed from truncated_at_bytes
    truncate_at_bytes: usize,
    internal_buf: Vec<u8>,
    pos: usize,
}

impl<R: Read> LogTrimmerReader<R> {
    /// Creates a new LogTrimmerReader.
    /// If `truncate_at_bytes` is 0, truncation is disabled (pass-through mode).
    pub fn new(reader: R, truncate_at_bytes64: u64) -> Self {
        Self {
            input: BufReader::new(reader),
            truncate_at_bytes: truncate_at_bytes64 as usize,
            internal_buf: Vec::with_capacity(4096),
            pos: 0,
        }
    }

    fn refill_buffer(&mut self) -> io::Result<bool> {
        self.internal_buf.clear();
        self.pos = 0;

        let mut found_header = false;
        let mut space_count = 0;
        let mut current_msg_len = 0;

        loop {
            let available = self.input.fill_buf()?;
            if available.is_empty() {
                return Ok(!self.internal_buf.is_empty());
            }

            let mut consumed = 0;
            let mut line_complete = false;
            let mut limit_reached = false;

            for &b in available {
                consumed += 1;
                self.internal_buf.push(b);

                if b == b'\n' {
                    line_complete = true;
                    break;
                }

                if !found_header {
                    if b == b' ' {
                        space_count += 1;
                        // Standard K8s format: <time> <stream> <tag> <message>
                        if space_count == 3 {
                            found_header = true;
                        }
                    }
                } else {
                    current_msg_len += 1;
                    // Only trigger if we exceeded the limit
                    if current_msg_len > self.truncate_at_bytes {
                        limit_reached = true;
                        break;
                    }
                }
            }

            self.input.consume(consumed);

            if limit_reached {
                // 1. Remove the overflowing byte
                self.internal_buf.pop();
                // 2. Add the newline
                self.internal_buf.push(b'\n');

                // 3. Discard the rest of the line (ignoring the return value)
                let _ = self.discard_rest_of_line()?;

                return Ok(true);
            }

            if line_complete {
                return Ok(true);
            }
        }
    }

    /// Helper: Consumes bytes until a newline or EOF, returning the count of bytes discarded.
    fn discard_rest_of_line(&mut self) -> io::Result<usize> {
        let mut total_discarded = 0;

        loop {
            let available = self.input.fill_buf()?;
            if available.is_empty() {
                return Ok(total_discarded);
            }

            if let Some(index) = available.iter().position(|&b| b == b'\n') {
                let bytes_to_consume = index + 1;
                self.input.consume(bytes_to_consume);
                total_discarded += bytes_to_consume;
                return Ok(total_discarded);
            }

            let len = available.len();
            self.input.consume(len);
            total_discarded += len;
        }
    }
}

impl<R: Read> Read for LogTrimmerReader<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        while self.pos >= self.internal_buf.len() {
            let has_more = self.refill_buffer()?;
            if !has_more {
                return Ok(0);
            }
        }

        let available = self.internal_buf.len() - self.pos;
        let to_copy = std::cmp::min(available, buf.len());

        buf[..to_copy].copy_from_slice(&self.internal_buf[self.pos..self.pos + to_copy]);
        self.pos += to_copy;

        Ok(to_copy)
    }
}

/// A reader that returns file content in reverse line order.
/// It implements the `Read` trait so that consumers can use it like any other reader.
pub struct ReverseLineReader<R: Read + Seek> {
    inner: R,
    pos: u64,              // current position in the file
    min_pos: u64,          // lower bound (inclusive)
    buf: Vec<u8>,          // current chunk buffer
    buf_start: usize,      // start index in the buffer (always 0)
    buf_end: usize,        // current valid end index in the buffer
    line_buf: Vec<u8>,     // accumulates bytes for a line spanning chunks (stored in reverse order)
    current_line: Vec<u8>, // the next line (in correct order) waiting to be read
}

impl<R: Read + Seek> ReverseLineReader<R> {
    /// Creates a new ReverseLineReader wrapping a seekable reader.
    pub fn new(mut inner: R, min_pos: u64, max_pos: u64) -> io::Result<Self> {
        let pos = inner.seek(SeekFrom::Start(max_pos))?;
        Ok(Self {
            inner,
            pos,
            min_pos,
            buf: Vec::new(),
            buf_start: 0,
            buf_end: 0,
            line_buf: Vec::new(),
            current_line: Vec::new(),
        })
    }

    /// Fills the internal buffer by reading a chunk from the file.
    /// Returns Ok(true) if a chunk was read, or Ok(false) if at the beginning.
    fn fill_buf(&mut self) -> io::Result<bool> {
        if self.pos <= self.min_pos {
            return Ok(false);
        }
        let available = self.pos - self.min_pos;
        let size = std::cmp::min(CHUNK_SIZE as u64, available) as usize;
        self.pos -= size as u64;
        self.inner.seek(SeekFrom::Start(self.pos))?;
        self.buf.resize(size, 0);
        self.inner.read_exact(&mut self.buf)?;
        // Reset indices: we work with buf_start = 0, buf_end = size.
        self.buf_start = 0;
        self.buf_end = size;
        Ok(true)
    }

    /// Retrieves the next line (as a Vec<u8>) in proper order.
    /// Lines are determined by the newline character (`b'\n'`). The newline is kept at the end.
    fn next_line(&mut self) -> io::Result<Option<Vec<u8>>> {
        loop {
            if self.buf_end > self.buf_start {
                if let Some(newline_offset) =
                    memchr::memrchr(b'\n', &self.buf[self.buf_start..self.buf_end])
                {
                    let newline_pos = self.buf_start + newline_offset;
                    // If the newline is the last byte in the buffer...
                    if newline_pos + 1 == self.buf_end {
                        // If there's no accumulated data, skip this newline.
                        if self.line_buf.is_empty() {
                            self.buf_end = newline_pos;
                            continue;
                        } else {
                            // If there's accumulated data, form the line and append the newline.
                            let mut line = self.line_buf.clone();
                            line.reverse();
                            line.push(b'\n');
                            self.line_buf.clear();
                            self.buf_end = newline_pos;
                            return Ok(Some(line));
                        }
                    } else {
                        // Normal case: there is content after the newline.
                        // This slice comes from the current chunk and represents the earlier part of the line.
                        let mut line_part = self.buf[newline_pos + 1..self.buf_end].to_vec();
                        if !self.line_buf.is_empty() {
                            // Instead of prepending, append the accumulated (reversed) bytes.
                            let mut accumulated = self.line_buf.clone();
                            accumulated.reverse();
                            line_part.extend(accumulated);
                            self.line_buf.clear();
                        }
                        self.buf_end = newline_pos;
                        line_part.push(b'\n');
                        return Ok(Some(line_part));
                    }
                } else {
                    // No newline found in the current buffer;
                    // accumulate the entire buffer (reversed) so that later, when combined, it yields the correct order.
                    self.line_buf
                        .extend(self.buf[self.buf_start..self.buf_end].iter().rev());
                    self.buf_end = self.buf_start;
                }
            }
            if self.pos <= self.min_pos {
                // Reached the beginning of the file.
                if self.line_buf.is_empty() {
                    return Ok(None);
                } else {
                    let mut line = self.line_buf.clone();
                    line.reverse();
                    self.line_buf.clear();
                    return Ok(Some(line));
                }
            }
            // Fill the buffer with the next chunk.
            if !self.fill_buf()? {
                if self.line_buf.is_empty() {
                    return Ok(None);
                } else {
                    let mut line = self.line_buf.clone();
                    line.reverse();
                    self.line_buf.clear();
                    return Ok(Some(line));
                }
            }
        }
    }
}

impl<R: Read + Seek> Read for ReverseLineReader<R> {
    /// Reads bytes from the reverse line stream into `out_buf`.
    /// It serves the bytes from an internal `current_line` buffer.
    fn read(&mut self, out_buf: &mut [u8]) -> io::Result<usize> {
        let mut total_written = 0;
        while total_written < out_buf.len() {
            if self.current_line.is_empty() {
                // Load next line (if available) into current_line.
                match self.next_line()? {
                    Some(line) => self.current_line = line,
                    None => break, // no more lines
                }
            }
            let to_write = std::cmp::min(out_buf.len() - total_written, self.current_line.len());
            out_buf[total_written..total_written + to_write]
                .copy_from_slice(&self.current_line[..to_write]);
            total_written += to_write;
            self.current_line.drain(..to_write);
        }
        Ok(total_written)
    }
}

#[cfg(test)]
mod tests {
    use std::{
        error::Error,
        io::{Cursor, Read, Write},
    };

    use rand::distr::Alphanumeric;
    use rand::{self, Rng};
    use rstest::rstest;

    use tempfile::NamedTempFile;

    use super::*;

    #[test]
    fn test_term_reader_cancellation() -> Result<(), Box<dyn Error>> {
        let data = b"some data here\n";
        let mut buf = [0u8; 100];

        let token = CancellationToken::new();
        token.cancel(); // Cancel immediately

        let mut reader = TermReader::new(token, &data[..]);
        let n = reader.read(&mut buf)?;

        assert_eq!(n, 0, "Should return 0 bytes when cancelled");
        Ok(())
    }

    #[rstest]
    #[case(
        5,
        "2024-11-20T10:00:00Z stdout F 1234567890\n",
        "2024-11-20T10:00:00Z stdout F 12345\n"
    )]
    #[case(
        10,
        "2024-11-20T10:00:00Z stdout F 1234567890\n",
        "2024-11-20T10:00:00Z stdout F 1234567890\n"
    )]
    #[case(
        20,
        "2024-11-20T10:00:00Z stdout F 1234567890\n",
        "2024-11-20T10:00:00Z stdout F 1234567890\n"
    )]
    fn log_trimmer_reader_truncates_message(
        #[case] limit: u64,
        #[case] input: &str,
        #[case] expected: &str,
    ) -> Result<(), Box<dyn Error>> {
        let mut reader = LogTrimmerReader::new(Cursor::new(input.as_bytes()), limit);
        let mut output = String::new();
        reader.read_to_string(&mut output)?;

        assert_eq!(output, expected);
        Ok(())
    }

    #[rstest]
    #[case(
        3,
        "2024-11-20T10:00:00Z stdout F abcdef\n2024-11-21T10:00:00Z stdout F xyz\n",
        "2024-11-20T10:00:00Z stdout F abc\n2024-11-21T10:00:00Z stdout F xyz\n"
    )]
    #[case(
        2,
        "noheader longmessageexceedinglimit\n2024-11-21T10:00:00Z stdout F qwerty\n",
        "noheader longmessageexceedinglimit\n2024-11-21T10:00:00Z stdout F qw\n"
    )]
    fn log_trimmer_reader_handles_lines_independently(
        #[case] limit: u64,
        #[case] input: &str,
        #[case] expected: &str,
    ) -> Result<(), Box<dyn Error>> {
        let mut reader = LogTrimmerReader::new(Cursor::new(input.as_bytes()), limit);
        let mut output = String::new();
        reader.read_to_string(&mut output)?;

        assert_eq!(output, expected);
        Ok(())
    }

    #[test]
    fn test_reverse_line_reader() -> Result<(), Box<dyn Error>> {
        // Write file
        let mut tmpfile = NamedTempFile::new()?;

        let mut lines = Vec::with_capacity(1000);
        for _i in 1..=100 {
            let random_text: String = rand::rng()
                .sample_iter(&Alphanumeric)
                .take(900) // Generate 1024 characters
                .map(char::from)
                .collect();
            lines.push(random_text.clone());
            tmpfile.write_all(random_text.as_bytes())?;
            tmpfile.write_all(b"\n")?; // Write a newline after each line.
        }
        tmpfile.flush()?;

        // Reverse lines for testing
        lines.reverse();

        // Read file
        let file = tmpfile.into_file();
        let max_pos = file.metadata()?.len();

        let mut reader = ReverseLineReader::new(file, 0, max_pos)?;
        let mut n = 0;
        while let Some(line) = reader.next_line()? {
            let line_str = String::from_utf8_lossy(&line);
            let trimmed_line = line_str.trim_end();
            assert_eq!(trimmed_line, lines[n], "n: {}", n);
            n += 1;
        }

        Ok(())
    }
}
