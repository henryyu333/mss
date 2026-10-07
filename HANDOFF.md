# mss 当前状态与进度（Handoff）

最后更新：2026-10-07。写给自己：下次打开这个仓库时，从这里继续。

## 项目是什么

mss 是给编程 Agent 用的**按需历史召回引擎**：为 Claude Code、Codex、Cursor、opencode、Grok、pi、omp、DeepSeek 留在本机的会话记录建索引，只在人或脚本明确要求时才搜索，返回逐字原文。

定位四原则：**Local only · No daemon · No MCP · Explicit recall**。永远保持"一个 binary + 一个 skill"（`skills/mss/SKILL.md`），不往记忆系统或会话管理器方向走。对比对象：Wake（iAmCorey/Wake，给人看的 Rust+GPUI 会话管理器，有 MCP 和桌面 App）——mss 是给 Agent 的显式检索原语，不做 GUI。

## 发布状态（截至 v0.2.0）

- **仓库**：https://github.com/henryyu333/mss，About 简介和 Topics（cli、coding-agents、claude-code、codex、cursor、session-search、ai-history、golang、agent-skills）已设置。
- **Release v0.2.0**：https://github.com/henryyu333/mss/releases/tag/v0.2.0，6 个平台二进制（macOS/Linux/Windows × amd64/arm64），每个压缩包带 `SKILL.md` 中英版和 `README` 中英版。自 v0.2.0 起 `LICENSE` 与 README 均含 deja-vu 署名（MIT 义务已履行）。
- **Homebrew**：`brew install henryyu333/tap/mss` 可用。tap 在 https://github.com/henryyu333/homebrew-tap（Casks/mss.rb）。二进制未公证，cask 里有 postflight 去 quarantine。
- **CI**：`.github/workflows/ci.yml`，push/PR 时在 macOS 和 Linux 跑 gofmt、vet、全部测试，需要 sqlite3 和 zstd。
- **发版**：`.github/workflows/release.yml`，推 `v*` tag 由 GoReleaser（`.goreleaser.yaml`）自动构建发布。版本号用 ldflags 打进 `mss version`；`go install @vX` 也能显示 tag。

## 下次发版怎么做

1. 提交代码，本地 `go test ./...` 全绿（本机已装 zstd，相关测试不会被跳过）。
2. `git tag -a v0.3.0 -m "…" && git push origin v0.3.0`，release workflow 自动发布。
3. **brew 配方目前是手动推的**：等 release 出来后，从 Release 下载 checksums.txt，更新 homebrew-tap 里 `Casks/mss.rb` 的版本号和 4 个 sha256（模板见该文件，与 GoReleaser 生成的一致），提交推送即可。
4. **想要自动更新配方**：在 GitHub Settings → Developer settings 建一个只对 `homebrew-tap` 有 Contents 读写权限的 fine-grained token，存成 mss 仓库的 secret `HOMEBREW_TAP_GITHUB_TOKEN`。`.goreleaser.yaml` 已写好支持，token 放上即生效（`skip_upload` 在没有 token 时为 true，不会让发版失败）。

## 遗留问题（不紧急）

- `docs/ARCHITECTURE.md` 第 83–104 行描述的 `mss blame` / `--attribution` / `--git-note` 在代码里不存在（疑似迁移前的旧功能），`cmd/mss/main.go` 注释里还有 `how`、`blame`、"MCP tools" 的残留提法。需要清理文档。
- 代码里有大量 `#2251`、`#832` 这类编号（约 780 个不同编号），GitHub 上没有对应 issue，像是从别处迁移来的内部编号。公开仓库里对不上，可考虑清理或保留。
- `AGENTS.md` 在公开仓库根目录，是写给 AI 的工作说明，可留可删。

## 本机环境备注

- `mss` 通过 brew 装在 `/opt/homebrew/bin/mss`；`~/.local/bin/mss` 已同步为同一二进制。
- 本机已装 `zstd`（brew 装 mss 时自动带入），DeepSeek 的 8 个会话和压缩的 Codex rollout 现在能读；之前 `mss sources` 里"never read"的条目下次 `mss index` 会补进索引。
- 测试用的临时工具装在 `/tmp/zbin`、`/tmp/zstdx`、`/tmp/gobin`，重启后自动消失，不用管。
