Reads a text file from the user's machine.

- `file_path` must be an absolute path in its plain form: no `.` or `..` components, and no `~`.
- A file in the chat's directory, such as a command's saved output or the workspace's, and one in a folder the user granted, is read at once. Any other file waits for the user to approve it.
- Reads up to 2000 lines from `offset` (1-based, default 1). Lines longer than 2000 characters are cut. When you already know which part of the file you need, read only that part.
- Results are returned with line numbers, as `cat -n` prints them.
- Reads text only: not images, PDFs, binaries or directories. A symbolic link is refused with its target; read the target.
- Kubetail's own directories cannot be read, but for the chat's.
