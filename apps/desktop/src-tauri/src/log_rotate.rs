// Copyright 2024 The Kubetail Authors
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

//! A log file that rotates by size: `main.log` is live, `main.log.1` the
//! newest archive, and the archive past the cap is dropped.

use std::fs::{self, File, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};

pub struct RotatingFile {
    path: PathBuf,
    file: File,
    len: u64,
    max_bytes: u64,
    max_archives: usize,
}

impl RotatingFile {
    /// Opens `path` for appending, counting what it already holds, so the cap
    /// spans restarts.
    pub fn open(path: &Path, max_bytes: u64, max_archives: usize) -> io::Result<Self> {
        assert!(max_archives > 0, "rotation needs an archive to move into");
        let file = open_append(path)?;
        let len = file.metadata()?.len();
        Ok(Self {
            path: path.to_path_buf(),
            file,
            len,
            max_bytes,
            max_archives,
        })
    }

    /// `main.log.<n>`.
    fn archive(&self, n: usize) -> PathBuf {
        let mut name = self.path.clone().into_os_string();
        name.push(format!(".{n}"));
        name.into()
    }

    /// Moves every file one number older, oldest first: the rename onto the
    /// last archive replaces it, and the live file becomes `.1`.
    fn rotate(&mut self) -> io::Result<()> {
        for n in (1..self.max_archives).rev() {
            let archive = self.archive(n);
            if archive.exists() {
                fs::rename(archive, self.archive(n + 1))?;
            }
        }
        fs::rename(&self.path, self.archive(1))?;
        self.file = open_append(&self.path)?;
        self.len = 0;
        Ok(())
    }
}

impl Write for RotatingFile {
    /// Rotates before a write that finds the file past the cap, never within
    /// one, so no line is split across two files.
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        if self.len > self.max_bytes {
            // A failed rotation leaves the live file in place, and the next
            // write tries again: a long file is better than a lost line.
            let _ = self.rotate();
        }
        let n = self.file.write(buf)?;
        self.len += n as u64;
        Ok(n)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.file.flush()
    }
}

fn open_append(path: &Path) -> io::Result<File> {
    OpenOptions::new().append(true).create(true).open(path)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_dir(tag: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("kubetail-rotate-{tag}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).expect("temp dir");
        dir
    }

    fn names(dir: &Path) -> Vec<String> {
        let mut names: Vec<String> = fs::read_dir(dir)
            .expect("dir readable")
            .map(|e| e.expect("entry").file_name().to_string_lossy().into_owned())
            .collect();
        names.sort();
        names
    }

    /// Rotation is what a reader sees as history: the live file holds the
    /// newest lines, each numbered archive is one rotation older, and archives
    /// past the cap are gone.
    #[test]
    fn rotates_by_size_and_keeps_a_bounded_history() {
        let dir = temp_dir("history");
        let mut file = RotatingFile::open(&dir.join("main.log"), 64, 2).expect("open");
        // Each line passes the cap on its own, so every line after the first
        // begins with a rotation.
        for n in 1..=4 {
            let line = format!("line{n} {}\n", "x".repeat(64));
            file.write_all(line.as_bytes()).expect("write");
        }
        file.flush().expect("flush");
        // Windows removes no open file, and the cleanup below removes this one.
        drop(file);

        assert_eq!(names(&dir), ["main.log", "main.log.1", "main.log.2"]);
        let read = |name: &str| fs::read_to_string(dir.join(name)).expect("readable");
        assert!(read("main.log").starts_with("line4"));
        assert!(read("main.log.1").starts_with("line3"));
        assert!(read("main.log.2").starts_with("line2"));

        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn a_reopened_file_counts_what_it_already_holds() {
        let dir = temp_dir("reopen");
        let path = dir.join("main.log");
        fs::write(&path, "x".repeat(65)).expect("seed");

        let mut file = RotatingFile::open(&path, 64, 1).expect("open");
        file.write_all(b"after restart\n").expect("write");
        drop(file);

        assert_eq!(names(&dir), ["main.log", "main.log.1"]);
        assert_eq!(fs::read_to_string(&path).unwrap(), "after restart\n");

        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn a_failed_rotation_keeps_the_line() {
        let dir = temp_dir("failed");
        let path = dir.join("main.log");
        // A non-empty directory where the archive goes fails the rename on
        // every platform.
        fs::create_dir_all(dir.join("main.log.1").join("blocker")).unwrap();

        let mut file = RotatingFile::open(&path, 0, 1).expect("open");
        file.write_all(b"first\n").expect("write");
        file.write_all(b"second\n").expect("write");
        drop(file);

        assert_eq!(fs::read_to_string(&path).unwrap(), "first\nsecond\n");

        fs::remove_dir_all(&dir).unwrap();
    }
}
