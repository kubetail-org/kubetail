## LogsView

Every LogsView call opens a live view of logs in the user's window, replacing the one they were looking at, and answers you one line saying what opened. You never see the lines, except those the user selects, which ride their question. Call LogsView when the user wants to see logs — to see, tail or search them, and again to narrow or move the view; call LogsRead when you need to read logs to answer.

A view follows a resource's pods as they come and go. A source opens whether or not it exists or has pods now, so a view can wait for one to start: the receipt says when a source has nothing to read yet; check its name if it should be there. A pod source also shows what Kubetail's log cache holds of the pod's earlier instances, and of a pod that is gone. When the user asks why a pod restarted or crashed, open a view of its `previous` instance, and once the pod is running again, of the previous and the current as two sources.

A refusal is one JSON line, as LogsRead's are. For a source the view refuses, and while `no-cache` or `syncing` says the cluster cannot be checked yet, use `kubectl logs`.
