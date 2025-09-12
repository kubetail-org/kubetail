use tokio::select;
use tokio::sync::{broadcast::Sender as BcSender, mpsc};
use tokio_stream::wrappers::ReceiverStream;
use tonic::Status;

/// Wraps a `mpsc::Receiver<Result<T, Status>>` with a termination signal.
///
/// When the broadcast channel receives a shutdown signal, this wrapper emits a
/// single `Err(Status::unavailable("server shutting down"))` and then
/// terminates the stream. If the inner receiver yields an `Err(Status)`, that
/// error is forwarded and the stream terminates without emitting an additional
/// shutdown error. When the inner receiver ends (returns `None`), the stream
/// completes normally.
pub fn wrap_with_shutdown<T: Send + 'static>(
    mut rx: mpsc::Receiver<Result<T, Status>>,
    term_tx: BcSender<()>,
) -> ReceiverStream<Result<T, Status>> {
    let (out_tx, out_rx) = mpsc::channel(100);
    let mut term_rx = term_tx.subscribe();

    tokio::spawn(async move {
        loop {
            select! {
                biased;
                // Prefer shutdown over completing the stream to avoid EOF on shutdown.
                _ = term_rx.recv() => {
                    let _ = out_tx
                        .send(Err(Status::new(tonic::Code::Unavailable, "server shutting down")))
                        .await;
                    break;
                }
                // Propagate inner items
                maybe = rx.recv() => {
                    match maybe {
                        Some(Ok(item)) => {
                            if out_tx.send(Ok(item)).await.is_err() {
                                break;
                            }
                        }
                        Some(Err(status)) => {
                            let _ = out_tx.send(Err(status)).await;
                            break;
                        }
                        None => {
                            break;
                        }
                    }
                }
            }
        }
        // Drop out_tx to close the outer stream
    });

    ReceiverStream::new(out_rx)
}

#[cfg(test)]
mod tests {
    use super::wrap_with_shutdown;
    use tokio::sync::{broadcast, mpsc};
    use tokio_stream::StreamExt;
    use tonic::Status;

    // Helper to extract a Status from a Result value in tests
    fn status_code<T>(r: Result<T, Status>) -> tonic::Code {
        r.err().unwrap().code()
    }

    #[tokio::test]
    async fn propagates_items_and_completes_on_inner_close() {
        let (tx, rx) = mpsc::channel::<Result<i32, Status>>(8);
        let (term_tx, _term_rx) = broadcast::channel::<()>(1);

        // Wrap (keep a clone of the sender alive)
        let mut out = wrap_with_shutdown(rx, term_tx.clone());

        // Send some items then close
        tx.send(Ok(1)).await.unwrap();
        tx.send(Ok(2)).await.unwrap();
        drop(tx);

        // Collect all output
        let results: Vec<_> = out.collect().await;
        assert_eq!(results.len(), 2);
        assert_eq!(results[0].as_ref().ok(), Some(&1));
        assert_eq!(results[1].as_ref().ok(), Some(&2));
    }

    #[tokio::test]
    async fn forwards_inner_error_and_terminates() {
        let (tx, rx) = mpsc::channel::<Result<i32, Status>>(8);
        let (term_tx, _term_rx) = broadcast::channel::<()>(1);
        let mut out = wrap_with_shutdown(rx, term_tx.clone());

        tx.send(Err(Status::aborted("boom"))).await.unwrap();
        // After an error, wrapper should terminate; additional items are ignored.
        let first = out.next().await.unwrap();
        assert_eq!(first.unwrap_err().code(), tonic::Code::Aborted);
        assert!(out.next().await.is_none());
    }

    #[tokio::test]
    async fn emits_unavailable_on_shutdown_without_items() {
        let (_tx, rx) = mpsc::channel::<Result<i32, Status>>(8);
        let (term_tx, _term_rx) = broadcast::channel::<()>(1);
        let mut out = wrap_with_shutdown(rx, term_tx.clone());

        // Signal shutdown
        let _ = term_tx.send(());

        // First (and only) item should be UNAVAILABLE
        let first = out.next().await.unwrap();
        assert_eq!(status_code(first), tonic::Code::Unavailable);
        assert!(out.next().await.is_none());
    }

    #[tokio::test]
    async fn emits_unavailable_after_some_items_on_shutdown() {
        let (tx, rx) = mpsc::channel::<Result<i32, Status>>(8);
        let (term_tx, _term_rx) = broadcast::channel::<()>(1);
        let mut out = wrap_with_shutdown(rx, term_tx.clone());

        // Send one item
        tx.send(Ok(42)).await.unwrap();
        let first = out.next().await.unwrap();
        assert_eq!(first.as_ref().ok(), Some(&42));

        // Now signal shutdown
        let _ = term_tx.send(());

        // Next should be the UNAVAILABLE error and then end
        let second = out.next().await.unwrap();
        assert_eq!(status_code(second), tonic::Code::Unavailable);
        assert!(out.next().await.is_none());
    }

    #[tokio::test]
    async fn prefers_shutdown_over_inner_error() {
        let (tx, rx) = mpsc::channel::<Result<i32, Status>>(8);
        let (term_tx, _term_rx) = broadcast::channel::<()>(1);
        let mut out = wrap_with_shutdown(rx, term_tx.clone());

        // Push an inner error, then signal shutdown; wrapper should forward the
        // shutdown status (biased select prefers shutdown if both are ready).
        tx.send(Err(Status::unknown("inner"))).await.unwrap();
        let _ = term_tx.send(());

        let first = out.next().await.unwrap();
        assert_eq!(status_code(first), tonic::Code::Unavailable);
        assert!(out.next().await.is_none());
    }
}
