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

use std::io::{self, BufRead, BufReader, Read, Seek, SeekFrom};

use crate::util::format::FileFormat;
use memchr::{memchr, memchr2};
use tokio_util::sync::CancellationToken;

const LOG_TRIMMER_READER_BUFFER_SIZE: usize = 32 * 1024; // 32 KB
const REVERSE_READER_CHUNK_SIZE: usize = 64 * 1024; // 64KB
pub const TRUNCATION_SENTINEL: u8 = 0x1F;

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
    format: FileFormat,
    truncate_at_bytes: usize,
    truncate_enabled: bool,
    docker_line_buf: Vec<u8>,
    internal_buf: Vec<u8>,
    pos: usize,
}

impl<R: Read> LogTrimmerReader<R> {
    /// Creates a new LogTrimmerReader.
    /// The `format` is used to detect where the log message starts.
    /// If `truncate_at_bytes` is 0, truncation is disabled (pass-through mode).
    pub fn new(reader: R, format: FileFormat, truncate_at_bytes64: u64) -> Self {
        let truncate_at_bytes = truncate_at_bytes64 as usize;
        Self {
            input: BufReader::with_capacity(LOG_TRIMMER_READER_BUFFER_SIZE, reader),
            format,
            truncate_at_bytes,
            truncate_enabled: truncate_at_bytes > 0,
            docker_line_buf: Vec::with_capacity(LOG_TRIMMER_READER_BUFFER_SIZE),
            internal_buf: Vec::with_capacity(Self::buffer_capacity(truncate_at_bytes)),
            pos: 0,
        }
    }

    #[inline]
    fn buffer_capacity(truncate_at_bytes: usize) -> usize {
        // Reserve enough room for header + message up to the truncate limit, but keep
        // a sensible minimum to avoid frequent reallocations on long lines.
        let target = truncate_at_bytes.saturating_add(256);
        std::cmp::max(LOG_TRIMMER_READER_BUFFER_SIZE, target)
    }

    fn refill_buffer(&mut self) -> io::Result<bool> {
        match self.format {
            FileFormat::Docker => self.refill_buffer_docker(),
            FileFormat::CRI => self.refill_buffer_cri(),
        }
    }

    fn refill_buffer_cri(&mut self) -> io::Result<bool> {
        self.internal_buf.clear();
        self.pos = 0;

        let mut found_header = false;
        let mut space_count = 0;
        let mut current_msg_len = 0;
        let mut truncated_bytes: u64 = 0;

        loop {
            let available = self.input.fill_buf()?;
            if available.is_empty() {
                return Ok(!self.internal_buf.is_empty());
            }

            let mut consumed = 0;
            let mut line_complete = false;
            let mut truncated = false;
            let mut newline_consumed = false;

            while consumed < available.len() {
                if !found_header {
                    match memchr2(b'\n', b' ', &available[consumed..]) {
                        Some(rel_idx) => {
                            let idx = consumed + rel_idx;
                            let byte = available[idx];
                            self.internal_buf
                                .extend_from_slice(&available[consumed..=idx]);
                            consumed = idx + 1;

                            if byte == b' ' {
                                space_count += 1;
                                // Standard K8s format: <time> <stream> <tag> <message>
                                if space_count == 3 {
                                    found_header = true;
                                }
                            } else {
                                line_complete = true;
                                break;
                            }
                        }
                        None => {
                            self.internal_buf.extend_from_slice(&available[consumed..]);
                            consumed = available.len();
                            break;
                        }
                    }
                } else {
                    let start = consumed;
                    let search_slice = &available[start..];
                    let newline_rel = memchr(b'\n', search_slice);
                    let bytes_until_newline = newline_rel.unwrap_or(search_slice.len());

                    let mut take = bytes_until_newline;
                    if self.truncate_enabled {
                        let remaining = self.truncate_at_bytes.saturating_sub(current_msg_len);
                        if bytes_until_newline > remaining {
                            take = remaining;
                            truncated = true;
                        }
                    }

                    if take > 0 {
                        self.internal_buf.extend_from_slice(&search_slice[..take]);
                        current_msg_len = current_msg_len.saturating_add(take);
                    }

                    if truncated {
                        truncated_bytes = (bytes_until_newline - take) as u64;
                        if newline_rel.is_some() {
                            consumed = start + bytes_until_newline + 1;
                            newline_consumed = true;
                        } else {
                            consumed = available.len();
                        }
                        line_complete = true;
                        break;
                    }

                    if let Some(_) = newline_rel {
                        self.internal_buf.push(b'\n');
                        consumed = start + bytes_until_newline + 1;
                        line_complete = true;
                        break;
                    } else {
                        consumed = available.len();
                        break;
                    }
                }
            }

            self.input.consume(consumed);

            if truncated {
                if !newline_consumed {
                    let (discarded, saw_newline) = self.discard_rest_of_line()?;
                    let extra = if saw_newline {
                        discarded.saturating_sub(1)
                    } else {
                        discarded
                    };
                    truncated_bytes = truncated_bytes.saturating_add(extra as u64);
                }
                Self::append_truncation_marker_raw(&mut self.internal_buf, truncated_bytes);
                return Ok(true);
            }

            if line_complete {
                return Ok(true);
            }
        }
    }

    fn refill_buffer_docker(&mut self) -> io::Result<bool> {
        self.internal_buf.clear();
        self.pos = 0;

        const PREFIX: &[u8] = b"{\"log\":\""; // message starts at the 9th byte
        self.docker_line_buf.clear();

        // Read a single line using fill_buf + memchr to avoid per-call allocation churn.
        loop {
            let available = self.input.fill_buf()?;
            if available.is_empty() {
                if self.docker_line_buf.is_empty() {
                    return Ok(false);
                }
                break;
            }

            if let Some(rel_idx) = memchr(b'\n', available) {
                let take = rel_idx + 1;
                self.docker_line_buf.extend_from_slice(&available[..take]);
                self.input.consume(take);
                break;
            } else {
                self.docker_line_buf.extend_from_slice(available);
                let len = available.len();
                self.input.consume(len);
            }
        }

        if self.docker_line_buf.len() < PREFIX.len() {
            self.internal_buf.extend_from_slice(&self.docker_line_buf);
            return Ok(true);
        }

        // Find end of the message (next unescaped quote) by hopping between quotes.
        let mut msg_end = self.docker_line_buf.len();
        let payload = &self.docker_line_buf[PREFIX.len()..];
        let mut search_start = 0;
        while let Some(rel_idx) = memchr(b'"', &payload[search_start..]) {
            let idx_in_payload = search_start + rel_idx;
            let abs_idx = PREFIX.len() + idx_in_payload;

            // Count preceding backslashes to determine if this quote is escaped.
            let mut backslashes = 0;
            let mut cursor = abs_idx;
            while cursor > 0 && self.docker_line_buf[cursor - 1] == b'\\' {
                backslashes += 1;
                cursor -= 1;
            }

            if backslashes % 2 == 0 {
                msg_end = abs_idx;
                break;
            }

            search_start = idx_in_payload + 1;
        }

        let message = &self.docker_line_buf[PREFIX.len()..msg_end];
        if !self.truncate_enabled || message.len() <= self.truncate_at_bytes {
            self.internal_buf.extend_from_slice(&self.docker_line_buf);
            return Ok(true);
        }

        let truncated_bytes = (message.len() - self.truncate_at_bytes) as u64;

        self.internal_buf
            .extend_from_slice(&self.docker_line_buf[..PREFIX.len()]);
        self.internal_buf
            .extend_from_slice(&message[..self.truncate_at_bytes]);
        Self::append_truncation_marker_json(&mut self.internal_buf, truncated_bytes);
        self.internal_buf
            .extend_from_slice(&self.docker_line_buf[msg_end..]);

        Ok(true)
    }

    fn append_truncation_marker_raw(buf: &mut Vec<u8>, truncated_bytes: u64) {
        buf.extend_from_slice(&truncated_bytes.to_be_bytes());
        buf.push(TRUNCATION_SENTINEL);
        buf.push(b'\n');
    }

    fn append_truncation_marker_json(buf: &mut Vec<u8>, truncated_bytes: u64) {
        const HEX: &[u8; 16] = b"0123456789ABCDEF";
        for byte in truncated_bytes
            .to_be_bytes()
            .into_iter()
            .chain(std::iter::once(TRUNCATION_SENTINEL))
        {
            buf.extend_from_slice(b"\\u00");
            buf.push(HEX[(byte >> 4) as usize]);
            buf.push(HEX[(byte & 0x0F) as usize]);
        }
    }

    /// Helper: Consumes bytes until a newline or EOF, returning the count of bytes discarded
    /// and whether a newline was encountered.
    fn discard_rest_of_line(&mut self) -> io::Result<(usize, bool)> {
        let mut total_discarded = 0;

        loop {
            let available = self.input.fill_buf()?;
            if available.is_empty() {
                return Ok((total_discarded, false));
            }

            if let Some(index) = memchr(b'\n', available) {
                let bytes_to_consume = index + 1;
                self.input.consume(bytes_to_consume);
                total_discarded += bytes_to_consume;
                return Ok((total_discarded, true));
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
        let size = std::cmp::min(REVERSE_READER_CHUNK_SIZE as u64, available) as usize;
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
    use crate::util::format::FileFormat;

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
        {
            let mut expected = b"2024-11-20T10:00:00Z stdout F 12345".to_vec();
            expected.extend_from_slice(&5u64.to_be_bytes());
            expected.push(TRUNCATION_SENTINEL);
            expected.push(b'\n');
            expected
        }
    )]
    #[case(
        10,
        "2024-11-20T10:00:00Z stdout F 1234567890\n",
        b"2024-11-20T10:00:00Z stdout F 1234567890\n".to_vec()
    )]
    #[case(
        20,
        "2024-11-20T10:00:00Z stdout F 1234567890\n",
        b"2024-11-20T10:00:00Z stdout F 1234567890\n".to_vec()
    )]
    fn log_trimmer_reader_truncates_message(
        #[case] limit: u64,
        #[case] input: &str,
        #[case] expected: Vec<u8>,
    ) -> Result<(), Box<dyn Error>> {
        let mut reader =
            LogTrimmerReader::new(Cursor::new(input.as_bytes()), FileFormat::CRI, limit);
        let mut output = Vec::new();
        reader.read_to_end(&mut output)?;

        assert_eq!(output, expected);
        Ok(())
    }

    #[rstest]
    #[case(
        3,
        "2024-11-20T10:00:00Z stdout F abcdef\n2024-11-21T10:00:00Z stdout F xyz\n",
        {
            let mut expected = b"2024-11-20T10:00:00Z stdout F abc".to_vec();
            expected.extend_from_slice(&3u64.to_be_bytes());
            expected.push(TRUNCATION_SENTINEL);
            expected.push(b'\n');
            expected.extend_from_slice(b"2024-11-21T10:00:00Z stdout F xyz\n");
            expected
        }
    )]
    #[case(
        2,
        "noheader longmessageexceedinglimit\n2024-11-21T10:00:00Z stdout F qwerty\n",
        {
            let mut expected =
                b"noheader longmessageexceedinglimit\n2024-11-21T10:00:00Z stdout F qw".to_vec();
            expected.extend_from_slice(&4u64.to_be_bytes());
            expected.push(TRUNCATION_SENTINEL);
            expected.push(b'\n');
            expected
        }
    )]
    fn log_trimmer_reader_handles_lines_independently(
        #[case] limit: u64,
        #[case] input: &str,
        #[case] expected: Vec<u8>,
    ) -> Result<(), Box<dyn Error>> {
        let mut reader =
            LogTrimmerReader::new(Cursor::new(input.as_bytes()), FileFormat::CRI, limit);
        let mut output = Vec::new();
        reader.read_to_end(&mut output)?;

        assert_eq!(output, expected);
        Ok(())
    }

    #[test]
    fn log_trimmer_reader_truncates_docker_format() -> Result<(), Box<dyn Error>> {
        let input = r#"{"log":"abcdefghij","stream":"stdout","time":"2024-11-20T10:00:00Z"}"#;
        let input_with_newline = format!("{input}\n").into_bytes();
        let mut reader =
            LogTrimmerReader::new(Cursor::new(input_with_newline), FileFormat::Docker, 5);

        let mut output = Vec::new();
        reader.read_to_end(&mut output)?;

        let mut expected = br#"{"log":"abcde\u0000\u0000\u0000\u0000\u0000\u0000\u0000\u0005\u001F","stream":"stdout","time":"2024-11-20T10:00:00Z"}"#.to_vec();
        expected.push(b'\n');

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
