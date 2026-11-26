Launch an agent to do a multi-step task in the background, with a budget of calls of its own, and get its report back as a notification.

The one agent type, general-purpose, holds all of your tools except Agent, Memory, LogsView and LogsWatch. `subagent_type` may be omitted.

Reach for this when answering would mean many calls whose output you don't need to keep: delegate it and you keep the conclusion, not the raw output. For a single lookup where you already know the object, file or command, do it directly. Once you've delegated a task, don't also do it yourself — wait for its notification.

The agent starts with no memory of this conversation. It sees the newest `<context>` block, less its `## View`, and your prompt, nothing else, so the prompt is a complete brief: the goal, what you already know, the names involved, and what to return. Its report is not shown to the user — relay what matters. Its calls that need the user's approval wait on the user, as yours do.
