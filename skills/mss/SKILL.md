---
name: mss
description: "只读搜索本机历史 Agent Session（mss 索引），把与关键词最相关的旧对话原文临时带回当前会话。仅在用户显式输入 /mss <内容> 时使用；用户未显式调用时绝不使用，也不因普通对话出现『之前』『我们讨论过』等词而触发。"
hide: true
---

# mss（手动历史会话搜索）

把过去 Session 的相关**原文**临时带回当前对话。只有用户显式输入 `/mss <内容>` 才运行；普通聊天、以及"之前 / 我们讨论过 / 某项目名"等词的提及，都不构成触发理由。

## 硬约束

- 只允许的**写**：mss 自己的索引。禁止改写 / 删除任何 Session 文件，禁止写 memory、PROJECT.md 或其他文件，禁止 resume / 切换旧 Session。
- 找不到足够证据就说"没有找到"，列出试过的词，不得补全历史。
- 全程用 `--json`，按 `docs/json-output.md` 的信封字段读答案。

## 步骤

1. **定范围**：从 `/mss` 的内容里拆出项目名、时间范围、线索（人名 / 术语 / 命令 / 原话）。内容有歧义时（同一个词可能指不同项目或对象）先问一次再搜。默认先在当前项目或用户指明的项目里找；证据不足再扩大范围，并在汇报里说明扩大了。

2. **检索词**：自行扩展同义词、中英文变体和相关名词，准备 2–3 组查询。

3. **列候选**：先生成一个本轮 nonce，再搜候选全集：
   ```bash
   NONCE="mss-self-$(uuidgen | tr A-F a-f | cut -c1-12)"   # 或等价的随机串
   mss search --sessions --exclude-self "$NONCE" --json "<query>"
   ```
   - nonce 必须是本轮独有、不会在别处出现的字符串；它通过这条命令行被写进本会话的转录，mss 据此把**当前会话**连同它的子代理和分叉一起排除。之后本轮每次 mss 调用都带同一个 nonce。
   - 另有要排除的 session 用 `--exclude <id-or-prefix>`（可重复），它会连带排除其子代理与分叉。
   - 读信封：`match` 是 `found` / `candidates` / `none`；`coverage.self_requested` 存在而 `self_excluded` 缺席表示当前会话没能排除（转录里还没写入这条命令行，此时 `complete` 为 `false`），汇报时必须说明"当前会话未排除"，不得静默。`coverage` 里 `unread`（存在的源没读到；没有该 harness 的数据不算） / `skipped` / `clipped` 说明本次检索没覆盖的部分，`complete: true` 表示无缺口。
   - `sessions` 是匹配全集（`found` 时不受条数上限，最多 500 行并以 `capped` 说明），每行有 session 元数据、`hit_count` 和 `matched_indices`（记录序号，与 `show` 同一套编号；`--re` 或无检索词的查询只有 `hit_count`）。
   - 加过滤器收窄：`--project` / `--since` / `--harness` / `--role`。

4. **读原文**：对候选用 `matched_indices` 里的序号直接读命中附近，不要只看搜索摘要下结论：
   ```bash
   mss show <id-prefix> --harness <name> --json --around <index> --limit 40
   ```
   每条消息带 `index`；`window.clipped` 为 `true` 表示窗口内有消息被索引截断，需要更后面的内容就加大 `--limit` 或改用 `--offset` 向后翻。候选太多时说明读了哪些、跳过了哪些、为什么跳过。

5. **汇报**：
   - 按时间线写；每条结论附 harness、日期、session id 和原文引用。
   - 历史原话和基于历史的推断分开写，不得混为一谈。
   - 被后来推翻的决定标"已被 <session/日期> 推翻"。
   - 结尾写覆盖范围：搜了哪些词和项目、读了几个 session、`coverage` 里的缺口、当前会话是否已排除。

6. **Fallback（仅当 coverage 报告某数据源未读时）**：只有 `coverage.unread` 指明某 harness 的数据源这次没读到，才允许对**该数据源的原始目录**做只读 `rg`（`rg -l --hidden --no-ignore`），并在汇报中说明。其它情况一律以 mss 为准；`match: "none"` 就是没有找到，不 fallback。

- 若 `mss` 不在 PATH 或索引为空，直接说历史搜索不可用，不要臆造结果。
