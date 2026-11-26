## TaskStop

Stops a background command, an agent or a log watch started in this conversation, by the ID its result gave. A command is told to stop, and killed if it has not exited within a few seconds; an agent and a watch stop at once. The stop sends no notification, except a watch's, which carries the lines it held to your next message. A background command a stopped agent started keeps running.
