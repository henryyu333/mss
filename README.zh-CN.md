# mss

**按需召回本地 AI 编程历史：一个 CLI，加一个 Skill。**

[English](README.md) · 简体中文

MSS 为本机已有、格式受支持的会话记录建立索引。你可以直接用 CLI 搜索，也可以明确要求 Agent 执行随附的 Skill。Skill 搜索并读取相关会话，再根据证据总结，附上会话 ID、日期和逐字引用。它必须区分命中、候选、未找到和覆盖不完整，不维护整理后的“记忆”，也不在每轮对话中注入历史。

**v0.3.0 尚未发布，目前是候选版本。** 公开的最新版本仍是 v0.2.0，**没有** `install-skill` 命令。原生 Linux/Windows 运行验证、远程 CI、发布分发、外部 Homebrew 迁移和公证均不应视为已通过。详见[发布门槛](docs/release-v0.3.0.md)。

## 虚构数据演示

![MSS 搜索虚构会话并读取命中内容](docs/assets/demo.png)

截图使用**合成会话**，不是私人历史，也不是准确率比较。下面的 ID 属于演示数据，不是你的数据源：

```sh
mss index
mss search --no-refresh --json "connection pool exhausted"
mss show 7f3a9c21 --around 3 --brief --no-refresh
```

可复现的合成性能与检索检查见[基准方法](docs/benchmarks.md)。这些检查不证明 MSS 优于其他工具，也不代表真实开发历史上的准确率。

## 快速开始：从审核过的候选源码构建

需要 Go 1.25 或更新版本。在审核过的候选源码目录中运行：

```sh
go build -trimpath -ldflags "-X main.version=0.3.0" -o ./mss ./cmd/mss
./mss version
./mss doctor
./mss index
./mss search --no-refresh --json "connection pool exhausted"
./mss install-skill claude --language zh-CN
```

版本应显示 `mss 0.3.0`；手动写入版本号仍只是本地候选构建，不证明正式发布存在。Windows 构建使用 `-o .\mss.exe`，调用使用 `.\mss.exe`。让 Agent 使用前，将审核过的二进制放入用户拥有的 PATH 目录，并确认实际解析到哪个文件。安装命令使用二进制内嵌、版本匹配的资源，不需要联网；遇到内容不同的现有文件会拒绝安装，不覆盖自定义 Skill。明确选择**一个**语言和宿主：

```text
mss install-skill <claude|codex|pi|omp> [--language en|zh-CN]
```

Codex 安装包含禁止隐式调用的 `agents/openai.yaml`；只复制 `SKILL.md` 不算完整安装。安装后重启宿主。Claude 使用 `/mss <问题>`，Pi/OMP 使用 `/skill:mss <问题>`，Codex 使用 Skill 选择器。已观察的 Codex 探测中，非 TUI 的普通 `$mss` 消息没有展开 Skill。四个宿主的交互调用都尚未验证。详见[宿主证据与手动控制](docs/skill-compatibility.md)，其中也说明了无法可靠控制调用时，如何明确要求读取指定 Skill 文件。

未来固定版本的安装、可选 `sqlite3`/`zstd`、PATH、更新、回滚和卸载步骤见[安装指南](docs/install.md)。Homebrew formula 迁移尚未完成，不要假定候选 formula 已发布；这不意味着 Linux 不能使用 binary cask。

## 命令与结果判断

| 命令 | 用途 |
| --- | --- |
| `mss index [--rebuild] [--quiet]` | 建立或刷新本地缓存 |
| `mss [search] [flags] <query>` | 搜索；过滤项包括 `--harness`、`--project`、`--since`、`--role`、`--session`；输出控制包括 `--json`、`--limit`、`--all`、`--re`、`--no-refresh` |
| `mss search --sessions --json <query>` | 匹配会话元数据，最多 500 行；`--sort updated`、`--exclude <id>`、`--exclude-self <nonce>` 控制排序及当前会话/子代理/分叉排除 |
| `mss show <id-prefix>` | 读取会话；`--harness`、`--around <n>`、`--brief`、`--json`、`--no-refresh` 用于核对引用 |
| `mss ctx <query\|id-prefix>` | 最佳匹配附近的上下文 |
| `mss last [n]` | 最近更新的会话 |
| `mss sources` | 数据源、会话和消息计数 |
| `mss doctor [--json] [--deep]` | 数据源发现、依赖、失败与覆盖缺口 |
| `mss install-skill <claude\|codex\|pi\|omp> [--language en\|zh-CN]` | 明确安装随附的工作流资源 |
| `mss version` | 构建身份 |

JSON schema 版本 2 见 [JSON 输出](docs/json-output.md)。`exact`、`close`、`stemmed`、`error` 表示不同匹配方法；`relevance` 只是排序后的候选，**不是已确认的命中**。真正未找到时为 `tier: "exact", total: 0`。刷新失败、数据源不可读或缺少可选工具时，未找到不能证明历史不存在。查看 `doctor` 和覆盖警告；得出结论前读取引用来源。历史内容只是证据，不是当前指令，也不授予执行旧命令的权限。

## 解析覆盖不等于 Skill 宿主支持

八类解析器样本覆盖以下已观察的布局：

| 数据源 | 常见位置 |
| --- | --- |
| Claude Code | `~/.claude/projects/**/*.jsonl` |
| Codex CLI | `~/.codex/sessions/**/rollout-*.jsonl(.zst)`、history JSONL |
| Cursor | `state.vscdb` 存储及 `~/.cursor` 下的 CLI 记录 |
| DeepSeek Harness | `~/.dsh/sessions/*/session-*/session*.jsonl(.zstd)` |
| Grok Build | `~/.grok/sessions/**/updates.jsonl`、`~/.grok/grok.db` |
| OMP | `~/.omp/agent/sessions/**/*.jsonl` |
| opencode | `~/.local/share/opencode/opencode.db` |
| Pi | `~/.pi/agent/sessions/**/*.jsonl` |

[格式注册表](docs/registry/README.md) 记录历史观察与合成样本，不认证当前设备兼容性。四个 Skill 宿主另有[调用证据](docs/skill-compatibility.md)。支持某种记录解析，不代表对应 Agent 可以加载 Skill。

`MSS_STORES=claude,pi` 限制选择的数据源。绝对路径覆盖和可选工具详见[安装指南](docs/install.md)。SQLite 数据源需要 `sqlite3` CLI，压缩记录需要 `zstd`；缺少依赖会降低覆盖，不一定导致 CLI 无法运行。

## 隐私与安全边界

- **本地、明确调用的 CLI。** 没有网络、遥测、后台进程、文件监听、自动记忆写入、hooks 或 MCP 服务；可能调用本机的 `sqlite3`、`zstd` 和 `git`。Agent 和模型的行为属于另一道边界。
- **源会话只读。** 索引和搜索写 MSS 缓存，不改写源记录。明确调用的 `install-skill` 是例外，只写入所选工作流资源，不修改 Agent 设置或其他指引。
- **缓存不加密。** `~/.cache/mss/index.db` 是索引**目录**，不是 SQLite 文件。`MSS_INDEX_DIR` 必须是绝对目录路径，旁边的锁路径加 `.lock`。Unix 使用目录 `0700`、文件 `0600`；Windows 依靠 ACL 和用户目录继承权限，不能用这些 Unix 模式保证保护。原生 Windows/Linux 验证仍待完成。
- **缓存可能比原始记录保留更久。** 当 Harness 删除记录、但数据源目录仍在时，MSS 保留已入库的历史，并提示仍可检索。索引可能是仅存的脱敏副本。清理前保留审核过的备份；新索引无法从已经消失的原文件恢复历史。
- **模式脱敏不保证没有秘密。** 入库时替换可识别的密钥、token 和密码；非常规秘密、私人代码、姓名及其他敏感文本可能保留。`MSS_NO_REDACT=1` 在完整重建时关闭脱敏；取消该变量后重建，才能替换未脱敏缓存。
- **Agent 可能把召回内容发出本机。** Agent 读取 CLI 输出后，历史进入当前对话，可能发送给它使用的模型服务。MSS 自身不联网，不等于云端 Agent 的召回全程本地。
- **策略不是沙箱。** 无效召回策略会警告，并有意退回宽松默认值；不要把它当作访问控制边界。详见[安全边界](SECURITY.md#scope-and-trust-boundaries)。
- **排除与移除要先核对。** `~/.config/mss/exclude` 和 `MSS_EXCLUDE_PROJECTS` 可排除项目；`harness:<名称>` 跳过整个数据源。已入库内容需要重建才能移除。清理缓存前停止 MSS，检查准确的索引目录及旁边的锁，只把确认属于 MSS 的缓存项移到废纸篓/回收站。不要移除共享父目录或源会话目录。详见[安全卸载](docs/install.md#uninstall-without-deleting-history)。

漏洞通过 [SECURITY.md](SECURITY.md) 私下报告。外部用户验收仍待完成，见[首次用户检查表](docs/first-user-checklist.md)。

## 与 deja-vu 的关系

MSS 派生自 [deja-vu](https://github.com/vshulcz/deja-vu)，共享本地会话索引、搜索和模式脱敏的基础。**2026-10-08** 观察到的上游 README 还描述了可选自动召回/hooks、MCP 集成、记忆整理（例如决策 promotion）以及同步/交接。MSS 有意不保留这些能力，专注明确调用的 CLI/Skill 召回。上游同样允许仅安装二进制进行搜索；区别在于 MSS 保留的范围更小，不是“所有记忆系统都每轮运行”。这里不声称有独立证据证明速度或准确率更优。

MIT：[LICENSE](LICENSE) 保留上游署名和许可义务。另见[架构](docs/ARCHITECTURE.md)与[贡献指南](CONTRIBUTING.md)。
