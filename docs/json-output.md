# JSON output contract

Several `mss` commands accept `--json` and print machine-readable output for
scripting, dashboards, and editor integrations. Object-shaped responses include
a `schema_version` field so consumers can detect breaking changes.

## Stability policy

- **Within a `schema_version`**, changes are additive only: new optional fields
  may appear, but existing field names, types, and meanings stay the same.
- **Bumping `schema_version`** signals a breaking change (field removal, rename,
  or type change). Consumers should branch on `schema_version` before parsing
  the rest of the envelope.
- The current version is **2** (constant in `internal/jsonout`).
- Search, `show`, `last` and `doctor` print one envelope each in this
  document's shapes. A command's section below states whether it carries
  `schema_version`.

### What changed in version 2

`mss search --json` used to return a bare array on the exact path and an object
envelope on every fallback path, so a consumer had to handle two shapes and
could not tell which it had without inspecting the value. It now always returns
the envelope, and the envelope answers the two questions a caller cannot answer
from a list of hits:

- `tier` — which tier answered: `exact`, `close`, `stemmed`, `semantic`,
  `error`, or `relevance`. **`relevance` means nothing matched** and these are
  the nearest sessions mss could find — unless `strict` is set, in which case
  that many of them do match (see below). Counting those as hits overstates
  recall, and this used to be readable only as a sentence on stderr. **`error`
  IS a match** — the query was a pasted error and these sessions hit that exact
  error (matched by signature, not by words); count them as hits.
- `strict` — how many of the hits hold every word of the query. Present on the
  `relevance` tier, where an answer of fewer than ten matching sessions is
  published with the ranking merged underneath it: the tier label then says
  nothing matched while `strict` says how much of it did. Each such hit also
  carries `"strict": true`, and the order is the merged ranking's, so they are
  not the first hits. Omitted when there are none, which is the ordinary
  relevance answer. **A caller counting recall should count `strict` hits and
  not the rest.** Measured over 93 two-word queries on a 2,422-session store:
  all 20 answers labelled `relevance` had a strict head of 1 to 9 sessions,
  and none of them had matched nothing.
- `total` and `capped` — how many sessions matched, and whether a cap hid some.
- `policy_withheld` — how many matching sessions this machine's trust policy
  kept out of the answer. Omitted when none were. Present on `search --json`,
  `last --json`, so an empty result can be told apart from a
  rule.
  Counting the returned hits measures the cap: that figure moves when the
  window's membership changes, whether or not retrieval improved.
  **`capped: false` means the response holds everything that matched, on every
  tier**; when it is `true`, `total` is the figure to read and the hit count is
  not.

`capped` is omitted when false. When it is true, `total` is the count before
policy scoping and reranking ran, because there is no way to know how those
would have treated the sessions the cap removed.

### `hits` is not a fixed window across tiers

The number of hits a tier returns is that tier's own decision, so the same
invocation can return 15 on one tier and 50 on another. Read `total` and
`capped` for coverage; the length of `hits` answers a different question.

- `exact`, `close`, `stemmed` and `semantic` serve the ranked result cap:
  `--limit N` (1–100), 15 by default, and `--all` for no cap. With `--all`,
  `total` equals the hit count.
- `relevance` ranks the candidate pool and serves the top 50. That bound belongs
  to the ranking rather than to output: it is applied during retrieval, and
  `--limit` and `--all` act on the result set downstream of it, so neither moves
  it. A relevance response can therefore come back `capped: true` with `--all`
  passed, and `total` can exceed the 50 hits it returned.

Reporting `total` as the length of that window instead of the pool behind it was
[#497](https://github.com/henryyu333/mss/issues/497): deeper queries all came
back `"total": 50, "capped": false`, which reads as "50 matched, none withheld"
in the one case where a consumer most needs to keep looking.

### Reading without a refresh (`--no-refresh`)

`mss search` and `mss show` refresh the index before they answer. A caller
that already refreshed — the manual-recall flow runs `mss index --quiet` once
at the start — can pass `--no-refresh`: the command then reads the index as it
was, takes no write lock, and cannot wait on another `mss` that is building.
Unlike a plain read it will not build an index that does not exist yet, nor
repair a damaged one; both refusals name `mss index`.

An answer served this way says so, in a `refresh` object that appears on the
search, candidate-list, and `show` envelopes alike:

```json
"refresh": {"refreshed": false, "last_refresh": "2026-10-07T00:21:05Z"}
```

`refreshed` is `false` on every envelope that carries the field — an answer
that refreshed omits it, so its presence is the signal. `last_refresh` is when
the index last walked this machine's stores, the same stamp `mss doctor`
compares stores against; it is omitted when the index records no walk (a store
an import built, or one that could not be read). The terminal gets the same
fact as a stderr line.

## `mss search --json`

Every search returns one envelope:

```json
{
  "schema_version": 2,
  "produced_by": "mss",
  "match": "found",
  "tier": "exact",
  "total": 391,
  "capped": true,
  "coverage": {"complete": true},
  "hits": [
  {
    "session": {
      "harness": "claude",
      "id": "abc123",
      "project": "myapp",
      "path": "/home/user/.claude/projects/.../session.jsonl",
      "started": "2026-01-02T03:04:05Z",
      "updated": "2026-01-02T03:10:00Z",
      "source": {"origin": "local", "instance": "workstation"},
      "messages": [
        {"role": "user", "text": "why does the parser fail on …", "time": "2026-01-02T03:04:05Z", "index": 17}
      ]
    },
    "count": 2,
    "snippets": ["matched text …"],
    "score": 1.5,
    "tier": "exact",
    "tier_detail": "",
    "superseded": "2026-07-19"
  }
  ]
}
```

`produced_by` is `"mss"` on every envelope. A copy of this answer sitting
inside a later transcript is mss's own output, not something a person said —
the marker is what keeps a later search from finding it again as evidence.

`match` says what the answer is: `found` when sessions matched the query,
`candidates` when nothing matched and the list is the relevance ranking,
`none` when there is nothing. It restates what `tier` and `strict` already
say, in the word a consumer decides on: `relevance` with no strict hits is
`candidates`, while a thin strict head inside one stays `found`.

`coverage` is present on every answer and reports what the refresh could not
see: `unread` names stores that exist but could not be read (with the reason,
for example `deepseek: zstd CLI not found`) — a harness with nothing on disk
is absent, not unread. `skipped` counts malformed records and failed files
per harness, `clipped` counts messages stored short of the transcript, and
`complete` is true only when none of those happened — and a requested
`--exclude-self` resolved (`self_requested` notes the request; `self_excluded`
is present only when the nonce matched, and its absence with `complete: false`
is the unexcluded signal).

Every message in a hit carries `index` — its 0-based position in the
session's records, the numbering `mss show --offset` and `--around` read —
so a consumer walks from the answer to the passage without re-counting.

The envelope's `tier` and each hit's `tier` are the same idea at two scopes,
which is why they share a name. The fallback flags stay alongside for readers
written against version 1:

```json
{
  "schema_version": 2,
  "tier": "close",
  "total": 4,
  "hits": [ … ],
  "fuzzy": true
}
```

Stemmed search may also include `variants`; semantic search sets `semantic`.
`superseded` (optional) carries the date of a newer same-project session whose
matches overlap this hit — an earlier-attempt signal. `reused` (optional)
counts recent agent recalls that served this session. `moved` (optional) is a
sentence saying how many of the files the session touched have commits since
it ended; only the first three hits are checked, and `MSS_MOVED=0` turns it
off. `lifecycle`, `lifecycle_note` and `lifecycle_at` (optional) on the hit
carry the note state recorded on the session.

### What a hit's `messages` are

The passages that matched, each with the message after it — the answer to what
matched — and at most twenty per hit. `messages_total` is how many messages the
session holds and `messages_capped` says the list is a selection; both are
omitted when the whole session fits. `snippets` is unchanged: the two or three
excerpts the text output prints.


Hit output bounds message windows rather than encoding every matching session's
entire transcript. `mss show --json` is the surface for reading a wider session
window and carries `offset`, `limit`, `total` and `returned`. Measured MSS
performance evidence is in [benchmarks.md](benchmarks.md), not inherited corpus
timing anecdotes.

`--limit N` bounds the ranked result set to 1–100 hits, on the tiers that serve
that cap (see [`hits` is not a fixed
window](#hits-is-not-a-fixed-window-across-tiers)). Every session has
`source.origin: "local"`; `source.instance` carries the stable operator-chosen
`MSS_SOURCE_INSTANCE` name when one is configured.

### `mss search --sessions`

The candidate list answers the other question a search can ask — which
sessions matched — without the excerpts or the hit cap. It forces JSON:

```json
{
  "schema_version": 2,
  "produced_by": "mss",
  "match": "found",
  "query": "needle",
  "total": 3,
  "sessions": [
    {
      "session": {"harness": "deepseek", "id": "aaaa", "project": "tmp/altu", "…": "…"},
      "hit_count": 4,
      "matched_indices": [0, 1, 2, 3]
    }
  ],
  "coverage": {"complete": true}
}
```

`sessions` is the matching set after the same filters (`--harness`,
`--project`, `--since`, `--role`, `--session`, `--exclude`): hits first in
hit order, then the rest by harness and id. `--sort updated` replaces that
order with last-updated first — identity breaking ties — applied before the
500-row cap, so a capped list drops the oldest rows. On the `found` tiers it
is the whole set; on a `candidates` answer it is the relevance ranking's
window — `total` and `capped` there describe the ranking, and past `500` rows
the list itself stops with `capped: true`. `--re` and termless queries keep
`hit_count` but omit `matched_indices`, which a pattern rather than terms
produced. `hit_count` is how many records matched; `matched_indices` are their
record numbers — each feeds `mss show <id> --harness <h> --around N` directly.

On a mixed relevance answer with a strict head, `--sessions` serves only those
verified matching sessions as `match: "found"`, with record positions that meet
the complete query. The untyped list does not append relevance-only neighbours
to that found set. A purely relaxed query still serves `match: "candidates"`.
Normal ranked `search --json` retains its mixed result window and each hit's
existing `strict` marker; its top-level `found` means at least one strict hit,
not that every ranked hit is evidence. No JSON fields or schema version change.

Co-occurrence substitutions and quoted-phrase relaxations are relevance
candidates, not strict matches. They keep `match: "candidates"` and `strict`
absent, even when every replacement or unquoted term appears in a session.

`--exclude <id-or-prefix>` is repeatable and removes the session plus its
lineage — the subagents it spawned and the forks that continue it — because
excluding one alone leaves the same conversation in the answer under another
id. `--exclude-self <nonce>` resolves the session that asked: a skill passes
a unique token it embedded in its own invocation, the session carrying it is
excluded with its lineage, and `coverage.self_excluded` reports whether the
token was found.

### The session object

`search`, `last` and `show` all carry the same session object. The examples
above show the fields that are always there; these are the rest, each omitted
when empty:

| Field | Meaning |
|---|---|
| `path` | file the session was read from — the transcript, or the store for the database-backed harnesses; opencode is the exception and gives the project directory it ran in |
| `title` | first user turn, elided to terminal width |
| `agent_title` | `title` came from the assistant because the session has no user turn |
| `touched` | the few files this session worked on most |
| `gave_up` | the session's own text reports something being tried and backed out |
| `words` | length of the whole session in words, as the index counted it; ranking normalises by it |
| `kind` | the harness's own word for a session an agent spawned — `sidechain` from Claude, `subagent` or `subagent_fork` from Grok, and whatever a harness mss has not met yet calls it; absent for sessions a person started |
| `parent` | the session this one was spawned from, where the harness records the edge itself — mss never infers one |
| `agent` | name of the agent that ran a spawned session |

`messages` is likewise absent rather than empty on `last`, which never returns
turns.

Inside a message, `time` is the turn's own stamp and is absent when the
transcript did not carry one — the zero time reads as a date in the year one
rather than as the absence of a date, and every surface mss prints shows `-`
for it instead.

## `mss last --json`

Recent session metadata uses a versioned envelope and never includes messages:

```json
{
  "schema_version": 2,
  "sessions": [
    {
      "harness": "codex",
      "id": "abc123",
      "project": "myapp",
      "started": "2026-01-02T03:04:05Z",
      "updated": "2026-01-02T03:10:00Z",
      "source": {"origin": "local", "instance": "workstation"}
    }
  ]
}
```

The existing positional count remains the bound, for example
`mss last 20 --json --harness codex`.

## `mss show <exact-id> --harness <name> --json`

Machine reads require the composite harness plus exact native session ID. They
return redacted index content in a bounded message window; the default limit is
50 and the maximum is 200.

```json
{
  "schema_version": 2,
  "produced_by": "mss",
  "session": {
    "harness": "codex",
    "id": "abc123",
    "project": "myapp",
    "started": "2026-01-02T03:04:05Z",
    "updated": "2026-01-02T03:10:00Z",
    "source": {"origin": "local", "instance": "workstation"},
    "messages": [
      {"role": "user", "text": "bounded redacted text", "time": "2026-01-02T03:04:05Z", "index": 0}
    ]
  },
  "window": {"offset": 0, "limit": 50, "total": 81, "returned": 50}
}
```

Use `--offset N --limit N` to page without parsing human output, or
`--around N` to centre the window on record number N — the same `index`
every message in a search hit carries; `--offset` and `--around` are
exclusive. `window.clipped` is present and true when the window holds a
message the index stored short of the transcript — or, on an index that only
kept the file count, when the session is known to hold one anywhere. An offset past the end
returns a session with no `messages` key and a `returned` count of zero.
A `--no-refresh` window carries the `refresh` object described under
[Reading without a refresh](#reading-without-a-refresh---no-refresh).
