# mss

[English](README.md) | 简体中文

搜索你的编程 Agent 过去的会话：由你开口时才搜，而不是由 Agent 自己猜。

`mss` 是一个 Go 程序。它为各个编程 Agent 已经留在本机磁盘上的会话记录建立索引，并把匹配的对话以**原文**带回来。只有你运行它时它才工作：没有后台进程，没有 Agent 自行写入的记忆，也不会把你没要求的历史塞进对话。会话文件保持在各 Agent 原来存放的位置；索引只是放在本机、随时可以重建的缓存。

## 安装

```sh
go install github.com/henryyu333/mss/cmd/mss@latest
```

或者从源码目录构建：

```sh
make build   # 生成 ./mss
```

以 SQLite 存储的数据源（opencode、Cursor）通过 `PATH` 上的 `sqlite3` 命令读取；其他 Agent 的记录都是普通文件。不依赖 CGO。

## 快速上手

```sh
mss index                          # 建立或更新索引
mss "connection pool exhausted"    # 搜索全部会话
mss --harness claude --since 30d "panic in indexer"
mss last 20 --role user            # 最近二十条用户消息
mss show 01a00feb --harness codex  # 按 id 前缀读取一个会话
mss ctx "schema migration rollback" > context.md
mss sources                        # 读取了哪些数据源、各有多少内容
```

没有匹配时会直接说明，不会拿相近的结果冒充。每条结果带一个 `tier`：
- `exact`：精确匹配；
- `close` / `stemmed`：拼写纠正或词形归一后的匹配；
- `relevance`：只是按词重叠排出的最近邻，**不是真正的匹配**；
- `error`：按错误签名匹配。

真正没找到是 `tier: "exact", total: 0`。`--json` 输出同样的结构供脚本解析，字段说明见 [`docs/json-output.md`](docs/json-output.md)（英文）。

## 命令

| 命令 | 作用 |
| --- | --- |
| `mss index [--rebuild] [--quiet]` | 建立或增量更新索引 |
| `mss [search] [flags] <query>` | 搜索；可用 `--json`、`--harness`、`--project`、`--since`、`--role`、`--session`、`--limit`、`--all`、`--re`、`--no-refresh` |
| `mss search --sessions --json <query>` | 列出全部匹配会话，只含元数据：命中次数和命中消息的位置，最多 500 行。`--sort updated` 按最后更新时间从新到旧排列；`--exclude <id>`、`--exclude-self <nonce>` 会把某个会话连同它的子代理和分叉一起排除 |
| `mss show <id-prefix>` | 按结果里的 id 读取一个会话；`--around <n>` 从第 `n` 条消息附近开始读；`--brief` 每条消息一行头部（序号、角色、时间）+ 截断正文，便于快速浏览；`--no-refresh` 直接读现有索引，不先刷新 |
| `mss ctx <query\|id-prefix>` | 取最佳匹配附近的一大段上下文，方便贴进对话 |
| `mss last [n]` | 最近更新的会话 |
| `mss sources` | mss 会读的每个数据源，以及会话数和消息数 |
| `mss doctor [--json] [--deep]` | 安装情况、找到了什么、哪些没读到 |
| `mss version` | 版本信息 |

## 支持的 Agent

| Agent | 会话存放位置 |
| --- | --- |
| Claude Code | `~/.claude/projects/**/*.jsonl` |
| Codex CLI | `~/.codex/sessions/**/rollout-*.jsonl(.zst)`、`~/.codex/history.jsonl` |
| opencode | `~/.local/share/opencode/opencode.db` |
| Cursor | Cursor 的 `state.vscdb` 存储，以及 `~/.cursor` 下的 CLI 记录 |
| Grok Build | `~/.grok/sessions/**/updates.jsonl`、`~/.grok/grok.db` |
| pi | `~/.pi/agent/sessions/**/*.jsonl` |
| omp | `~/.omp/agent/sessions/**/*.jsonl` |
| DeepSeek Harness | `~/.dsh/sessions/*/session-*/session*.jsonl(.zstd)` |

每个数据源都可以用各自的 `MSS_…_ROOT` 环境变量指向别处；`MSS_STORES=claude,pi` 只读取列出的数据源。支持的格式和测试样本见 [`docs/registry/`](docs/registry/)（英文）。

## 工作原理

- **索引只是缓存。** `mss index` 读取会话文件，在 `~/.cache/mss/index.db` 里存一份脱敏副本，并增量更新：只是变长的记录从上次安全的位置接着读，发生变化的数据源会替换它涉及的会话。删掉索引的代价只是重建一次。
- **写入时脱敏。** API key、token 和看起来像密码的字符串在建索引时就被替换，所以 `show` 和 `ctx` 不会把它们返回出来。`mss doctor` 会报告每个数据源读到了什么，包括读不了的行。
- **数据不出本机。** 没有任何网络功能：没有后台进程、不上传、没有 MCP 服务、没有 hooks。搜索只是对本机文件的本地查询。

常用环境变量：`MSS_INDEX_DIR`（索引位置）、`MSS_STORES`（读取哪些数据源）、`MSS_STORE_TIMEOUT`（每个数据源的读取时限）、`MSS_INCLUDE_SUBAGENTS`，以及上面提到的各数据源路径变量。

## 给 Agent 使用

`mss` 设计为**只在明确要求时**运行：由人输入 `/mss <内容>`，或由脚本明确调用。它和记忆系统正好相反：没人要求时，什么都不搜、不注入、不记住；Agent 得出的结论也不会被保存。

[`skills/mss/SKILL.md`](skills/mss/SKILL.md) 是配套的 skill：
1. 用 `search --sessions` 列出候选会话；
2. 用 `show --json --around` 读命中位置附近的原文；
3. 按时间线汇报：每条结论注明 Agent、日期和会话 id，标出后来被推翻的决定和转述，并说明这次搜索没覆盖到的部分（JSON 结果里的 `coverage`）。

## 许可证

MIT，见 [`LICENSE`](LICENSE)。
