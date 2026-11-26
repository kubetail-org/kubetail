## LogsWatch

Call LogsWatch only when the user asks to be told about something later: "tell me when webapp throws again", "let me know when it's back up", "collect the errors over the next hour". A question about what is in the logs now is LogsRead.

A watch follows its sources' containers as they come and go, so a rollout needs no second watch. Leave out `grep` to be told when a source writes anything at all, as when the user waits for a service to come up. The call answers at once with the task's ID, and the watch runs in the background, taking one of the chat's task slots. It ends when it holds `limit` matching lines, when they reach `max_bytes`, when `until` arrives, or when every container it follows has stopped, as a Job's do. To gather what happens over a window, raise `limit` and give an `until`; with no `until` a watch waits up to a day. A `since` in the past counts lines already written, so the watch can end as soon as it starts. To watch on from a read, give `cursor` the `next` or `follow` token it printed, in place of the sources, `filters`, `grep` and `since`.

When the watch ends, you are told in a `<task-notification>`, whose `<result>` is a sample like LogsRead's. Read around its first and last line with LogsRead before you answer, since matching lines rarely explain themselves. A watch that ended at its time with no match is an answer: say nothing matched, and do not start another unless asked. Stop a watch with TaskStop and its ID; you are still told what it held.

A refusal is one JSON line, as LogsRead's are. One saying the chat runs as many background tasks as it may clears when one ends, or when you stop one with TaskStop.
