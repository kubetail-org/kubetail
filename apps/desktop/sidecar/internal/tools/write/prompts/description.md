Writes a file on the user's machine, replacing it whole if one is there.

- `file_path` must be an absolute path in its plain form: no `.` or `..` components, and no `~`. Use the path you read the file at. Missing parent directories are created.
- A write in the chat's workspace does not wait for the user; any other file waits for the user to approve the path and the content.
- Replacing a file needs a Read of the whole file in this conversation first, and fails if it changed since. For a file outside the workspace, Read it before you ask, or the user approves a write that is then refused. For a change to part of a file, use Edit.
- The content is written exactly as given: no newline is added and no line endings are converted.
- Kubetail's own directories cannot be written, but for the chat's workspace.
