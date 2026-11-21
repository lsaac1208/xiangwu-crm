# 真实模型压力数据验收

这份入口只存在于 Go `_test.go`，不会进入生产二进制。它建立另一份空临时业务数据库，与普通 A01–A30 的七条线索评测数据分开；共享的生产连接仍仅允许读取已保存模型配置和管理员状态，以及登记预算账本。启动服务不会调用模型。

使用 `Agent-Stress-Fixture.json` 声明的 `agent-stress-fixture-v1`：10,001 条线索，奇数来源 A、成交 1 元，偶数来源 B、未成交，每条正好 5 条跟进。独立预期为 50,005 条跟进、净成交额 500,100 分。与既有性能测试的 50,001 条跟进数据不可混用。

启动前，Go 从真实导入行重新计算内容 SHA-256，并验证行数、金额、来源、业务状态、外键关系和每条线索五条跟进。`fixture-access.json` 同时记录原始声明文件 SHA-256、内容 SHA-256、数据集和版本。Python 评测器独立生成规范行指纹，三方一致才允许发起请求。指纹覆盖清单中明确声明的业务字段，不包含随机管理员映射、密钥或数据库默认值；不是生产数据库散列。

在现有受限 live acceptance 环境上额外设置：

```sh
KXONE_AGENT_LIVE_DATASET=stress-v1
```

仍用 `TestAgentLiveAcceptance` 启动，其他 `KXONE_AGENT_LIVE_*` 环境要求不变。使用新的私有 evidence 目录，并在开通新实例前停止前一实例。只有设置 `KXONE_AGENT_LIVE_ACCEPTANCE=1` 才启动真实入口。字段加密 key 必须从受限服务器环境继承，不能出现在命令参数或记录中。

明确发起付费验收：

```sh
python3 scripts/agent_live_evaluate.py \
  --fixture-access /private/stress/fixture-access.json \
  --output /private/stress/evaluation \
  --dataset stress-v1 --cases A35,A36 --repeat 3 \
  --max-spend-yuan 20 --allow-paid-requests
```

`stress-v1` 只允许 A35/A36；默认 `gold-v1` 仍只允许 A01–A30。单轮 3 元上限、持久化共享月预算、未知费用保留、同一请求恢复和活动目录锁均不变。fixture token 有效期一小时，必须分批执行，不应以重启另一个隔离库的方式恢复已有运行 ID。

A35 的金额必须按线索计一次，直接将线索和五条跟进 JOIN 后求和得到的倍增值是失败。A36 单独保存实际结果接口 `limit=20&offset=0/20` 的两个页面，核对匹配总数 10,001、每页 20、第一页截断为真、下一页不重复；没有匹配结果就保留空分页证据，不能冒充通过。运行详情默认 200 行预览不是“前 20 行”验收依据。

完成运行只代表证据采集结束。数字、受控渲染、结论、来源和隐私仍需人工独立复核。此入口没有并发业务写入压测，不替代业务 P95 或实际浏览器验收。

不付费的本地核验：

```sh
GOTOOLCHAIN=go1.25.13 go test ./cmd/server -run '^TestAgentLive' -count=1
python3 scripts/test_agent_live_evaluate.py
```

未启用显式环境时，真实模型入口跳过；压力导入、内容指纹、全量脱敏快照聚合与分页测试仍正常执行。
