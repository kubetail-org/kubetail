<context>
## Cluster

```json
{"cluster":{"name":"prod","context":"gke_acme_us-east1_prod","kubernetes":"v1.31.2"},"connection":{"status":"Connected","tls":"verified"},"freshness":{"status":"watching"},"inventory":{"namespaces":{"status":"watching","names":["default","kube-system","payments"]},"apiGroups":{"status":"discovered","names":["networking.istio.io"]}}}
```

## Memory

```json
{"memories":[{"body":"Never restart payments pods between 09:00 and 11:00 UTC.","by":"user","name":"deploy-window","scope":"cluster","updated":"2026-02-02"},{"body":"Istio 1.24 serves both the v1 and v1beta1 VirtualService APIs.","by":"model","name":"istio-apis","scope":"everywhere","updated":"2026-01-20"}],"today":"2026-03-15"}
```

## Workspace

```json
{"path":"/home/ana/.local/share/kubetail/chats/0195f1a0-5b6e-7c3d-8e4f-0a1b2c3d4e5f/workspace"}
```

## View

```json
{"call":"toolu_01AbC","selected":[{"at":"2026-03-15T09:14:20.004Z","message":"panic: assignment to entry in nil map","stream":"payments/webapp-7d9f-x2k/app"}],"shown":{"atEnd":true,"from":"2026-03-15T09:14:02.118Z","lines":38,"paused":false,"pinned":true,"to":"2026-03-15T09:14:31.902Z"},"view":{"anchor":"tail","grep":"error","pinToEnd":true,"sources":[{"kind":"deployment","name":"webapp","namespace":"payments"}]}}
```

## Sandbox

```json
{"commands":"sandboxed","network":"on for this message","read":["/home/ana/notes"],"readWrite":["/home/ana/src/payments"]}
```
</context>

Why is the payments deployment paused?
