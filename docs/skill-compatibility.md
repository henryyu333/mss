# Manual Skill compatibility

Checked on 2026-10-08. Session **parsing** support and Skill **invocation** support
are different contracts. A parser fixture does not prove that an agent loads a
Skill correctly. MSS ships one workflow in English and Chinese, not an agent
plugin, hook, MCP server, or automatic memory system.

## Invocation controls and evidence

| Harness | User installation path | Manual entry | Automatic-selection control | Evidence / remaining check |
| --- | --- | --- | --- | --- |
| Claude Code | `~/.claude/skills/mss/SKILL.md` | `/mss <question>` | `disable-model-invocation: true` | [Official specification](https://code.claude.com/docs/en/skills#control-who-invokes-a-skill); local CLI 2.1.284 present, interactive invocation not exercised |
| OMP | `~/.omp/agent/skills/mss/SKILL.md` | `/skill:mss <question>` | `disable-model-invocation: true` (normalized to hidden metadata) | [Official specification and implementation](https://github.com/can1357/oh-my-pi/blob/main/docs/skills.md); local CLI 18.8.0 present, interactive invocation not exercised; skill commands must be enabled |
| Pi | `~/.pi/agent/skills/mss/SKILL.md` | `/skill:mss <question>` | `disable-model-invocation: true` | Pi 1.0.4 real loader: both languages discoverable and absent from automatic prompt; interactive command still needs acceptance; [specification](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/skills.md) |
| Codex CLI | `~/.agents/skills/mss/SKILL.md` **and** `agents/openai.yaml` | select MSS with `/skills` / `$` picker; see non-TUI limitation below | `policy.allow_implicit_invocation: false` in the companion YAML | Codex 0.160.1 `debug prompt-input`: both languages absent from ordinary-chat prompt; TUI picker not exercised; [specification](https://learn.chatgpt.com/docs/build-skills) |

`hide: true` was OMP-specific, not a portable control. It left the former MSS
Skill in Pi 1.0.4 and Codex 0.160.1 automatic-selection context. Merely saying
“manual only” in a description is not a host-level invocation control.

Codex ignores the Claude/Pi invocation flag for this purpose. Installing only
`SKILL.md` without its companion policy is **not** a manual-only Codex install.
OMP profiles and custom agent directories need the same files in their configured
Skill root rather than the default path above.

These are bounded claims, not a certification of every harness release or of model
obedience. Hidden metadata prevents normal automatic selection; it is not a
security sandbox preventing a model from reading a known file or running a CLI.

## Codex non-TUI limitation

On Codex 0.160.1, the policy suppresses ordinary-chat advertisement, but a plain
`$mss` in `debug prompt-input` does **not** expand the Skill body. This matches the
still-open [upstream issue #40600](https://github.com/openai/codex/issues/40600),
reported for 0.149.0. Do not advertise `/mss` or plain non-TUI `$mss` as a tested
Codex entry point.

An explicit file-read request avoids relying on implicit routing:

```text
Read ~/.agents/skills/mss/SKILL.md and use MSS to recall: why did we drop the redis cache?
```

The user explicitly authorizes recall in this request. Actual model/tool execution
and the TUI picker remain external acceptance checks, not results of the offline
prompt probe.

## Unknown or older harnesses

If a host ignores either required invocation control, **do not install MSS in an
auto-discovered skills directory**. Keep the release's Skill outside discovery,
and explicitly ask the agent to read that exact file and perform recall. The CLI
can always be used directly without installing a Skill:

```sh
mss index
mss search --sessions --no-refresh 'redis cache'
```

No automatic installer scans for agents or edits an agent's settings, instructions,
hooks, or authentication. Choosing an installation target is an explicit action.

## Reproduce the offline compatibility probes

These use real installed host code, synthetic temporary homes and no model request.
They do not install a harness or contact a model provider:

```sh
python3 tools/check-codex-skill.py
node tools/check-pi-skill.mjs /absolute/path/to/pi/dist/core/skills.js
```

For bundled Pi installations, pass the bundle module exporting `loadSkillsFromDir`
and `formatSkillsForPrompt` instead. On the measured Pi 1.0.4 installation this was
`dist/bundle/chunks/chunk-H33F2TZD.js`. The chunk name is installation-specific, not
an MSS dependency. Both probes exercise the old unsafe control as well as the
shipped policy; they fail rather than silently skip if the host API is unavailable.
Node and Python are **verification tools**, not MSS runtime dependencies.

Before claiming interactive support for a harness version, use a synthetic Session
and record: harness version, command discovery, ordinary conversation causing no
MSS command, explicit invocation causing recall, source citation, and an injected
historical instruction being quoted rather than executed. Do not send private
history or credentials during this check.
