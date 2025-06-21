#![allow(clippy::all)]
pub mod cluster_agent {
    #![allow(clippy::all)]
    tonic::include_proto!("cluster_agent");

    pub const FILE_DESCRIPTOR_SET: &[u8] =
        tonic::include_file_descriptor_set!("topology_descriptor");
}
