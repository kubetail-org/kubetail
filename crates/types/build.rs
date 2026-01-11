// Copyright 2024-2026 The Kubetail Authors
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

use std::path::PathBuf;

fn main() {
    let out_dir = PathBuf::from(std::env::var("OUT_DIR").unwrap());
    tonic_prost_build::configure()
        .extern_path(".google.protobuf", "::prost_types")
        .compile_well_known_types(true)
        .file_descriptor_set_path(out_dir.join("topology_descriptor.bin"))
        .compile_protos(
            // List your proto files here
            &["../../proto/cluster_agent.proto"],
            // Include path(s) for proto file imports
            &["../../proto"],
        )
        .unwrap();
}
