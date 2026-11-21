#!/usr/bin/env python3
"""Replay everyday questions on an isolated CRM serving a server-local copy of
the production database and reconcile every answer with read-only SQL on the
copy. Prints only counts, metric ids, dates and pass/fail (no customer text)."""
import argparse, datetime, hashlib, json, re, sqlite3, sys, time, urllib.request, uuid
from collections import Counter
from pathlib import Path

SH = datetime.timezone(datetime.timedelta(hours=8))

def parse_ts(v):
    if not v:
        return None
    s = str(v).strip().replace('T', ' ')
    m = re.match(r'^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})(\.\d+)?\s*(Z|[+-]\d{2}:?\d{2})?', s)
    if not m:
        return None
    base = datetime.datetime.strptime(m.group(1), '%Y-%m-%d %H:%M:%S')
    frac = (m.group(2) or '.0')[1:7].ljust(6, '0')
    base = base.replace(microsecond=int(frac))
    tz = m.group(3)
    if not tz or tz == 'Z':
        base = base.replace(tzinfo=datetime.timezone.utc)
    else:
        sign = 1 if tz[0] == '+' else -1
        tz = tz[1:].replace(':', '')
        base = base.replace(tzinfo=datetime.timezone(sign * datetime.timedelta(hours=int(tz[:2]), minutes=int(tz[2:]))))
    return base.astimezone(SH).date().isoformat()

def truth(db_path):
    c = sqlite3.connect(f'file:{db_path}?mode=ro', uri=True)
    leads = c.execute("SELECT created_at,converted_at,COALESCE(source_channel,''),status,COALESCE(ownership_state,''),COALESCE(current_owner_user_id,'') FROM leads WHERE deleted_at IS NULL OR deleted_at=''").fetchall()
    names = {}
    for wid, name in c.execute("SELECT wecom_userid,employee_name FROM crm_wecom_user_bindings ORDER BY id"):
        names.setdefault(wid, name)
    c.close()
    rows = []
    for created, converted, source, status, own, owner in leads:
        if status in ('assigned_pending_claim',) or own == 'assigned_pending_claim':
            label = '已分配待领取'
        elif own == 'public_pool' or status == 'public_pool':
            label = '公海'
        elif own == 'pending_assignment' or status == 'pending_dispatch':
            label = '待主管分配'
        elif owner == '':
            label = '无负责人'
        else:
            label = names.get(owner) or owner
        rows.append({'d': parse_ts(created), 'cd': parse_ts(converted), 'src': source or '未填写', 'status': status, 'owner': label})
    return rows

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--fixture-access', type=Path, required=True)
    ap.add_argument('--copy-db', required=True)
    ap.add_argument('--output', type=Path, required=True)
    ap.add_argument('--rounds', type=int, default=2)
    ap.add_argument('--max-spend-yuan', type=float, required=True)
    ap.add_argument('--allow-paid-requests', action='store_true')
    args = ap.parse_args()
    if not args.allow_paid_requests or not 0 < args.max_spend_yuan <= 20:
        ap.error('Explicit paid opt-in and a ceiling within 20 yuan are required')
    if args.fixture_access.stat().st_mode & 0o077: ap.error('Fixture access must be private (0600)')
    access = json.loads(args.fixture_access.read_text())
    base = access['base_url'].rstrip('/')
    assert base.startswith('http://127.0.0.1:'), 'loopback only'
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def req(path, method='GET', body=None, raw=False):
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
        r = urllib.request.Request(base + path, data=data, method=method, headers={'Authorization': 'Bearer ' + access['token'], 'Content-Type': 'application/json'})
        with opener.open(r, timeout=30) as resp:
            content = resp.read()
            return content.decode() if raw else (json.loads(content) if content else None)
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    from agent_live_evaluate import campaign_lock
    with campaign_lock(args.output):
        from agent_evaluation_campaign import freeze_campaign, recorded_metric_spend, save_record, collect_paid_turn
        rows = truth(args.copy_db)
        access['candidate_identity']['oracle_rows_sha256'] = hashlib.sha256(json.dumps(rows,sort_keys=True).encode()).hexdigest()
        candidate_id = freeze_campaign(args.output, access, 'prodcopy-v1', [Path(__file__), Path(__file__).with_name('agent_evaluation_campaign.py'), Path(__file__).with_name('agent_live_evaluate.py')])
        today = datetime.datetime.now(SH).date()
        monday = today - datetime.timedelta(days=today.weekday())
        D = lambda d: d.isoformat()
        P = {'today': (D(today), D(today)), 'yesterday': (D(today - datetime.timedelta(days=1)),) * 2,
             'last_week': (D(monday - datetime.timedelta(days=7)), D(monday - datetime.timedelta(days=1))),
             'month': (D(today.replace(day=1)), D(today)), 'recent7': (D(today - datetime.timedelta(days=6)), D(today))}
        inr = lambda d, p: d is not None and p[0] <= d <= p[1]
        def lead_count(p): return sum(1 for r in rows if inr(r['d'], p))
        def by(key, p): return dict(Counter(r[key] for r in rows if inr(r['d'], p)))
        def deals(p): return sum(1 for r in rows if r['status'] == 'converted' and inr(r['cd'], p))
        convs = [
            [('今天有几条线索进来？', 'lead_count', 'today')],
            [('昨天进了几条线索？', 'lead_count', 'yesterday')],
            [('上周共有几条线索进来？', 'lead_count', 'last_week'), ('不是，我说的是今天，单单是今天', 'lead_count', 'today')],
            [('本月新增了多少条线索？', 'lead_count', 'month')],
            [('最近7天进了多少条线索？', 'lead_count', 'recent7')],
            [('本月哪个来源最多？', 'source_performance', 'month'), ('按负责人拆开', 'owner_current', 'month')],
            [('这个月成交了几单？', 'deal_count', 'month')],
        ]
        out = args.output; out.mkdir(parents=True, exist_ok=True, mode=0o700)
        table = []; failures = 0
        for rnd in range(1, args.rounds + 1):
            for conv_index, conv in enumerate(convs, 1):
                path = out / f'R{rnd:02}-{conv_index}.json'
                record = json.loads(path.read_text()) if path.exists() else {'candidate_id':candidate_id, 'turns': [], 'micro': 0}
                if record.get('failure'): raise RuntimeError('Recorded failure; review before spending more')
                if not record.get('conversation_id'):
                    record['conversation_id'] = req('/api/agent/conversations', 'POST', {'title': f'生产副本对数 r{rnd}'})['id']
                    save_record(path, record)
                for index, (q, metric, period) in enumerate(conv):
                    if index == len(record['turns']):
                        record['turns'].append({'question':q,'request_id':'prodcopy-'+str(uuid.uuid4())})
                        save_record(path, record)
                    turn = record['turns'][index]
                    if turn['question'] != q: raise RuntimeError('Question changed; reconcile evidence')
                    collect_paid_turn(out, path, record, turn, req, round(args.max_spend_yuan * 1e6))
                    rid, micro = turn['run_id'], turn['micro']
                    sel = turn['selected']
                    content = turn['content']; first = content.split('\n')[0]
                    p = P[period]; problems = []
                    chosen = [s for s in sel if s['metric_id'] == metric]
                    if not chosen:
                        problems.append(f'metric {[s["metric_id"] for s in sel]} != {metric}')
                    else:
                        prm = chosen[0].get('params') or {}
                        if (prm.get('date_from'), prm.get('date_to')) != p:
                            problems.append(f'params {prm.get("date_from")}..{prm.get("date_to")} != {p[0]}..{p[1]}')
                    res = [r for r in turn.get('results', []) if r.get('query_params') is not None]
                    res = [r for r in res if any(k in r['columns'] for k in ('新增线索数', '来源', '当前归属', '成交数'))]
                    agent_value = None
                    expect = None
                    if not res:
                        problems.append('no result')
                    else:
                        r = res[-1]
                        if r.get('query_params')[:2] != list(p):
                            problems.append(f'executed params {r.get("query_params")} != {list(p)}')
                        cols = r['columns']
                        if metric == 'lead_count':
                            agent_value = r['rows'][0][cols.index('新增线索数')]; expect = lead_count(p)
                            if not (agent_value == expect and ((f'{expect} 条' in first) if expect else ('没有' in first))):
                                problems.append(f'count agent={agent_value} sql={expect}')
                        elif metric == 'deal_count':
                            agent_value = r['rows'][0][cols.index('成交数')]; expect = deals(p)
                            if not (agent_value == expect and ((f'{expect} 单' in first) if expect else ('没有成交' in first))):
                                problems.append(f'deals agent={agent_value} sql={expect}')
                        else:
                            key = '来源' if metric == 'source_performance' else '当前归属'
                            agent_value = {row[cols.index(key)]: row[cols.index('线索数')] for row in r['rows']}
                            expect = by('src' if metric == 'source_performance' else 'owner', p)
                            if agent_value != expect:
                                problems.append('breakdown mismatch')
                            if metric == 'source_performance' and expect:
                                top = max(expect.values())
                                if f'共 {top} 条' not in first:
                                    problems.append('top source count not in answer')
                    if turn['status'] != 'completed':
                        problems.append('status ' + turn['status'])
                    ok = not problems; failures += 0 if ok else 1
                    summary = (sum(agent_value.values()) if isinstance(agent_value, dict) else agent_value)
                    exp_summary = sum(expect.values()) if isinstance(expect, dict) else expect
                    table.append({'round': rnd, 'question': q, 'metric': metric, 'period': f'{p[0]}..{p[1]}', 'agent_value': summary, 'sql_value': exp_summary, 'groups': len(expect) if isinstance(expect, dict) else None, 'first_line': first, 'ok': ok, 'problems': problems, 'micro': micro, 'model': turn.get('model'), 'sources': sorted({s.get('source') for s in sel})})
                    turn['validation'] = table[-1]
                    record['failure'] = problems or None
                    save_record(path, record)
                    print(json.dumps(table[-1], ensure_ascii=False), flush=True)
                    if problems: raise RuntimeError('Production-copy reconciliation failed; evidence retained')
        spent = recorded_metric_spend(out)
        (out / 'prodcopy-table.json').write_text(json.dumps({'spent_micro': spent, 'failures': failures, 'rows': table}, ensure_ascii=False, indent=2))
        print(f'PRODCOPY failures={failures} runs={len(table)} spent={spent/1e6:.4f}元', flush=True)
        sys.exit(1 if failures else 0)

if __name__ == '__main__':
    # One collector owns a campaign; concurrent resumes cannot duplicate runs.
    from agent_live_evaluate import campaign_lock
    main()
