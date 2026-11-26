Replaces exact text in a file on the user's machine.

- `file_path` must be an absolute path in its plain form: no `.` or `..` components, and no `~`. Use the path you read the file at.
- An edit in the chat's workspace does not wait for the user; any other file waits for the user to approve the path and both strings.
- Needs a Read of the file in this conversation first, and fails if it changed since. For a file outside the workspace, Read it before you ask, or the user approves an edit that is then refused.
- `old_string` must match the file exactly, including indentation, and be unique unless `replace_all` is true. Strip the Read line prefix (line number and tab) before matching.
- `replace_all: true` replaces every occurrence.
- Does not create files: use Write.
- Text Read shows as `[redacted]` cannot be matched or written.
- Kubetail's own directories cannot be changed, but for the chat's workspace.
