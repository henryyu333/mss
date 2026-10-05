# pi (pi.dev coding agent)

| Field | Value |
| --- | --- |
| **Format** | JSONL transcript |
| **Default store path** | `~/.pi/agent/sessions/<encoded-project>/<timestamp>_<uuid>.jsonl` |
| **Env override** | `MSS_PI_ROOT` |
| **mss parser** | `internal/sources/pi.go` |
| **Last verified** | 2026-09-06 |
## Discovery

pi stores session transcripts under `~/.pi/agent/sessions/`. Each project directory uses the same `--`-encoded path scheme as Claude Code, e.g. `--Users-max-code-mss--` for `/Users/max/code/mss`. Within each project directory, session files are named `<ISO-timestamp>_<UUID>.jsonl`. The encoding is lossy (`my-app` and `my/app` give the same name), so the header's `cwd` names the project and the directory is the fallback for a header without one (#4427).
## File layout

Each `.jsonl` file is a single session. The first line is always a session header:

```json
{"type":"session","version":3,"id":"<uuid>","timestamp":"<ISO-8601>","cwd":"<absolute-path>"}
```

Subsequent lines are typed events:

| `type` | Description |
| --- | --- |
| `session` | Session header (first line only) |
| `model_change` | Model/provider switch |
| `thinking_level_change` | Thinking level adjustment |
| `message` | User prompt, assistant response, or tool result |
## Message records

Messages use a wrapper envelope:

```json
{
  "type": "message",
  "id": "<hex>",
  "parentId": "<hex-or-null>",
  "timestamp": "<ISO-8601>",
  "message": {
    "role": "user|assistant|toolResult",
    "content": [{"type": "text", "text": "..."}],
    "timestamp": 1784448616190
  }
}
```

### Roles

| `message.role` | mss maps to |
| --- | --- |
| `user` | `user` |
| `assistant` | `assistant` |
| `toolResult` | tool output (`RoleToolOutput`) |

### Content

`message.content` is an array of typed blocks. mss extracts `text` from blocks where `"type": "text"`. Blocks with `"type": "thinking"` are skipped.

### Tool calls

A `toolCall` block carries `name` and `arguments`. mss reads them for pi and every harness built on it (omp, OpenClaw, gjc, prime, senpi, Kimchi), so `mss files`, `how`, `restore` and `blame` have something to go on (#4113):

| `name` | Arguments | mss records |
| --- | --- | --- |
| `read` | `path` | the file |
| `edit` | `path`, `edits[].oldText` / `newText` (older pi: one `oldText` / `newText` pair) | the file, the replaced span, the written lines |
| `write` | `path`, `content` | the file and the written lines |
| `apply_patch` (OpenClaw) | `input`, a `*** Begin Patch` body | each file the patch names, its removed lines, its added lines |
| `bash` (OpenClaw: `exec`; the pi-coding-agent under Senpi and Kimchi also `powershell`) | `command` | the command, and `→ exit N` from the matching `toolResult` (`details.exitCode` when there is one, else the "Command exited with code N" line that ends a failed result, `exit 0` for a result that is not an error) |

A relative `path` resolves against the header's `cwd`. gjc's `edit` takes one `input` string in its hashline form instead; see the gjc entry. omp and gjc also edit in a replace mode, omp's `{path, old_string, new_string}` (or `edits` of those) and gjc's `{path, edits:[{old_text, new_text}]}`, and a patch mode, `{path, edits:[{op, diff}]}`: those give the same records, a patch's `-` lines per hunk the replaced span and its `+` lines, or a created file's whole `diff`, the written lines (#4524).

### Timestamps

Both ISO-8601 strings (`"timestamp"` in the envelope) and Unix milliseconds (`"timestamp"` inside `message`) are observed. The parser uses the envelope timestamp.
## Session identity

The `id` field from the session header line is used as the session ID. The UUID also appears in the filename.
## Known quirks and drift

- Project directory encoding uses `--` prefix and suffix (e.g. `--Users-max-code-foo--`) compared to Claude Code's single `-` prefix. The `resolveEncodedPath` function handles both.
- Version field observed: `3`. No version migration behavior is known.
- The `parentId` chain forms a tree, not a flat list; mss ignores the tree structure and processes messages in file order.

**Last verified:** 2026-09-06
