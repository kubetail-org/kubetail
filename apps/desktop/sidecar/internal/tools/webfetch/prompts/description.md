Fetches a web page, from the user's machine, as markdown.

- Every fetch waits for the user to approve the URL.
- HTTP is upgraded to HTTPS. Local and private addresses, and hostnames without a dot, are refused.
- A redirect to another host is returned to you rather than followed; call again with the redirect URL.
- Reads text pages only: HTML, markdown, plain text, JSON, YAML, XML. Not PDFs or images.
- A page over 30,000 bytes is saved to a file, with a preview; read the rest with Read.
- Fails on pages that need signing in.
