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

use std::path::Path;

/// Represents the format of log files
#[allow(clippy::upper_case_acronyms)]
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum FileFormat {
    /// Docker JSON format logs
    Docker,
    /// Kubernetes CRI format logs
    CRI,
}

impl FileFormat {
    /// Docker writes `<name>-json.log`; every other file is CRI.
    pub fn from_path(path: &Path) -> Self {
        if path.to_string_lossy().ends_with("-json.log") {
            FileFormat::Docker
        } else {
            FileFormat::CRI
        }
    }
}

impl Default for FileFormat {
    /// Default to CRI format for backward compatibility
    fn default() -> Self {
        FileFormat::CRI
    }
}

#[cfg(test)]
mod test {
    use std::path::Path;

    use rstest::rstest;

    use super::FileFormat;

    #[rstest]
    #[case("/var/log/containers/pod_ns_app-abc-json.log", FileFormat::Docker)]
    #[case("/var/log/containers/pod_ns_app-abc.log", FileFormat::CRI)]
    #[case("/var/log/containers/json.log", FileFormat::CRI)]
    fn test_from_path(#[case] path: &str, #[case] expected: FileFormat) {
        assert_eq!(FileFormat::from_path(Path::new(path)), expected);
    }
}
