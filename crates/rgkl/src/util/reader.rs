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
use std::io::{self, Read, Seek, SeekFrom};

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
    truncate_at_bytes: u64,
    current: u64,
    discarding: bool,
    emitted: bool,
    format: FileFormat,
    docker_tail: VecDeque<u8>,
}

impl<R: Read> TermReader<R> {
    pub fn new(
        ctx: CancellationToken,
        inner: R,
        truncate_at_bytes: u64,
        format: FileFormat,
    ) -> Self {
        Self {
            ctx,
            inner,
            truncate_at_bytes,
            current: 0,
            discarding: false,
            emitted: false,
            format,
            docker_tail: VecDeque::new(),
        }
    }
}

impl<R: Read> Read for TermReader<R> {
    #[inline(always)]
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if self.ctx.is_cancelled() {
            return Ok(0);
        }

        loop {
            let n = self.inner.read(buf)?;
            if n == 0 {
                return Ok(0);
            }

            if self.truncate_at_bytes == 0 {
                return Ok(n);
            }

            if !self.discarding {
                if !self.fast_scan(buf, n) {
                    return Ok(n);
                }
            }

            let mut temp_buf = Vec::with_capacity(n);
            self.process_bytes(&buf[..n], &mut temp_buf);

            if !temp_buf.is_empty() {
                let out_len = temp_buf.len();
                buf[..out_len].copy_from_slice(&temp_buf);
                return Ok(out_len);
            }

            if self.ctx.is_cancelled() {
                return Ok(0);
            }
        }
    }
}

impl<R: Read> TermReader<R> {
    fn process_bytes(&mut self, input: &[u8], output: &mut Vec<u8>) {
        let mut idx = 0;
        while idx < input.len() {
            let b = input[idx];

            if self.discarding {
                self.handle_discarding_byte(b, output);
                idx += 1;
                continue;
            }

            if b == b'\n' {
                output.push(b);
                self.reset_line_state();
                idx += 1;
                continue;
            }

            if self.current >= self.truncate_at_bytes {
                self.start_truncation(output);
                self.handle_discarding_byte(b, output);
                idx += 1;
                continue;
            }

            output.push(b);
            self.current += 1;
            idx += 1;
        }
    }

    fn handle_discarding_byte(&mut self, byte: u8, output: &mut Vec<u8>) {
        if self.format == FileFormat::Docker && self.emitted {
            self.push_docker_tail(byte);
        }

        if byte == b'\n' {
            if self.emitted && self.format == FileFormat::Docker {
                if let Some(suffix) = self.take_docker_suffix() {
                    output.extend_from_slice(&suffix);
                }
            }
            output.push(b'\n');
            self.reset_line_state();
        }
    }

    fn start_truncation(&mut self, output: &mut Vec<u8>) {
        if !self.emitted {
            match self.format {
                FileFormat::Docker => output.extend_from_slice(DOCKER_SENTINEL_ESCAPED),
                FileFormat::CRI => output.push(TRUNCATION_SENTINEL),
            }
            self.emitted = true;
            if self.format == FileFormat::Docker {
                self.docker_tail.clear();
            }
        }
        self.discarding = true;
    }

    fn push_docker_tail(&mut self, byte: u8) {
        if byte == b'\n' {
            return;
        }
        self.docker_tail.push_back(byte);
        if self.docker_tail.len() > DOCKER_TAIL_LIMIT {
            self.docker_tail.pop_front();
        }
    }

    fn take_docker_suffix(&mut self) -> Option<Vec<u8>> {
        if self.docker_tail.is_empty() {
            return None;
        }
        let mut tail: Vec<u8> = self.docker_tail.iter().copied().collect();
        self.docker_tail.clear();
        if let Some(idx) = find_last_subslice(&tail, DOCKER_STREAM_MARKER) {
            tail.drain(..idx);
            Some(tail)
        } else {
            None
        }
    }

    fn reset_line_state(&mut self) {
        self.current = 0;
        self.discarding = false;
        self.emitted = false;
        self.docker_tail.clear();
    }
}

impl<R: Read> TermReader<R> {
    /// Fast memchr-based scan: returns true if truncation is required.
    #[inline(always)]
    fn fast_scan(&mut self, buf: &[u8], n: u64) -> bool {
        let mut cur = self.current;
        let limit = self.truncate_at_bytes;
        let mut start = 0u64;

        while start < n {
            if let Some(nl) = memchr(b'\n', &buf[start..n]) {
                let idx = start + nl;
                let seg = idx - start;

                // past limit?
                cur += seg;
                if cur > limit {
                    return true;
                }

                // reset on newline
                cur = 0;
                start = idx + 1;
            } else {
                // last segment
                cur += n - start;
                if cur > limit {
                    return true;
                }
                break;
            }
        }

        self.current = cur;
        false
    }
}

fn find_last_subslice(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    if needle.is_empty() || haystack.len() < needle.len() {
        return None;
    }
    let mut idx = haystack.len() - needle.len();
    loop {
        if &haystack[idx..idx + needle.len()] == needle {
            return Some(idx);
        }
        if idx == 0 {
            break;
        }
        idx -= 1;
    }
    None
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
    use std::{error::Error, io::Write};

    use rand::{self, distr::Alphanumeric, Rng};
    use serde_json::Value;

    use tempfile::NamedTempFile;

    use super::*;

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

    #[test]
    fn test_term_reader_passthrough() -> Result<(), Box<dyn Error>> {
        // When max_line_length = 0, should pass through unchanged
        let data = b"This is a Test";
        let mut buf = [0u8; 14];

        let mut reader =
            TermReader::new(CancellationToken::new(), &data[0..14], 0, FileFormat::CRI);
        let bytes_read = reader.read(&mut buf).expect("Read should succeed");

        assert_eq!(bytes_read, 14, "Should read 14 bytes");
        assert_eq!(
            &buf, b"This is a Test",
            "Buffer should contain data unchanged"
        );
        Ok(())
    }

    #[test]
    fn test_term_reader_no_truncation_needed() -> Result<(), Box<dyn Error>> {
        // Lines shorter than max should pass through
        let data = b"short\nlines\nhere\n";
        let mut buf = [0u8; 100];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        assert_eq!(n, data.len());
        assert_eq!(&buf[..n], data);
        Ok(())
    }

    #[test]
    fn test_term_reader_exact_limit() -> Result<(), Box<dyn Error>> {
        // Line exactly at max_line_length should not be truncated
        let data = b"exactly10c\n";
        let mut buf = [0u8; 100];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        assert_eq!(
            &buf[..n],
            data,
            "Line at exactly max length should not be truncated"
        );
        Ok(())
    }

    #[test]
    fn test_term_reader_single_long_line() -> Result<(), Box<dyn Error>> {
        // Single line exceeding max should be truncated
        let data = b"this line is way too long for the limit\n";
        let mut buf = [0u8; 100];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        let result = &buf[..n];
        let mut expected = b"this line ".to_vec();
        expected.push(TRUNCATION_SENTINEL);
        expected.push(b'\n');
        assert_eq!(result, &expected);
        Ok(())
    }

    #[test]
    fn test_term_reader_multiple_long_lines() -> Result<(), Box<dyn Error>> {
        // Multiple long lines should each be truncated
        let data = b"first very long line here\nsecond also too long\nshort\n";
        let mut buf = [0u8; 200];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        let result = &buf[..n];
        assert!(
            result
                .windows(1)
                .filter(|w| w[0] == TRUNCATION_SENTINEL)
                .count()
                >= 2
        );
        assert!(std::str::from_utf8(result)?.contains("short\n"));
        Ok(())
    }

    #[test]
    fn test_term_reader_mixed_lines() -> Result<(), Box<dyn Error>> {
        // Mix of short, exact, and long lines
        let data = b"short\nexactly10c\nthis is way too long\nok\n";
        let mut buf = [0u8; 200];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        let result = String::from_utf8_lossy(&buf[..n]);

        assert!(result.contains("short\n"));
        assert!(result.contains("exactly10c\n"));
        assert!(result.matches(TRUNCATION_SENTINEL as char).count() >= 1);
        assert!(result.contains("ok\n"));
        Ok(())
    }

    #[test]
    fn test_term_reader_line_spanning_reads() -> Result<(), Box<dyn Error>> {
        // Test that state is maintained across multiple read() calls
        let data = b"this is a very long line that exceeds the limit\n";

        // Use a cursor with small buffer to force multiple reads
        let mut reader = TermReader::new(
            CancellationToken::new(),
            std::io::Cursor::new(data),
            10,
            FileFormat::CRI,
        );

        let mut result = Vec::new();
        let mut buf = [0u8; 16]; // Small buffer to force chunking

        loop {
            let n = reader.read(&mut buf)?;
            if n == 0 {
                break;
            }
            result.extend_from_slice(&buf[..n]);
        }

        let output = String::from_utf8_lossy(&result);
        assert!(output.contains(char::from(TRUNCATION_SENTINEL)));
        assert_eq!(
            output.matches('\n').count(),
            1,
            "Should have exactly one newline"
        );
        Ok(())
    }

    #[test]
    fn test_term_reader_no_trailing_newline() -> Result<(), Box<dyn Error>> {
        // Line without trailing newline that exceeds limit
        let data = b"this is a very long line without newline";
        let mut buf = [0u8; 100];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        let result = &buf[..n];
        assert!(result.starts_with(b"this is a "));
        assert_eq!(result[result.len() - 1], TRUNCATION_SENTINEL);
        Ok(())
    }

    #[test]
    fn test_term_reader_cancellation() -> Result<(), Box<dyn Error>> {
        let data = b"some data here\n";
        let mut buf = [0u8; 100];

        let token = CancellationToken::new();
        token.cancel(); // Cancel immediately

        let mut reader = TermReader::new(token, &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        assert_eq!(n, 0, "Should return 0 bytes when cancelled");
        Ok(())
    }

    #[test]
    fn test_term_reader_empty_lines() -> Result<(), Box<dyn Error>> {
        // Empty lines and lines with just newlines
        let data = b"\n\nshort\n\n";
        let mut buf = [0u8; 100];

        let mut reader = TermReader::new(CancellationToken::new(), &data[..], 10, FileFormat::CRI);
        let n = reader.read(&mut buf)?;

        assert_eq!(&buf[..n], data);
        Ok(())
    }

    #[test]
    fn test_term_reader_very_long_line() -> Result<(), Box<dyn Error>> {
        // Very long line (much longer than buffer)
        let long_line = "a".repeat(1000);
        let data = format!("{}\n", long_line);
        let mut buf = [0u8; 200];

        let mut reader = TermReader::new(
            CancellationToken::new(),
            data.as_bytes(),
            10,
            FileFormat::CRI,
        );

        let mut result = Vec::new();
        loop {
            let n = reader.read(&mut buf)?;
            if n == 0 {
                break;
            }
            result.extend_from_slice(&buf[..n]);
        }

        let output = String::from_utf8_lossy(&result);
        assert!(output.starts_with("aaaaaaaaaa"));
        assert!(output.contains(char::from(TRUNCATION_SENTINEL)));
        assert!(output.len() < 100, "Should be much shorter than original");
        Ok(())
    }

    #[test]
    fn test_term_reader_docker_truncation_preserves_json() -> Result<(), Box<dyn Error>> {
        let long_log = "Z".repeat(256);
        let json_line = format!(
            "{{\"log\":\"{}\",\"stream\":\"stdout\",\"time\":\"2024-01-01T00:00:00Z\"}}\n",
            long_log
        );
        let mut buf = vec![0u8; json_line.len()];

        let mut reader = TermReader::new(
            CancellationToken::new(),
            json_line.as_bytes(),
            64,
            FileFormat::Docker,
        );

        let n = reader.read(&mut buf)?;
        let truncated = std::str::from_utf8(&buf[..n])?;

        assert!(truncated.contains("\\u001F"));

        let parsed: Value = serde_json::from_str(truncated)?;
        let log = parsed["log"].as_str().unwrap();
        assert!(log.contains(char::from(TRUNCATION_SENTINEL)));
        assert_eq!(parsed["time"].as_str(), Some("2024-01-01T00:00:00Z"));

        Ok(())
    }
}
