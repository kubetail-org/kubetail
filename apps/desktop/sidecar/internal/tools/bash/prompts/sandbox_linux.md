
A program installed as a snap, one under `/snap/bin`, does not run in the sandbox, since it starts through snapd. A command that runs one needs this chat run outside the sandbox. A folder the sandbox keeps closed is an empty directory inside it, so listing one shows nothing rather than failing.
