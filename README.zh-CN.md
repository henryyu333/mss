<h1 align="center">mss</h1>

<p align="center">
  <strong>Search your AI coding history. On demand.</strong><br>
  <sub>按需搜索你的 AI 编程历史。</sub>
</p>

<p align="center">
  <a href="https://github.com/henryyu333/mss/releases/latest"><img src="https://img.shields.io/github/v/release/henryyu333/mss?style=flat-square" alt="Release"></a>
  <a href="https://github.com/henryyu333/mss/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/henryyu333/mss/ci.yml?branch=main&label=CI&style=flat-square" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/henryyu333/mss?style=flat-square" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-007AFF?style=flat-square" alt="Platform">
</p>

<p align="center">
  <a href="README.md">English</a> · 简体中文
</p>

---

**mss 是给编程 Agent 用的按需历史召回引擎。** 它为 Claude Code、Codex、Cursor、opencode 等已经留在你本机的会话记录建立索引，有人开口时，把相关对话**逐字原文**带回来。

历史不是记忆。mss 不会自动记住，也不会自动召回，更不会往没人要的上下文里塞东西。Agent 需要历史时，精确地查；平时，mss 什么都不做。

```sh
mss index                                   # 刷新本地索引
mss "connection pool exhausted"             # 之前在哪儿遇到过？
mss show 01a00feb --around 42 --brief       # 在命中位置附近读那个会话
```

### 只在本机 · 没有后台进程 · 没有 MCP · 只在明确要求时召回

- **只在本机**：只读你机器上已有的文件；不联网、不上传、不需要账号。
- **没有后台进程**：两次调用之间什么都不运行；没有文件监听、没有 hooks、不在后台写入。
- **没有 MCP**：就是一个普通命令行，任何 Agent 都能通过 shell 调用；不用注册，也不用保持运行。
- **只在明确要求时召回**：只有人或脚本明确要求时才搜。没找到就说没找到，不拿相近结果冒充命中。

### 它不是记忆系统，也不是会话管理器

| | 记忆系统 | 会话管理器 | **mss** |
| --- | --- | --- | --- |
| 给谁用 | Agent，自动 | 人，通过图形界面 | **Agent，在明确要求时** |
| 何时运行 | 每一轮对话 | 应用开着时 | **只在被调用时** |
| 返回什么 | Agent 自己写的摘要 | 可浏览的会话记录 | **逐字原文，附会话 id 和日期** |
| 往上下文里加什么 | 自动注入的召回 | — | **没人要求就什么都不加** |

mss 始终只是一个 binary 加一个 skill：

- **binary** 是搜索引擎；
- **[skill](skills/mss/SKILL.md)** 教 Agent 正确使用它：只在 `/mss <内容>` 时运行、只刷新一次索引、逐字引用、说明没覆盖到的部分。

## 安装

**Homebrew**（macOS、Linux）

```sh
brew install henryyu333/tap/mss
```

**预编译二进制**：从 [最新 Release](https://github.com/henryyu333/mss/releases/latest) 下载对应系统的压缩包，解压后把 `mss` 放进 `PATH`。每个压缩包里也带了 `SKILL.md`。

**从源码安装**（Go 1.25+）

```sh
go install github.com/henryyu333/mss/cmd/mss@latest
```

运行时依赖：存在 SQLite 里的数据源（opencode、Cursor、Grok）通过 `sqlite3` 命令读取；zstd 压缩的记录（较新的 Codex rollout、DeepSeek Harness）通过 `zstd` 命令读取。macOS 自带 `sqlite3`，`brew install zstd` 可以补上另一个。缺少它们时 mss 照常运行，`mss doctor` 会列出读不了的数据源。

## 安装 skill

让 Agent 用对 mss 靠的是 skill。把 [`skills/mss/`](skills/mss/) 复制到你的 Agent 的 skills 目录，例如：

```sh
mkdir -p ~/.claude/skills/mss
curl -fsSL https://raw.githubusercontent.com/henryyu333/mss/main/skills/mss/SKILL.md \
  -o ~/.claude/skills/mss/SKILL.md
```

之后明确地要历史：`/mss 当时为什么去掉了 redis 缓存`。普通对话里即使出现"之前""我们讨论过"，skill 也不会触发。

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

每条结果带一个 `tier`：`exact` 是精确匹配；`close` / `stemmed` 是拼写纠正或词形归一后的匹配；`relevance` 只是按词重叠排出的最近邻，**不是真正的匹配**；`error` 是按错误签名匹配。真正没找到是 `tier: "exact", total: 0`。`--json` 输出同样的结构供脚本解析，字段说明见 [`docs/json-output.md`](docs/json-output.md)（英文）。

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
- **从不改写会话文件。** 会话文件留在各 Agent 原来存放的位置，mss 只读不写。

常用环境变量：`MSS_INDEX_DIR`（索引位置）、`MSS_STORES`（读取哪些数据源）、`MSS_STORE_TIMEOUT`（每个数据源的读取时限）、`MSS_INCLUDE_SUBAGENTS`，以及上面提到的各数据源路径变量。

内部实现见 [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)（英文）。

## 许可证

MIT，见 [`LICENSE`](LICENSE)。
