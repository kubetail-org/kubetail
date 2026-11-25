Replaces exact text in a file on the user's machine.

- `file_path` must be an absolute path in its plain form: no `.` or `..` components, and no `~`.
- An edit in the chat's workspace does not wait for the user; any other file waits for the user to approve the path and both strings.
- Needs a Read of the file in this conversation first, and fails if it changed since. For a file outside the workspace, Read it before you ask, or the user approves an edit that is then refused.
- Use the path you read the file at. Another spelling of the same file, through a symbolic link in a parent directory, needs its own Read.
- `old_string` must match the file exactly, including indentation, and be unique unless `replace_all` is true. Strip the Read line prefix (line number and tab) before matching.
- `replace_all: true` replaces every occurrence.
- Does not create files: use Write. An empty `old_string`, or one equal to `new_string`, is refused.
- Read shows `[redacted]` in place of a secret. Text holding it cannot be matched, and `new_string` cannot hold it.
- A symbolic link is refused with its target. Kubetail's own directories cannot be changed, but for the chat's workspace.
