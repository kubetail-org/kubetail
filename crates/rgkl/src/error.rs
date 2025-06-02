use thiserror::Error;

#[derive(Debug, Error)]
pub(crate) enum Error {
    #[error("Error reading data")]
    IOError(#[from] std::io::Error),
}
