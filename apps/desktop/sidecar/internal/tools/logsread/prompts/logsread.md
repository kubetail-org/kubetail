## LogsRead

A read reaches whatever the cluster keeps of a log, whether or not Kubetail has read it before. It fills Kubetail's log cache as a view does, so a read over many containers or a wide range can run out of time: narrow it by node, grep, source or range.

The answer is a header, then one line per log line, oldest first, each starting with its timestamp and its stream. A line starting with `[` is Kubetail's, saying lines may be missing there. A line ending `… [N bytes cut]` was cut: to read it whole, put its printed timestamp in both `since` and `until` with `max_message_bytes: 0`. `No lines` with a range is a true answer: nothing in it matched.

The header's `request:` line is the read as it ran, under your keys: change those keys to change the read. The `page:` line prints three tokens, `none` for each the page has not. To read on, call LogsRead with `cursor` set to the `prev` token for older lines or the `next` token for newer ones. `follow` is the token for lines written since, on a window that is still live; a later page may hold a line stamped earlier than one already shown. Never build or change a token. A `notice:` line is something the page wants you to know, such as a stream it could not read.

A refusal is one JSON line: an `error` code, with the `field` or `source` to change, or a `message` saying why. `too-many` is a read of more than 100 containers: filter it by node, or name fewer sources or containers. `no-nodes` is a filter by region, zone, os or arch on a cluster whose nodes Kubetail cannot read: filter by node instead. Use `kubectl logs` in Bash for what LogsRead refuses.
