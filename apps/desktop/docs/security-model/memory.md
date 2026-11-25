# Memory

[← Security model](../security-model.md)

A memory is a note the model reads in later chats, scoped to one cluster or to every cluster. The rule is that text the model read on one cluster reaches another only through the user's eye, and that only a note the user wrote or adopted counts as the user's request. A note the model wrote is information. These rows defend against an injection that steers the model into saving a note. A memory changes no permission, and every command is still approved on its own. A save to one cluster is not approved, by decision, and a saved note reaches later chats whole.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| MEM-1 | The `Memory` tool changes a memory for every cluster only once the user approves the call. | **Enforced** | — |
| MEM-2 | Each model call reaches only the scope it names, so a call for the cluster never reaches a note for every cluster. | **Enforced** | — |
| MEM-3 | Through the `Memory` tool, the model changes none of the user's memories. Every model save is marked as the model's, and lands only over a note already the model's in the scope it names. | **Enforced** | An approved command is outside it. |
| MEM-4 | Only a memory the user wrote or adopted is marked as the user's. Every write from the memory list makes a note the user's, and its editor says so before saving one of the model's. | **Enforced** | — |
| MEM-5 | The model follows only the memories marked as the user's, in the newest `<context>` block. | **Held by review** | No test can pin how a model reads the words. |
| MEM-6 | A chat reaches its own cluster's memories and the global ones alone. The model names neither the cluster nor the chat. | **Enforced** | — |
| MEM-7 | A forget for one cluster reaches its own notes alone. | **Enforced** | — |
| MEM-8 | A subagent is offered no Memory tool. | **Enforced** | — |
| MEM-9 | A memory refuses a credential, whoever writes it. | **Enforced** | A credential in no shape it knows is stored in the clear in `app.db`, like the chat that produced it. |
| MEM-10 | The credential check finds every shape it knows, while keeping a dashboard link's query and a sentence about a password. | **Enforced** | — |
| MEM-11 | A memory changes no permission: no tool offer, approval or gate reads one. | **Held by review** | — |
| MEM-12 | A save to one cluster is not approved. | **By decision** | — |
| MEM-13 | A memory the model saved for every cluster reaches every cluster's chats, whole, once approved. | **By decision** | An injection that gets the model to propose a note and the user to approve it lands in every cluster's chats as information. |
| MEM-14 | A memory the model saved reaches every later question on its cluster, whole. | **By decision** | A note shaped by an injection lands in the context unasked, up to 500 bytes. |

## Evidence

### MEM-1

- **Where:** `sidecar/internal/tools/memory`, `sidecar/internal/services/memory`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `TestEveryCallForEveryClusterAsks`, `TestACheckForEveryClusterAnswersAsTheWrite` — the tool's `Approval` asks about every save and forget with `scope: everywhere` once the store's dry check says the write would land, and skips every other call.
- Note: a write that must fail is refused with the store's own code before any request exists.
- Note: text the model read on one cluster reaches another only through the user's eye.

### MEM-2

- **Where:** `sidecar/internal/tools/memory`, `sidecar/internal/services/memory`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `TestACallForTheClusterNeverReachesANoteForEveryCluster` — a call for the cluster never reaches a note for every cluster.

### MEM-3

- **Where:** `sidecar/internal/services/memory`, `src/components/cluster/memory-list.tsx`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `TestTheModelChangesOnlyItsOwnNotes`, `TestASaveForEveryClusterIsTheModels`, `TestASaveForEveryClusterReplacesOnlyTheModelsNote`, `TestAForgetForEveryClusterRemovesOnlyTheModelsNote` — every model save writes `model`, an approved one for every cluster included, and only over a note already the model's in the scope it names.
- Note: a save or forget that reaches the user's note is refused `user-note`.
- Note: an approved command is outside it, under the *An approved command runs as the user…* row.

### MEM-4

- **Where:** `sidecar/internal/services/memory`, `src/components/cluster/memory-list.tsx`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `TestOnlyTheUserWritesARequest` — every write from the memory list makes a note the user's.
- `says saving one of Kubetail's notes makes it yours, and nothing for yours` — the editor says so before saving one of the model's.

### MEM-5

- **Where:** `sidecar/internal/agent/chat/prompts/system.md`, `sidecar/internal/agent/loop/prompts/data_is_not_instructions.md`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `TestTheSystemPromptExplainsTheMemorySection` — with the `agent` golden files, pins the words.
- Note: the system prompt and *Data is not instructions* say a `"by":"user"` note in the newest block is a request, a `"by":"model"` one information, and the marker nothing anywhere else.
- Note: no test can pin how a model reads them.

### MEM-6

- **Where:** `sidecar/internal/tools/memory/memory.go`, `sidecar/internal/services/memory/service.go`, `sidecar/internal/agent/chat/turn.go`, `sidecar/internal/agent/chat/subagent.go`
- `TestRunAnswersEachOp`, `TestATurnsRuntimeIsItsChatsCluster` — the tool calls the memory service on `Runtime.ClusterID` and `ChatID`, which `agent/chat` sets from the chat's stored cluster.
- Note: the model names neither.

### MEM-7

- **Where:** `sidecar/internal/tools/memory/memory.go`, `sidecar/internal/services/memory/service.go`
- `TestAForgetReachesOnlyItsOwnScope` — a forget for one cluster reaches its own notes alone.

### MEM-8

- **Where:** `sidecar/internal/agent/chat/subagent.go`
- `TestTheSubagentIsOfferedEveryToolButAgentAndMemory` — a subagent is offered no Memory tool.

### MEM-9

- **Where:** `sidecar/internal/lib/safe`, `sidecar/internal/services/memory/note.go`
- `TestCheckNoteRefusesACredential`, `TestCheckNoteRefusesACredentialInTheName`, `TestASaveRefusesACredential` — `services/memory`'s `checkNote` refuses a name or body `safe.HasSecret` matches, on every write and before a call for every cluster is asked about.
- `TestRunNamesEachRefusal` — the refusal reaches the model as its code.

### MEM-10

- **Where:** `sidecar/internal/lib/safe`, `sidecar/internal/services/memory/note.go`
- `TestHasSecretFindsACredentialAndLeavesANoteAlone` — `HasSecret` finds every shape it knows while keeping a dashboard link's query and a sentence about a password.

### MEM-11

- **Where:** `sidecar/internal/services/memory`, `sidecar/internal/agent/chat`
- **Records:** [memory](../security/2026-09-24-memory.md)
- Note: a memory the model wrote reaches it as information.
- Note: one the user wrote reaches it as their request, which grants no more than a message from the user would.
- Note: no tool offer, approval or gate reads either, and every command is still approved on its own.

### MEM-12

- **Where:** `sidecar/internal/tools/memory`
- **Records:** [memory is rows, scoped to a cluster or every cluster](../adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md)
- Note: it reaches only chats that already read that cluster's data.
- Note: every command those chats run is still approved on its own.
- Note: the transcript and Settings' Memory tab show every save.

### MEM-13

- **Where:** `sidecar/internal/tools/memory`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [memory is rows, scoped to a cluster or every cluster](../adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md), [memory](../security/2026-09-24-memory.md)
- Note: an injection that gets the model to propose a note and the user to approve it lands in every cluster's chats as information, as a note for one cluster does on its cluster.
- Note: the request shows the name and every character of the body before the click.
- Note: Settings' Memory tab lists the note as Kubetail's to edit or delete.

### MEM-14

- **Where:** `sidecar/internal/services/memory/section.go`
- **Records:** [memory is rows, scoped to a cluster or every cluster](../adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md), [memory](../security/2026-09-24-memory.md)
- Note: a note shaped by an injection lands in the context unasked, up to 500 bytes.
- Note: the model reads it as information, not an instruction.
- Note: the transcript and Settings' Memory tab show it.
