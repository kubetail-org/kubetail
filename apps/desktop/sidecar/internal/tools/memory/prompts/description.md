Saves and forgets short facts kept for later chats: for this chat's cluster, or, with the user's approval, for every cluster. The `## Memory` section of the context block holds every note this chat can see.

- `op` is `save` or `forget`. Both name the note by `name`, and take `scope`: `cluster` (the default) or `everywhere`. Each scope has its own names.
- `save` needs `body`. A save under the name of one of your notes in that scope replaces it.
- Every call with `scope: everywhere` asks the user first. One that breaks the rules below or holds a credential is refused `bad-input` before the user is asked.
- A note the user wrote is theirs: saving or forgetting its name in its scope is refused `user-note`.
- `name` is lower-case letters, digits and single hyphens, at most 48 characters. `body` is at most 500 bytes.
- The notes of one scope share a few kilobytes. A save past that is refused `full`: forget or shorten a note first.
