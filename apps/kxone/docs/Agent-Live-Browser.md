# 真实云模型浏览器验收

`web/e2e/agent-live.spec.js` 使用已经启动的隔离 Go 服务和真实模型，不拦截 API 或替换回答。入口配置必须显式启用，普通 CI 与现有浏览器回归不会调用它。先完成前端构建，并按真实 API 验收工具说明启动隔离服务；服务需提供 `web/dist`、`/api/user/info` 和完整 Agent API。

服务生成的 `fixture-access.json` 必须是 `0600` 文件，包含 `base_url`、`token`、`second_token`、`account`、`second_account`、`model`、`fixture_hash`；两个 token 只属于隔离账号，文件不能放 API Key。`fixture_hash` 必须为 `Agent-Evaluation-Fixture.json` 原文件 SHA-256。浏览器仅接受 `127.0.0.1` 或 `::1` 地址，可通过 SSH 本地转发连接服务器上的隔离服务。禁止使用生产 CRM 地址。

在 `apps/kxone/web` 执行，以下路径替换为本轮隔离服务的实际路径：

```sh
AGENT_LIVE_E2E=1 \
AGENT_LIVE_FIXTURE_FILE=/absolute/private-evidence/fixture-access.json \
AGENT_LIVE_OUTPUT_DIR=/absolute/private-evidence/browser \
npx playwright test --config playwright.agent-live.config.js
```

如通过不同端口的本地转发访问，可另设 `AGENT_LIVE_BASE_URL=http://127.0.0.1:18089`。默认使用 Chrome；`AGENT_LIVE_BROWSER_CHANNEL` 可显式选择已安装的其他 Chromium 通道。导出的 Excel 由 Python 3 标准库 ZIP/XML 独立读取，`AGENT_LIVE_PYTHON` 可指定解释器路径。

该用例创建两轮完成分析和一轮显式停止的分析，自动重试为零；停止前等待首个模型用量事件，因此停止用例也可能产生费用。先在评测 A02 的来源问题后明确要求同结果柱状图及数据表，再追问好学校按当前负责人分组的数量、净成交金额和跟进依据。原评测 A02 问题和预期未修改。柱状图必须实际存在、绑定最终选定来源结果，横纵轴逐行对应独立来源数量；报告也必须含由保存结果生成的图表。模型 Markdown 表格会被替换为查询依据入口提示，只有真实结果组件呈现数据表。每轮必须明确声明最终采用结果，并实际展开回答的“数据与查询依据”，核对数据时点，按最终结果 ID 定位页面表格；仅探索查询中存在正确数据不能通过。来源及负责人结果与独立 fixture 逐行核对，页面单元格、原始查询及已保存的脱敏绑定参数与真实结果核对，最终采用的跟进记录也须实际展开核对。Excel 和 HTML 仅与最终采用的保存结果逐格核对，并断言没有额外过程结果数据页；独立 JSON 绑定参数也须一致（不拼接 SQL）。空值和空结果占位按各格式的真实呈现处理，不改变数值校验。

同时验证 320/375/768/1440 宽度，四种宽度复用同一回答，不新增模型请求。第一轮在收到真实 SSE 事件后中断浏览器自己的传输，保持服务端运行；核对重新订阅使用已接收事件游标且保持同一运行，然后在分析运行时刷新。故障注入只 abort 浏览器传输，不伪造事件、结果或模型回答。每轮还在接收后和完成后重放同一 request_id，核对返回同一 run ID；终态重放前后模型调用、预留与结算事件必须完全一致。浏览器界面的第一轮只发送一次新问题 POST；另验证来源下钻接口 200 和对应线索实际内容、返回及再次刷新不创建重复运行。第二隔离账号当前为主管，检查其历史列表已返回的内容，以及对会话、运行、SSE、结果和下载的 403 拒绝；该真实云用例不单独证明“其他管理员”的隔离，管理员间隔离另有 API 安全回归。定性顾虑归纳仍需人工对照实际读取记录审核，不能仅凭来源链接存在判为内容正确。

输出保存运行 JSON、模型名、问题、数据时点、实际结果 ID、耗时、双格式导出、文件哈希、四种宽度截图及账本用量快照。输出目录强制置于项目外并设为 `0700`，JSON 和导出文件为 `0600`；关闭可能包含登录凭证的 trace/video。浏览器账本差值只是隔离服务整体观测，有并行评测时按运行 ID 与服务端调用账本对账，不把差值冒充本用例费用。真实 API 关键问题重复三次和供应商请求脱敏审查由评测工具另外执行；本用例单次通过不能替代它们。

每次执行使用新的输出子目录，避免 Playwright 清理上一轮产物。该用例与同一隔离管理员的评测请求须串行执行，因为每账号只允许一项分析。最后一轮在首个模型用量事件后点击“停止生成”，核对 cancelled、无核验正文、刷新不重新调用；取消报告必须标记未完成且不导出未确认结果。停止不保证供应商撤销已产生费用，未知用量仍保留预留。服务确认接收后立即保存 `01-sources-accepted.json` / `02-good-school-accepted.json`；断流或断线后先按其中运行 ID 查询原任务，不能直接重跑整个用例再次付费。即使最终账本读取失败，也保留运行及检查证据并明确费用待核对。

隔离服务导入、初始化和登录完成后保存 `business-before.json`；关闭 HTTP 并取消未完成分析后保存 `business-after.json` 与 `business-integrity.json`。只读事务按真实主键排序，对固定 17 项分析业务表允许名单的结构及全部列值作规范化 SHA-256，输出仅包含各表行数、散列、存在与否和一致性，不保存客户原文或凭证值。AI 会话、结果、配置、费用等表不纳入业务指纹，其余未覆盖表逐一列在 `excluded_tables`，因此 `full_database_proof` 始终为 false。当前测试路由仅创建其中 16 张表，未创建的 `xiangwu_form_submissions` 明确列入 `missing_tables`，不冒充已验证的空表。缺少启动前指纹的旧进程只能保存当前观测，不能补造 before 或宣称前后一致；异常退出未取得 after 时也不能认定不可变验证通过。

本脚本的运行恢复检查也由普通 `agent.spec.js` 对可控模型执行，代码共用 `agent-run-checks.js`。可控模型通过只说明协议和浏览器检查可运行，不代表真实云验收通过。二十条以上历史的条件纠正使用显式选择的 A44 多轮场景；服务进程重启使用下述独立夹具，不能用当前两轮加停止替代。

## 独立进程崩溃与同库恢复

入口为 `cmd/server/agent_restart_acceptance_test.go` 中的 `TestAgentRestartAcceptance`。普通 `TestAgentLiveAcceptance` 每次新建临时业务库，重新运行它不能证明同库恢复。新入口不提供网页及第二账号，不能把它的 `fixture-access.json` 交给上面的浏览器脚本。

`TestAgentControlledProcessCrashRestart` 已覆盖本地可控模型的真实子进程 SIGKILL：一笔已结算调用、一笔用量未知的流式调用、保存的查询结果、临时快照，随后同库恢复两次；另以明确标记的隔离队列探针覆盖 queued 状态。它不调用云模型，也不证明真实供应商恢复验收已经通过。

真实模式仅从指定业务库的受限只读连接读取管理员保存的主模型配置及必要账号状态；另一条授权回调受限的连接仅允许读取/新增/结算 `ai_budget_reservations`。业务查询始终使用新建的 gold SQLite，不能指定现有业务库为恢复库。当前入口不载入普通 live 夹具的额外备用 profile，也不做主模型故障注入；真实备用验收须另行执行。

### 初始化前的固定条件

- 在隔离服务所在的 Linux 主机上运行以下流程，不使用生产 systemd 单元；本地验证可以用对应的可控模型测试。
- 冻结候选后编译一次测试二进制，init/resume 必须使用同一文件。夹具校验实际二进制 SHA-256，不仅核对人为填写的代码版本。
- 只使用全新 `/tmp/crm-agent-restart-*` 目录；不要预先创建该目录。入口自行以 `0700` 创建，已有目录不能再次 init。模型配置、价格、月限额、管理员、fixture、规则或二进制变化后必须另开新验收，不能编辑 `identity.json` 绕过检查。
- 夹具自己的加密密钥随机生成并保存为目录内 `field-key`（`0600`），同库重启必须保留。它不是生产加密密钥。SQLite、身份文件、访问文件均为私有文件；禁止打印访问 token、环境或密钥文件。
- 暂停同一验收账号的其他分析，保留每轮 8 次模型、12 次工具、180 秒、3 元与共同月预算。启动入口本身不调用模型；只有后面的显式创建运行会产生真实费用。失败后不能自动重跑。

在固定候选的 `apps/kxone` 目录执行，先把 `restart_go` 和证据父目录替换为本机的实际路径。不要开启 shell tracing。

```sh
umask 077
restart_go=/absolute/path/to/go1.25.13/bin/go
restart_stamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
restart_evidence="/absolute/private-evidence/restart-$restart_stamp"
restart_dir="/tmp/crm-agent-restart-$restart_stamp"
install -d -m 0700 "$restart_evidence"
test ! -e "$restart_dir"
test -z "$(git status --porcelain)"
test "$("$restart_go" version | awk '{print $3}')" = go1.25.13
git rev-parse HEAD > "$restart_evidence/code-revision.txt"
"$restart_go" test -c -o "$restart_evidence/server.test" ./cmd/server
sha256sum "$restart_evidence/server.test" > "$restart_evidence/test-binary.sha256"
restart_binary="$restart_evidence/server.test"

# 仅在已授权的服务器会话中加载既有环境；不要 echo、env 或 set -x。
set -a
. /etc/kxone/kxone.env
set +a
export KXONE_AGENT_LIVE_SOURCE_DB="${DB_NAME:?需要既有绝对数据库路径}"
export KXONE_AGENT_LIVE_FIXTURE="$PWD/docs/Agent-Evaluation-Fixture.json"
export KXONE_AGENT_RESTART_ACCEPTANCE=1
export KXONE_AGENT_RESTART_MODE=live
export KXONE_AGENT_RESTART_PHASE=init
export KXONE_AGENT_RESTART_DIR="$restart_dir"
export KXONE_AGENT_RESTART_LISTEN=127.0.0.1:0
export RESTART_EVIDENCE_DIR="$restart_evidence"
export RESTART_TEST_BINARY="$restart_binary"

nohup "$restart_binary" -test.run='^TestAgentRestartAcceptance$' -test.timeout=2h10m \
  > "$restart_evidence/init.log" 2>&1 &
restart_pid=$!
printf '%s\n' "$restart_pid" > "$restart_evidence/init.pid"
```

环境文件必须已含 `FIELD_ENCRYPTION_KEY` 或兼容的 `PII_ENCRYPTION_KEY`。真实模型 API Key 由受限连接读取已保存的密文配置，不放命令参数。`KXONE_AGENT_LIVE_SOURCE_DB` 只作为配置/共同账本来源，不能等于夹具内的 `kxone-test.db`。当前规则版本为夹具独立的 0，不复制生产公共规则；报告必须如此标注。

等待 `fixture-access.json` 原子写入后，核对其中 PID 等于 `init.pid`，`phase=init`、`mode=live`、`database` 为上述目录内 `kxone-test.db`。目录内 `identity.json` 的 `ready=true` 表示初始化和 gold 导入完成。`real_model_acceptance_passed=false` 是入口固定的诚实标记，不能手动改为 true；通过判定应写入独立验收报告。所有请求在夹具主机上使用文件里的 loopback `base_url` 与 bearer token，不经过生产域名。

### 显式创建一轮与观察

下面示例只创建一轮真实分析。请求 ID 在模型请求前落盘；若网络中断，先按该会话、request ID 和运行 ID 查询，不能换一个 request ID 重新开始。初始化失败、尚未生成访问文件或 PID 不符时先检查私有日志，不启动提问。

```sh
python3 - <<'PY'
import json, os, pathlib, urllib.request, uuid
root = pathlib.Path(os.environ['KXONE_AGENT_RESTART_DIR'])
evidence = pathlib.Path(os.environ['RESTART_EVIDENCE_DIR'])
access = json.loads((root / 'fixture-access.json').read_text())
assert access['pid'] == int((evidence / 'init.pid').read_text())
assert access['mode'] == 'live' and access['phase'] == 'init'
assert pathlib.Path(access['database']).resolve() == (root / 'kxone-test.db').resolve()
request_path = evidence / 'request.json'
assert not request_path.exists(), '已有请求证据：先恢复原运行，不再次创建付费问题'
def write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f: json.dump(value, f, ensure_ascii=False, indent=2)
def request(method, path, body=None):
    raw = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(access['base_url'] + path, data=raw, method=method,
          headers={'Authorization': 'Bearer ' + access['token'], 'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=15) as response: return json.load(response)
question = {'message': '按来源统计全部线索数量和净成交金额，再查看跟进依据。',
            'request_id': str(uuid.uuid4())}
write(request_path, question)
conversation = request('POST', '/api/agent/conversations', {'title': '真实进程恢复隔离验收'})
write(evidence / 'conversation.json', conversation)
run = request('POST', '/api/agent/conversations/' + conversation['id'] + '/runs', question)
write(evidence / 'accepted-run.json', run)
PY
```

只读观察接口为 `GET /__restart_observation`，须带同一 bearer。返回值仅覆盖该隔离业务库中的运行 ID，包括 `runs`、这些运行对应的共同账本 `reservations`、临时快照数、保存结果散列、done/delta 数和业务允许名单指纹。其他评测可能改变月总额；比较本用例的 reservation ID/内容，不比较全账户余额必须不变。

应保存 `before-crash.json`，至少核对选中 run 仍为 running、已有一笔 settled 及一笔 reserved、临时快照仍存在，尽量等待一个真实查询结果已保存。`outbound-audit.jsonl` 只记录 PID、时间、字节数及请求散列，其阶段是 **before_transmission**：它不能单独证明供应商已收到请求、完成调用或计费。已有结算证明前面的调用完成；当前在途调用是否计费仍可能未知。

### 只终止经过身份核对的专用进程

禁止 `pkill`、`killall` 或对 `kxone-server`/`kxone-worker` 执行停止。Linux 上使用 pidfd 固定进程身份，不能仅凭旧 PID 杀进程。下面命令同时核对实际二进制、测试入口、私有目录及 init 身份，保存即时观察，再 SIGKILL；它不调用模型。

```sh
python3 - <<'PY'
import hashlib, json, os, pathlib, signal, urllib.request
root = pathlib.Path(os.environ['KXONE_AGENT_RESTART_DIR']).resolve()
evidence = pathlib.Path(os.environ['RESTART_EVIDENCE_DIR'])
binary = pathlib.Path(os.environ['RESTART_TEST_BINARY']).resolve()
access = json.loads((root / 'fixture-access.json').read_text())
identity = json.loads((root / 'identity.json').read_text())
run_id = json.loads((evidence / 'accepted-run.json').read_text())['id']
pid = access['pid']
assert pid == int((evidence / 'init.pid').read_text()) and pid > 1
assert access['phase'] == 'init' and access['mode'] == 'live'
assert root.stat().st_mode & 0o777 == 0o700
assert pathlib.Path(access['database']).resolve() == root / 'kxone-test.db'
assert hashlib.sha256(binary.read_bytes()).hexdigest() == identity['test_binary_sha256']
assert hasattr(os, 'pidfd_open') and hasattr(signal, 'pidfd_send_signal'), '需要支持 pidfd 的 Linux/Python，不能退化为裸 PID 批量结束'
fd = os.pidfd_open(pid)
try:
    proc = pathlib.Path('/proc') / str(pid)
    assert (proc / 'exe').resolve() == binary
    args = (proc / 'cmdline').read_bytes().split(b'\0')
    assert b'-test.run=^TestAgentRestartAcceptance$' in args
    env = dict(v.split(b'=', 1) for v in (proc / 'environ').read_bytes().split(b'\0') if b'=' in v)
    assert env[b'KXONE_AGENT_RESTART_MODE'] == b'live'
    assert env[b'KXONE_AGENT_RESTART_PHASE'] == b'init'
    assert pathlib.Path(os.fsdecode(env[b'KXONE_AGENT_RESTART_DIR'])).resolve() == root
    req = urllib.request.Request(access['base_url'] + '/__restart_observation',
          headers={'Authorization': 'Bearer ' + access['token']})
    with urllib.request.urlopen(req, timeout=15) as response: before = json.load(response)
    assert any(r['id'] == run_id and r['status'] == 'running' for r in before['runs'])
    reservations = [r for r in before['reservations'] if r['run_id'] == run_id]
    assert any(r['status'] == 'settled' for r in reservations)
    assert any(r['status'] == 'reserved' for r in reservations)
    assert before['temporary_snapshots'] > 0
    out = os.open(evidence / 'before-crash.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(out, 'w') as f: json.dump(before, f, ensure_ascii=False, indent=2)
    signal.pidfd_send_signal(fd, signal.SIGKILL)
finally:
    os.close(fd)
PY
wait "$restart_pid" || true
```

若运行在 SIGKILL 前已经完成，不能把这次记录判为“在途崩溃恢复通过”。保留该轮事实、费用与原因，下一轮需要显式安排新的真实运行，不自动补测。若 Python 不支持 pidfd、身份检查失败或进程已退出，立即停在检查点；不要改成模糊匹配进程名。

### 同库 resume 与判定

保留全部夹具文件和原测试二进制，只修改 phase；不要重新编译、重新导入 gold、删除 WAL 或恢复整库。

```sh
export KXONE_AGENT_RESTART_PHASE=resume
nohup "$restart_binary" -test.run='^TestAgentRestartAcceptance$' -test.timeout=2h10m \
  > "$restart_evidence/resume.log" 2>&1 &
restart_pid=$!
printf '%s\n' "$restart_pid" > "$restart_evidence/resume.pid"
```

访问文件可能仍是旧进程留下的文件；必须等其 `pid` 等于 `resume.pid` 且 `phase=resume` 再请求。resume 会先校验二进制/fixture/配置/账号/规则身份，以及固定 17 项业务表允许名单的指纹；不符时拒绝打开写连接。该指纹不证明所有数据库表都未变化，未包含表的边界与前文一致。

下面只读采集恢复后状态，不会创建或重复模型请求，也不自动把观察改写为验收通过：

```sh
python3 - <<'PY'
import json, os, pathlib, time, urllib.request
root = pathlib.Path(os.environ['KXONE_AGENT_RESTART_DIR'])
evidence = pathlib.Path(os.environ['RESTART_EVIDENCE_DIR'])
expected_pid = int((evidence / 'resume.pid').read_text())
run_id = json.loads((evidence / 'accepted-run.json').read_text())['id']
for _ in range(100):
    access = json.loads((root / 'fixture-access.json').read_text())
    if access['pid'] == expected_pid and access['phase'] == 'resume': break
    time.sleep(0.2)
else:
    raise RuntimeError('新 resume 进程尚未就绪；检查私有日志，不重建夹具')
def capture(name, path):
    req = urllib.request.Request(access['base_url'] + path,
          headers={'Authorization': 'Bearer ' + access['token']})
    with urllib.request.urlopen(req, timeout=15) as response: value = json.load(response)
    fd = os.open(evidence / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f: json.dump(value, f, ensure_ascii=False, indent=2)
capture('after-restart.json', '/__restart_observation')
capture('recovered-run.json', '/api/agent/runs/' + run_id)
time.sleep(3)
capture('after-idle.json', '/__restart_observation')
PY
```

只有同时满足以下条件才可判定本轮恢复通过：

1. 原 run 和消息为 interrupted，原 run ID、消息和已保存结果仍在；`result_selection.status=unfinished`，不存在正式核验正文/图表或私有推理泄露。
2. 同一 reservation ID 的字段逐项相同，尤其 unknown 的 reserved 数额仍占用预算；不能归零、重复增加或把未证实的 usage 填为零。若 SIGKILL 前发生了合法结算，要据时间和实际用量解释，不能伪造前后完全不变。
3. 旧查询结果散列相同，快照数为 0，done 事件每个恢复运行只有一次，未经核验的 delta 事件为 0；源业务允许名单指纹相同。
4. 不发送新问题期间，`outbound-audit.jsonl` 没有新请求行，观察接口无新增 reservation。只读 GET、SSE 订阅或刷新不得重启模型。
5. 若再验证第二次恢复，先按同样 pidfd/二进制/phase 规则核对专用 resume 进程后结束，重新 resume 同库；done、保存结果和账本仍不重复。不能复用上面只接受 init 的 SIGKILL 段跳过身份检查。

最终单独写入代码/二进制/fixture/模型/规则版本、运行 ID、被终止 PID、前后观察、账本对账、审计行数、时间及结论。不要把 `real_model_acceptance_passed` 入口标记改写为通过。真实云结果与本地可控测试分别记录；该操作不替代真实备用、A44 长会话、完整题库或浏览器验收。

## 当前发布记录的补录要求

截至 2026-10-13，发布状态以项目根目录 `docs/CRM-Agent-验收与发布记录-20261013.md` 及本轮私有证据为准。测试提交 `ebdebc3` 增加了 A31 持久化导出、A44 长会话与进程恢复测试，但不是新的真实模型通过证明。最新协调记录仍报告 v24 A13 第二次回答数字未绑定，以及真实主模型浏览器第二题在隐私拒绝后耗尽调用次数、供应商返回 `context_exceeded_error`；不能把充值、单题完成或本地测试写成这些阻断已经解决。

正式发布前必须补齐：

- 最终冻结候选的版本/源码与二进制散列、对应真实评测目录；把 v24 失败和后续修复复测单列，不能沿用 v23 成功标记推定新候选通过。
- A08/A13/A21 各三次、去重语义变体、完整 A01–A30 与关键重复、大数据和语义缺失时间场景；每轮分别判定运行结果及全部自然语言结论。
- 主模型真实浏览器和真实备用调用证据；备用测试应保存真实 `provider_switch`/用量事件，并明确主服务失败是否为隔离故障注入。
- A44 超过二十条真实历史及本节真实进程恢复报告。A31 当前数据库变化后的历史导出一致性有确定性集成证据，不冒充云调用已经覆盖。
- 最终候选的整体 Go、前端、资源预算和 CRM 回归，以及无 profile/trace 的两核同负载性能；既往版本、剖析诊断和本机结果不能替代。
- 按 run/reservation ID 汇总已结算与未知预留、模型/单价版本和费用时间点；旧账本汇总不是今日费用，也不是最新供应商账单。
- 发布前在线备份、迁移顺序/校验和与演练、实际安全回滚二进制和运行演练；发布后实际 MainPID/版本、静态资源、服务、数据库、迁移、SSE 代理与心跳、各业务开关证据。发布脚本存在不等于实际已经演练或上线。

仓库 `deploy/nginx/kxone.conf` 已有 Agent events 专用路由（`proxy_read_timeout 210s`、关闭缓冲/缓存/gzip），但 `scripts/deploy_kxone.sh` / `scripts/deploy_remote.sh` 不负责安装或 reload Nginx。正式发布必须另外记录当前配置备份、候选配置差异、`nginx -t`、受控 reload 及实际生效核验，不能根据仓库里存在配置就宣称线上 SSE 已验证。

在这些门槛通过前保持线上 AI 关闭及旧统计入口。发布记录应清楚区分“可控模型通过”“真实运行完成”“语义通过”“已发布”，并保留失败、未知费用与未验证项。
