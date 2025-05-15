# Pull Request Commits

We request that changes in Pull Requests be squashed into one signed, single commit with this format:

```
<type>: commit title goes here (all lowercase)

* <type>: Main change 1
...
* <type>: Main change N

Signed-off-by: Your Name <you@example.com>
Co-authored-by: John Smith <john@example.com>
Co-authored-by: Jane Smith <jane@example.com>
```

With these types:

- `new` - for new feature
- `fix` - for bugfix
- `doc` - for documentation 
- `test` - for test
- `ref` - for refactoring
- `wip` - work in progress

To squash commits or to add a signature to an existing commit you can use `git reset` and then create a new commit (replace `N` with the number of commits in your PR):

```console
git reset HEAD~N
git commit -a -s -m "new: add hug-mode to skynet"
git push origin --force
```
