---
name: mss
description: "只读搜索本机历史 Agent Session（mss 索引），把与关键词最相关的旧对话原文临时带回当前会话。仅在用户显式输入 /mss <内容> 时使用；用户未显式调用时绝不使用，也不因普通对话出现『之前』『我们讨论过』等词而触发。"
hide: true
---

# mss（手动历史会话搜索）

把过去 Session 的相关**原文**临时带回当前对话。只有用户显式输入 `/mss <内容>` 才运行；普通聊天、以及"之前 / 我们讨论过 / 某项目名"等词的提及，都不构成触发理由。

## 硬约束

- 只允许的**写**：mss 自己的索引。禁止改写 / 删除任何 Session 文件，禁止写 memory、PROJECT.md 或其他文件，禁止 resume / 切换旧 Session。
- 索引**只在开头刷新一次**（`mss index --quiet`）；之后所有 search / show 都带 `--no-refresh`：直接读现有索引，不抢写锁，并行命令互不排队。漏掉它就会退回"先刷新再回答"，又回到排队等待。
- 找不到足够证据就说"没有找到"，列出试过的词，不得补全历史。
- 检索读 `--json` 信封，按 `docs/json-output.md` 的字段读答案；批量浏览用 `show --brief`；引用原文时以 `--json` 读到的逐字文本为准。

## 步骤

1. **定范围**：从 `/mss` 的内容里拆出项目名、时间范围、线索（人名 / 术语 / 命令 / 原话）。内容有歧义时（同一个词可能指不同项目或对象）先问一次再搜。默认先在当前项目或用户指明的项目里找；证据不足再扩大范围，并在汇报里说明扩大了。

2. **nonce 先落地，再刷新一次索引**：先单独跑一条命令，把随机标记打印进转录：
   ```bash
   echo "mss-nonce: $(uuidgen | tr 'A-F' 'a-f' | cut -c1-12)"
   ```
   从输出里读出**字面值**（例：`7f3a91c2d4e8`），后面每条命令都写字面值、不用变量。然后**只跑这一次**刷新：
   ```bash
   mss index --quiet
   ```
   成功时 stdout 上没有输出，等它返回即可（无变化时约 1–2 秒）；stderr 上的"跳过某文件 / 某源读不到"不算失败，那是在说明覆盖缺口。
   - nonce 必须在 `mss index` **之前**已写进转录（这条 `echo` 的命令行和输出），`--exclude-self` 才能在不刷新的搜索里认出当前窗口。
   - 若某次搜索的 coverage 显示 `self_requested` 存在而 `self_excluded` 缺席（转录落盘时序不同），只补一次 `mss index --quiet`，再原样重跑该搜索即可；不要每次都重刷。

3. **检索词**：自行扩展同义词、中英文变体和相关名词，准备 2–3 组查询。同一批查询一次跑完，不要为同一批候选反复小查询。

4. **列候选（一次拿全）**：
   ```bash
   mss search --sessions --no-refresh --sort updated --exclude-self 7f3a91c2d4e8 "<query>"
   ```
   - `--sort updated` 按最后更新时间从新到旧排。用户问"最后怎么样了 / 怎么处理了 / 现在什么状态"时，**先读最上面（最新）的几条**，再按需要细读；不要只挑 `hit_count` 大的——关键会话可能命中很少、项目名还是别的目录。
   - `--sessions` 的输出本来就是 JSON 信封，不必再加 `--json`。
   - 单行是硬要求：mss 有意不索引多行命令（heredoc、多行脚本被当作"说的都在产出里"）。要解析 JSON 就写成同一行的 `python3 -c '…'`；能用 `--brief` 就不必解析 JSON。
   - 另有要排除的 session 用 `--exclude <id-or-prefix>`（可重复），它会连带排除其子代理与分叉。
   - 读信封：`match` 是 `found` / `candidates` / `none`；`coverage.self_requested` 存在而 `self_excluded` 缺席表示当前会话没能排除（此时 `complete` 为 `false`），汇报时必须说明"当前会话未排除"，不得静默。`coverage` 里 `unread`（存在的源没读到；没有该 harness 的数据不算） / `skipped` / `clipped` 说明本次检索没覆盖的部分，`complete: true` 表示无缺口。
   - `refresh.refreshed` 为 `false` 表示这次没有刷新索引，`refresh.last_refresh` 是索引最后刷新时间；汇报覆盖范围时用它说明索引截止到什么时候。
   - `sessions` 是匹配全集（`found` 时不受条数上限，最多 500 行并以 `capped` 说明），每行有 session 元数据、`hit_count` 和 `matched_indices`（记录序号，与 `show` 同一套编号；`--re` 或无检索词的查询只有 `hit_count`）。
   - 加过滤器收窄：`--project` / `--since` / `--harness` / `--role`。

5. **批量浏览，再挑重点细读**：
   ```bash
   mss show <id-prefix> --harness <name> --no-refresh --brief --around <index> --limit 40
   ```
   - `--brief` 每条消息一行头部（index、role、time）+ 截断正文，一次读一整个窗口，不用自己写脚本格式化。
   - 只看某一方视角：加 `--role user` 或 `--role assistant`（可重复）。
   - 截断处会标明"用某条命令读整条"。需要**逐字引用**或看完整正文时，再用 `--json` 读同一窗口（`window.clipped: true` 表示窗口里有消息被索引截断，需要更后面的内容就加大 `--limit` 或改用 `--offset`）：
   ```bash
   mss show <id-prefix> --harness <name> --no-refresh --json --around <index> --limit 40
   ```
   - 候选太多时说明读了哪些、跳过了哪些、为什么跳过。`mss last` / `mss ctx` 不支持 `--no-refresh`，会先刷新索引，本流程不用它们。

6. **现状核对（只读，仅当用户在问"现在"）**：对相关仓库做只读查询回答"现在怎么样了"：
   ```bash
   git -C <repo> status --short; git -C <repo> log --oneline -5; git -C <repo> rev-list --count <a>..<b>
   ```
   要求：只运行只读命令，禁止修改、commit 或 push 任何东西；核对结果写进汇报单独的"当前状态（非历史记录）"一节。

7. **汇报**：
   - 按时间线写；每条结论附 harness、日期、session id 和原文引用。session id 写完整（或足以区分同前缀会话的前缀）——曾有两条不同会话都写成 `01a0f11c` 而被误当成重复行。
   - **引用必须逐字**：引号里的必须是原文逐字摘录，不得拼接、不得增删字，也不得把两处文本合成一句。需要概括时写"大意：……"，不加引号。
   - 历史原话和基于历史的推断分开写，不得混为一谈。
   - **原始 vs 转述**：同一线索在多处出现时，只有「用户消息里、日期最早」的那次算原始讨论；之后 agent 消息里的同句或改写是转述，标注「转述」并指明它指向哪次原始讨论，不得当成独立证据。最早一次本身就是 agent 说的就注明，不得伪装成用户原话。
   - **现状只来自现状核对**：汇报里凡是描述现在状态（worktree 是否存在、分支在哪、是否合并或上线）的句子，必须来自当次只读核对；历史会话里的说法只能写成"当时（<日期>）是……"，不得写成现状。
   - 单独的"当前状态（非历史记录）"一节：列出当次核对用的命令和结果，并说明本次没有修改任何文件。
   - 被后来推翻的决定标"已被 <session/日期> 推翻"。
   - 结尾写覆盖范围：搜了哪些词和项目、读了几个 session、`coverage` 里的缺口、索引最后刷新时间（`refresh.last_refresh`）、当前会话是否已排除。

8. **Fallback（仅当 coverage 报告某数据源未读时）**：只有 `coverage.unread` 指明某 harness 的数据源这次没读到，才允许对**该数据源的原始目录**做只读 `rg`（`rg -l --hidden --no-ignore`），并在汇报中说明。其它情况一律以 mss 为准；`match: "none"` 就是没有找到，不 fallback。

- 若 `mss` 不在 PATH 或索引为空，直接说历史搜索不可用，不要臆造结果。
