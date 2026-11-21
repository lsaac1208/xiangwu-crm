# 成交日期与当前状态：独立语义反例

这两份数据只用于隔离验收，不改变原始 gold，也不进入生产业务库。入口位于 `_test.go`，启动不会调用模型。原始 gold 的 SHA-256 固定为 `bc3c9cc69c0bda41f2e5589fcc37611f1ffe996d1ef47ddabc309fdc82936c92`。

| 独立 dataset | 唯一业务改动 | 按当前 status 成交 | 日期非空 | status 成交或日期非空 |
|---|---|---:|---:|---:|
| `semantic-active-timestamp-v1` | 线索 2 仍为 active/following，增加历史 `converted_at=2026-09-02T12:00:00+08:00` | 2 | 3 | 3 |
| `semantic-missing-timestamp-v1` | 线索 3 仍为 converted，设 `converted_at=null` | 2 | 1 | 2 |

两份数据的线索总数均为 7，当前成交率均为 `2/7 = 28.571428…%`。好学校与员工丁抖音（员工乙）仍各有 2 条线索、1 条当前成交，成交率均为 50%；净额仍为 8,000 元与 5,000 元。历史日期可以残留，历史时间也可能缺失，但它们都不能替代当前业务状态。

当前业务状态 `status` 与非空 `result_status` 若存在冲突，应披露冲突及统计限制，不得通过盲目 OR 把矛盾掩盖。这两份反例的两个状态字段保持一致，只用于辨别日期权威错误；不扩展为状态冲突数据集。

加载器先验证原 gold 字节散列，然后逐字段对照整个 JSON，证明除声明的那个 `converted_at` 外，所有业务数据完全一致。修改其他客户、金额、来源、跟进或独立预期会被拒绝。元数据明确记录 dataset、独立版本、独立文件 SHA-256、原 gold SHA-256 和语义预期。独立文件包含完整合成记录，其文件散列即版本化数据声明身份，不与普通 gold 或 stress 的散列混用。

在原 live acceptance 所需受限环境基础上，分别选择：

```sh
KXONE_AGENT_LIVE_DATASET=semantic-active-timestamp-v1
```

或：

```sh
KXONE_AGENT_LIVE_DATASET=semantic-missing-timestamp-v1
```

仍通过 `TestAgentLiveAcceptance` 启动，每个实例使用不同 loopback 端口与私有 evidence 目录；字段加密 key 仅从既有受限环境继承。显式 `KXONE_AGENT_LIVE_ACCEPTANCE=1`、受限生产配置读取、真实管理员 ID、共享费用账本和出站配置版本守卫与原入口完全相同。评测 runner 使用显式 `--dataset` 选择对应变体，并独立核对文件散列、原 gold 散列、版本和唯一字段差异；不得把其元数据伪装成 gold 绕过散列检查。默认只运行 A03、A07，可使用 `--repeat 3`，原问题和预期不改写。

如由主验收流程明确安排两个实例并行，每个实例只允许已核实主管理员进行一个付费运行，第二账号仍只用于权限检查。两个实例的调用均计入同一管理员和同一原月预算账本。单个服务的并发限制不会自动协调其他 fixture 实例；外部调度必须保证总共最多两项运行。没有显式问题请求就没有模型调用。

建议先执行不付费验证：

```sh
GOTOOLCHAIN=go1.25.13 go test ./cmd/server -run '^TestAgentLiveSemantic' -count=1
```

它会分别导入真实业务 schema，通过独立 SQL 核对行数、三个不同判定条件和状态字段无冲突，再核对同一脱敏快照中的结果。另有拒绝测试，覆盖原 gold hash 变化、其他金额被修改、声明日期不符以及修改独立预期。

真实验收沿用 A03 的当前整体成交率问题，以及 A07 的来源比较问题即可，无需向模型透露预期答案或增加固定问答模板。审查时必须同时检查查询条件、最终口径、实际结果和回答；只有答案数字正确仍不足以通过。原 gold 及先前失败证据应完整保留。
