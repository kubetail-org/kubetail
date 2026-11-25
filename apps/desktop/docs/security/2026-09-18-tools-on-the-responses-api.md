# Security record — tools on the Responses API, 18 September 2026

**Subject:** the OpenAI row's models now take the same tools the Anthropic row's do. The
Responses encoder (`sidecar/internal/llm/protocols/openai.go`) offers the cluster tools and
`spawn_agent` as function tools and the bash tool as a function named `bash` it defines itself;
a call comes back as the app's `tool_use`, its result goes back as a `function_call_output`.
The loop, the rows, the card and the transcript are unchanged. The living model is
[security-model.md](../security-model.md); the records this one widens are
[the first tool](2026-09-14-list-objects-tool.md), [the general agent](2026-09-17-spawn-agent.md)
and [the bash tool](2026-09-18-bash-tool.md).

## What now leaves

**Every cluster tool's result, in the OpenAI row's transcript.** The bound is the tools' and
unchanged: `list_objects` lists names and namespaces from the local mirror, 8 KiB per call, 8
calls per turn, 16 for a child. Nothing new leaves the machine that did not already leave on
the Anthropic row; what changes is which provider reads it. OpenAI requests carry
`store: false` as before, and every round replays from the record, so the provider holds
nothing between turns.

## What now runs

**A command a GPT model asks for, under the same gate.** The call arrives as the same
`tool_use` named `bash` with the same input shape — `{"command"}` or `{"restart"}` — so it is
one decode (`parseBashInput`, `commandLine`, `messageCommands`) on both wires, the same card,
the same Approve. `TestOpenAIOffersTheBashTheModelNames` pins the function's name and
parameters; the bash record's tests pin everything after the call arrives, and none of them
name a wire. The Responses API's own `shell` tool — a different call and result shape — is not
used; taking it would be a new record.

## What the model reads

`bashDescription` is the one new string a model is told, and it is the sidecar's: the Messages
API carries the tool's own description, so only this encoder has one to write. It names the
gate — once the user approves it in the transcript — and the two inputs. The rules are the
prompt's (`prompts/command.md`), the same file on both wires.

## Platform rows

`bedrock-gpt` and `foundry-gpt` are on the same encoder and gain the cluster tools and
`spawn_agent` like every row on it, and no bash (`withoutHostedTools`). No platform is on in
any build, so nothing here is verified for them; the setting that turns a platform on owes a
round on each.

## Consent

Unchanged. Per command, in the transcript, whatever model asked.

## Note, 19 September 2026

`parseBashInput` is `AnthropicBash.Commands` in `sidecar/internal/commandtools` (*A native tool
is a contract*); the one decode is unchanged.
