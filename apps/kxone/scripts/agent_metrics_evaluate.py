#!/usr/bin/env python3
"""Fixed-metrics live acceptance (phase two) against an isolated loopback CRM.

Runs docs/Agent-Metrics-Cases.json, checks server tables against independent
expectations and that published interpretation contains no numbers. Stops at
the first failure and never re-posts a recorded request.
"""
import argparse, datetime, json, re, time, urllib.parse, urllib.request, uuid
from pathlib import Path

NUMBER = re.compile(r'[0-9０-９%％]')

from agent_evaluation_campaign import freeze_campaign, recorded_metric_spend, save_record, collect_paid_turn
from agent_live_evaluate import campaign_lock


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture-access', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cases')
    parser.add_argument('--dataset', default='gold-v1', help='only cases of this dataset (gold-v1 or daily-v1)')
    parser.add_argument('--max-spend-yuan', type=float, required=True)
    parser.add_argument('--allow-paid-requests', action='store_true')
    args = parser.parse_args()
    if not args.allow_paid_requests or not 0 < args.max_spend_yuan <= 20:
        parser.error('Explicit paid opt-in and a ceiling within 20 yuan are required')
    if args.fixture_access.stat().st_mode & 0o077:
        parser.error('Fixture access file must be private (0600)')
    access = json.loads(args.fixture_access.read_text())
    base = access['base_url'].rstrip('/')
    url = urllib.parse.urlparse(base)
    if url.scheme != 'http' or url.hostname not in ('127.0.0.1', '::1'):
        parser.error('Only an isolated loopback CRM is allowed')
    docs = Path(__file__).resolve().parents[1] / 'docs'
    manifest = json.loads((docs / 'Agent-Metrics-Cases.json').read_text())
    fixture = json.loads((docs / 'Agent-Evaluation-Fixture.json').read_text())
    forbidden = list(fixture['independent_expected']['forbidden_provider_payload_fragments'])
    if args.dataset == 'daily-v1':
        forbidden += json.loads((docs / 'Agent-Daily-Fixture.json').read_text())['forbidden_provider_payload_fragments']
    shanghai = datetime.timezone(datetime.timedelta(hours=8))
    today = datetime.datetime.now(shanghai).date()
    monday = today - datetime.timedelta(days=today.weekday())
    dates = {'today': today, 'yesterday': today - datetime.timedelta(days=1), 'last_monday': monday - datetime.timedelta(days=7),
             'last_sunday': monday - datetime.timedelta(days=1), 'month_first': today.replace(day=1)}
    dates = {k: v.isoformat() for k, v in dates.items()}
    wanted = args.cases.split(',') if args.cases else [c['id'] for c in manifest['cases']]
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    with campaign_lock(output):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def request(path, method='GET', body=None, raw=False):
            data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
            req = urllib.request.Request(base + path, data=data, method=method, headers={'Authorization': 'Bearer ' + access['token'], 'Content-Type': 'application/json'})
            try:
                with opener.open(req, timeout=25) as response:
                    content = response.read()
                    return content.decode() if raw else (json.loads(content) if content else None)
            except urllib.error.HTTPError as error:
                raise RuntimeError(f'CRM HTTP {error.code}: {error.read(2048).decode(errors="replace")}') from None

        candidate_id = freeze_campaign(output, access, args.dataset, [docs / 'Agent-Metrics-Cases.json', docs / 'Agent-Evaluation-Fixture.json', docs / 'Agent-Daily-Fixture.json', Path(__file__), Path(__file__).with_name('agent_evaluation_campaign.py'), Path(__file__).with_name('agent_live_evaluate.py')])
        spent = recorded_metric_spend(output)
        for case in manifest['cases']:
            if case['id'] not in wanted or case.get('dataset', 'gold-v1') != args.dataset:
                continue
            for repeat in range(1, case['repeat'] + 1):
                path = output / f"{case['id']}-{repeat}.json"
                if path.exists():
                    record = json.loads(path.read_text())
                    if record.get('passed'):
                        print(f"{case['id']}-{repeat}: recorded pass", flush=True)
                        continue
                    if record.get('failure'):
                        raise RuntimeError(f'{path.name} has a recorded failure; review before spending more')
                else:
                    record = {'candidate_id': candidate_id, 'case': case['id'], 'repeat': repeat, 'turns': [], 'micro': 0, 'passed': False}
                    save_record(path, record)
                if not record.get('conversation_id'):
                    record['conversation_id'] = request('/api/agent/conversations', 'POST', {'title': f"固定指标验收 {case['id']}-{repeat}"})['id']
                    save_record(path, record)
                failure = None
                for index, question in enumerate(case['turns']):
                    if index == len(record['turns']):
                        record['turns'].append({'question': question, 'request_id': 'metric-eval-' + str(uuid.uuid4())})
                        save_record(path, record)
                    turn = record['turns'][index]
                    if turn['question'] != question: raise RuntimeError('Question changed; reconcile evidence')
                    collect_paid_turn(output, path, record, turn, request, round(args.max_spend_yuan * 1_000_000))
                    selected, content, micro = turn['selected'], turn['content'], turn['micro']
                    raw_expect = json.dumps(case['expect'][index], ensure_ascii=False)
                    for name, value in dates.items():
                        raw_expect = raw_expect.replace('{' + name + '}', value)
                    expect = json.loads(raw_expect)
                    failure = check_turn(expect, turn, forbidden)
                    save_record(path, record)
                    print(f"{case['id']}-{repeat} turn {index + 1}: {turn['status']} {turn['seconds']}s {micro / 1e6:.4f}元 metrics={[(s['metric_id'], s.get('source')) for s in selected]} model={turn['model']} {failure or 'OK'} | {content.split(chr(10))[0][:80]}", flush=True)
                    if failure:
                        break
                record['passed'] = failure is None
                record['failure'] = failure
                save_record(path, record)
                if failure:
                    raise RuntimeError(f"{case['id']}-{repeat} failed: {failure}")
        spent = recorded_metric_spend(output)
        print(f'All selected cases passed; accounted spend {spent / 1e6:.4f} 元', flush=True)


def check_turn(expect, turn, forbidden):
    if turn['status'] != 'completed':
        return f"run {turn['status']}: {turn['error']}"
    if expect.get('no_privacy_rejection') and 'provider_privacy_rejection' in turn['events']:
        return 'provider privacy rejection (short-contact false positive)'
    answer_status = ('needs_clarification' if 'scope_clarification' in turn['events'] else 'unsupported' if 'metric_declined' in turn['events'] else 'answered' if turn['selected'] else 'conversational')
    if expect.get('answer_status') and expect['answer_status'] != answer_status:
        return f'answer category {answer_status} != {expect["answer_status"]}'
    ids = [s['metric_id'] for s in turn['selected']]
    if expect['metrics'] and not set(expect['metrics']) & set(ids):
        return f'expected metric {expect["metrics"]}, got {ids}'
    for name, value in expect.get('params', {}).items():
        if not any(value in json.dumps(s.get('params', {}).get(name, ''), ensure_ascii=False) for s in turn['selected']):
            return f'param {name} missing {value}'
    blob = json.dumps([turn['content'], turn['results']], ensure_ascii=False)
    if any(fragment in blob for fragment in forbidden):
        return 'sensitive fixture fragment in answer'
    # Paragraph 1 is the server-template direct answer (numbers allowed);
    # the model interpretation sits between it and the "- 指标：" caliber line.
    direct, _, rest = turn['content'].partition('\n\n')
    interpretation = rest.split('- 指标：')[0]
    if NUMBER.search(re.sub(r'客户#\d+', '', interpretation)):
        return 'interpretation contains a number'
    if expect.get('answer_prefix') and not direct.startswith(expect['answer_prefix']):
        return f'direct answer does not start with {expect["answer_prefix"]}: {direct[:60]}'
    for fragment in expect.get('answer', []):
        if fragment not in direct:
            return f'direct answer lacks {fragment}: {direct[:80]}'
    if 'summary' in expect:
        ok = any(set(expect['summary']) <= set(r['columns']) and r['rows'] and all(r['rows'][0][r['columns'].index(k)] == v for k, v in expect['summary'].items()) for r in turn['results'])
        return None if ok else 'summary mismatch'
    if 'key' not in expect:
        return None
    for result in turn['results']:
        if expect['key'] not in result['columns']:
            continue
        rows = {str(row[result['columns'].index(expect['key'])]): dict(zip(result['columns'], row)) for row in result['rows']}
        if all(key in rows and all(rows[key].get(c) == v for c, v in cols.items()) for key, cols in expect['rows'].items()) \
                and not any(a in rows for a in expect.get('absent', [])) \
                and ('row_count' not in expect or len(rows) == expect['row_count']):
            return None
    return 'table values mismatch'

if __name__ == '__main__':
    main()
