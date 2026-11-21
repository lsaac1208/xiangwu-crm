"""Candidate-bound evaluation evidence. Contains no provider or production calls."""
import hashlib
import json
import os
import re
from pathlib import Path

HASH_FIELDS=('binary_sha256','frontend_sha256','rules_sha256','model_config_sha256')

def freeze_campaign(output, access, dataset, expectation_files):
    output=Path(output)
    candidate=access.get('candidate_identity') or {}
    if not candidate.get('code_revision') or any(not re.fullmatch('[a-f0-9]{64}',str(candidate.get(k,''))) for k in HASH_FIELDS):
        raise RuntimeError('Fixture lacks complete candidate identity; start a newly built isolated fixture')
    identity={'candidate':candidate,'dataset':dataset,'fixture_hash':access.get('fixture_hash'),
              'expectations':{Path(p).name:hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in expectation_files}}
    path=output/'candidate.json'
    if path.exists():
        if json.loads(path.read_text())!=identity:
            raise RuntimeError('Candidate, model, rules, data or expectations changed; use a new campaign directory')
    else:
        if any(re.fullmatch('[AMDPR][0-9]+-[0-9]+.json',p.name) for p in output.glob('*.json')):
            raise RuntimeError('Legacy unbound passes cannot be reused; use a new campaign directory')
        output.mkdir(parents=True,exist_ok=True,mode=0o700)
        temporary=path.with_suffix('.tmp')
        fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as f:json.dump(identity,f,ensure_ascii=False,sort_keys=True,indent=2)
        temporary.replace(path)
    candidate_id = hashlib.sha256(json.dumps(identity,sort_keys=True).encode()).hexdigest()
    for record_path in output.glob('*.json'):
        if re.fullmatch('[AMDPR][0-9]+-[0-9]+.json', record_path.name):
            if json.loads(record_path.read_text()).get('candidate_id') != candidate_id:
                raise RuntimeError('Run evidence belongs to another or unbound candidate; cannot reuse any pass')
    return candidate_id

def recorded_metric_spend(output):
    """Restore all metric/daily/copy costs, including interrupted attempts."""
    from agent_live_evaluate import turn_accounted_micro
    spent, seen = 0, set()
    for path in Path(output).glob('*.json'):
        if re.fullmatch('[MDPR][0-9]+-[0-9]+.json',path.name):
            record=json.loads(path.read_text())
            turns=record.get('turns')
            if turns:
                for turn in turns:
                    identity=turn.get('run_id') or turn.get('request_id')
                    if identity and identity in seen: raise RuntimeError('Duplicate paid run identity; reconcile before continuing')
                    if identity: seen.add(identity)
                    spent+=turn_accounted_micro(turn)
            elif 'micro' in record: spent+=int(record['micro'])
            else: raise RuntimeError('Unknown prior spend; reconcile before another paid run')
    return spent


def save_record(path, record):
    """Atomically persist request identity and its conservative cost first."""
    from agent_live_evaluate import turn_accounted_micro
    if record.get('turns'):
        record['micro'] = sum(turn_accounted_micro(turn) for turn in record['turns'])
    path = Path(path)
    temporary = path.with_suffix('.tmp')
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, 'w') as file:
        json.dump(record, file, ensure_ascii=False, indent=2)
        file.write('\n')
    temporary.replace(path)


def collect_paid_turn(output, path, record, turn, request, limit_micro):
    """Resume one idempotent request, retaining unknown charges on every exit.

    The request adapter is injectable so recovery can be tested without a
    provider. Known runs are GET-only; an uncertain POST reuses its saved ID.
    """
    import time
    from agent_live_evaluate import RUN_CEILING_MICRO, turn_accounted_micro
    if turn.get('finished'):
        return turn
    run_id = turn.get('run_id')
    started = time.monotonic()
    if not run_id:
        others = recorded_metric_spend(output) - turn_accounted_micro(turn)
        if others + max(RUN_CEILING_MICRO, turn_accounted_micro(turn)) > limit_micro:
            raise RuntimeError('Spend ceiling cannot cover a full run; evidence retained')
        turn['request_attempted'] = True
        save_record(path, record)
        run = request(f"/api/agent/conversations/{record['conversation_id']}/runs", 'POST',
                      {'message': turn['question'], 'request_id': turn['request_id']})
        run_id = run.get('id') or run.get('run', {}).get('id')
        if not run_id:
            raise RuntimeError('Run ID missing; preserve the request identity before recovery')
        turn['run_id'] = run_id
        save_record(path, record)
    detail = request('/api/agent/runs/' + run_id)
    while detail['run']['status'] in ('queued', 'running'):
        if time.monotonic() - started > 205:
            request(f'/api/agent/runs/{run_id}/cancel', 'POST', {})
            raise RuntimeError('Run exceeded wall-clock bound; retained unknown cost')
        time.sleep(1)
        detail = request('/api/agent/runs/' + run_id)
    events = [json.loads(line[5:]) for line in request(f'/api/agent/runs/{run_id}/events', raw=True).splitlines() if line.startswith('data:')]
    answer = next((m for m in detail.get('messages', []) if m['role'] == 'assistant' and m.get('run_id') == run_id), {})
    turn.update(status=detail['run']['status'], error=detail['run'].get('error'), model=detail['run'].get('model'),
                seconds=round(time.monotonic() - started, 1), content=answer.get('content') or '',
                selected=[e['data'] for e in events if e.get('type') == 'metric_selected'],
                events=[e.get('type') for e in events],
                results=[{k:r.get(k) for k in ('id','columns','rows','query_params','total_rows')} for r in detail.get('results', [])],
                model_usage_events=[e['data'] for e in events if e.get('type') == 'model_usage'],
                model_not_called=any(e.get('type') == 'model_skipped' and e.get('data', {}).get('reason') == 'not_requested' for e in events),
                model_usage_complete=any(e.get('type') == 'done' for e in events), finished=True)
    turn['micro'] = turn_accounted_micro(turn)
    save_record(path, record)
    return turn
