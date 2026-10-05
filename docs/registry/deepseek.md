# DeepSeek Harness

- **ID**: `deepseek`
- **Store**: `${DSH_HOME:-~/.dsh}/sessions/<workspace-slug>/session-<uuid>/session.v4.jsonl.zstd`, one append-only log per session. The number is the format generation: earlier dsh builds wrote `session.jsonl.zstd` and `session.v3.jsonl.zstd`, and when dsh opens an older session it writes the whole history into a new-generation file and leaves the old one beside it, unchanged. mss reads the newest generation in each session directory and accepts any `session.vN.jsonl`, raw or zstd-framed, so the next bump is read rather than skipped
- **Read override**: `MSS_DEEPSEEK_ROOT` (sessions root), `DSH_HOME` (the harness's own home, also honored)
- **Format**: JSONL, written as consecutive zstd frames by default; raw lines
  are a configuration and both are read
- **Prerequisite**: the `zstd` CLI, for the same reason Zed needs it. Without
  it the sessions are found and none of them can be read, and `mss index` says
  so rather than reporting an empty store.

The first line is the session header — `{"type":"session","id":"session-<uuid>",
"createdAt":<ms>,"cwd":"…"}` — and the project comes from that `cwd`. Every
line after it is one event: `{type, seq, time, data}`.

These event types carry the conversation:

- `user/message` with `data.source.kind == "user"` is what a person typed. The
  same type also carries what plugins splice into the turn — the sandbox policy
  snapshot, the skill catalogue — under other source kinds, and those are the
  harness describing itself rather than history worth recalling.
- `assistant/message` is the agent's turn, complete: `data.message.content` is a
  block array of `text` and `reasoning`. Only the text is recalled; reasoning is
  the model thinking out loud rather than what it told the person.
- `assistant/chunk` with `chunk.type == "text-delta"` is the same answer as it
  streamed. It is read only as a fallback, for a run interrupted before the
  complete message landed — otherwise the answer would land twice.
- `text-chunks` is a packed row: a run of three or more consecutive deltas
  stored as one line with the pieces in `data.texts`. A reader that knows only
  `assistant/chunk` keeps the stray deltas and loses every long answer, which is
  exactly the interrupted case the fallback exists for.
- `tool/result` is tool output, whose content nests a `tool-result` block around
  the text; it is kept under the `tool-output` role so search can tell it from
  speech. From v4 the message is the result itself: `role: "tool"`, the text
  blocks as its content and `isError` on the message, which is where a refused
  edit is read from.

`session/title` gives the session its name; when the model never answered, the
harness falls back to the first prompt.

- **Resume**: none. The launcher's examples mention a tui profile taking
  `--resume <session>`, but this release ships no bundle for one — the two apps
  are `headless`, which takes a task and exits, and `web`, whose flags are all
  about the server. Reopening a conversation is something the web sidebar does,
  so there is nothing for mss to print.
- **Handoff**: paste.

Format verified by installing dsh 0.1.1-rc.2, pointing it at a local model over
an OpenAI-compatible route, and reading what it wrote across sessions that
answered, called a tool, were interrupted mid-answer, and failed before
answering (`@deepseek-ai/dsh-session-persistence-jsonl`). The v4 generation
was checked against the source of `@deepseek-ai/dsh-base` 0.1.7-rc.2 (dsh
2.0.15): `dsh-session-format` names generation N `session.vN.jsonl`, and
`dsh-session-format-v3-to-v4` changes the tool-result shape and namespaces
plugin sources and block types, leaving the event envelope as it was.

**Last verified:** 2026-10-02
