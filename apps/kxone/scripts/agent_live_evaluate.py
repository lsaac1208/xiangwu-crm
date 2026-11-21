#!/usr/bin/env python3
"""Opt-in paid evaluation against an isolated loopback CRM fixture.

This runner records evidence. A completed model run is NOT a semantic pass.
Review answers/results against independent expectations before accepting them.
"""
import argparse
from contextlib import contextmanager
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

RUN_CEILING_MICRO = 3_000_000
GOLD_CASES = [f'A{i:02}' for i in range(1, 31)]
DEDUP_CASES = ['A41', 'A42', 'A43']
EXTRA_GOLD_CASES = ['A37', *DEDUP_CASES]
EXPLICIT_GOLD_CASES = ['A44']


def evaluation_plan(manifest, dataset, selected=None, repeat=None, use_policy=False):
    """Build an offline schedule; this function never reads credentials or calls HTTP.

    The top-level repeat policy deliberately takes precedence over the older
    per-case repeat field for A01..A30. A37 and A41..A43 are additional read-only
    coverage/dedup questions, not interactive/budget scenarios A31..A34/A38..A40.
    """
    records = manifest.get('cases', [])
    cases = {case['id']: case for case in records}
    policy = manifest.get('live_repeat_policy', {})
    critical = policy.get('critical_cases', [])
    if (len(cases) != len(records) or not isinstance(critical, list)
            or len(set(critical)) != len(critical)
            or not set(critical).issubset(GOLD_CASES)
            or not set(GOLD_CASES + ['A35', 'A36'] + EXTRA_GOLD_CASES).issubset(cases)
            or any(cases.get(case_id, {}).get('repeat') != 3 for case_id in DEDUP_CASES)
            or policy.get('critical_total_repetitions') != 3
            or policy.get('other_total_repetitions') != 1):
        raise RuntimeError('Invalid evaluation cases or critical repetition policy')
    if repeat is not None and (type(repeat) is not int or not 1 <= repeat <= 3):
        raise RuntimeError('Repetitions must be 1..3')
    if use_policy and repeat is not None:
        raise RuntimeError('Choose --repeat-policy or --repeat, not both')
    if dataset == 'gold-v1':
        default_cases = GOLD_CASES + EXTRA_GOLD_CASES
        allowed = default_cases + EXPLICIT_GOLD_CASES
    elif dataset == 'stress-v1':
        allowed = default_cases = ['A35', 'A36']
    elif dataset in SEMANTIC_FILES:
        allowed = default_cases = ['A03', 'A07']
    else:
        raise RuntimeError('Unknown evaluation dataset')
    selected = default_cases if selected is None else selected
    if not selected or len(selected) != len(set(selected)) or not set(selected).issubset(allowed):
        raise RuntimeError('Use unique A01..A30/A37/A41..A44 for gold, A35/A36 for stress, or A03/A07 for semantic variants')
    schedule = []
    for case_id in selected:
        required = (3 if case_id in critical else 1) if case_id in GOLD_CASES else cases[case_id].get('repeat')
        if dataset != 'gold-v1':
            required = 3
        if type(required) is not int or not 1 <= required <= 3:
            raise RuntimeError('Invalid required repetitions for ' + case_id)
        repetitions = required if use_policy else (3 if repeat is None else repeat)
        questions = cases[case_id].get('turns') or [cases[case_id]['question']]
        if not questions or any(not isinstance(question, str) or not question.strip() for question in questions):
            raise RuntimeError('Evaluation question missing: ' + case_id)
        schedule.append({'case_id': case_id, 'repetitions': repetitions,
                         'required_repetitions': required, 'turns_per_repetition': len(questions),
                         'meets_repetition_policy': repetitions >= required})
    turns = sum(item['repetitions'] * item['turns_per_repetition'] for item in schedule)
    return {'version': manifest.get('version'), 'dataset': dataset,
            'repeat_source': 'manifest_policy' if use_policy else 'explicit_or_legacy_default',
            'critical_cases': critical, 'schedule': schedule, 'scheduled_model_runs': turns,
            'maximum_run_ceiling_micro': turns * RUN_CEILING_MICRO,
            'acceptance_passed': False, 'paid_requests_authorized': False,
            'note': 'Offline schedule only. Runtime completion still requires independent numerical and semantic review.'}


def long_history_evidence(conversation, completed_run_ids):
    """Require real persisted pairs before the long-history follow-up; no seeded transcript."""
    messages = conversation.get('messages', [])
    if (len(completed_run_ids) != 11 or len(set(completed_run_ids)) != 11
            or conversation.get('has_more') is not False or len(messages) != 22
            or len({message.get('id') for message in messages}) != 22):
        raise RuntimeError('Long history requires eleven real completed message pairs')
    for run_id in completed_run_ids:
        pair = [message for message in messages if message.get('run_id') == run_id]
        if (len(pair) != 2 or sorted(message.get('role', '') for message in pair) != ['assistant', 'user']
                or any(message.get('status') != 'completed' for message in pair)):
            raise RuntimeError('Long history contains a missing, duplicate or unfinished message pair')
    return {'prior_message_count': len(messages), 'prior_completed_run_ids': completed_run_ids,
            'message_ids': [message['id'] for message in messages], 'source': 'persisted_conversation_api',
            'semantic_acceptance': 'pending independent review'}


def operational_event_evidence(events):
    """Preserve useful execution metadata, never arbitrary message/delta payloads."""
    fields = {
        'provider_switch': {'from_model', 'model', 'reason'},
        'context_checkpoint': {'segment', 'reason', 'result_count'},
        'context_usage': {'rules_bytes', 'history_and_question_bytes', 'tool_bytes',
                          'assistant_bytes', 'reasoning_bytes', 'tools_schema_bytes',
                          'request_bytes', 'segment'},
        'snapshot_ready': {'source_read_ms', 'source_rows'},
        'done': {'status'},
    }
    evidence = []
    for event in events:
        kind, data = event.get('type'), event.get('data')
        if kind == 'provider_privacy_rejection' and isinstance(data, dict):
            findings = []
            candidates = data.get('findings')
            for finding in (candidates if isinstance(candidates, list) else [])[:20]:
                if (isinstance(finding, dict) and finding.get('field') in {'content', 'reasoning', 'tool_arguments'}
                        and finding.get('rule') in {'known_identity', 'known_contact', 'credential', 'phone', 'wechat', 'email', 'id_card', 'unknown_name', 'oversized', 'sanitization', 'guard_unavailable'}
                        and all(type(finding.get(k)) is int and finding[k] >= 0 for k in ('count', 'bytes'))):
                    findings.append({k: finding[k] for k in ('field', 'rule', 'count', 'bytes')})
            evidence.append({'seq': event.get('seq'), 'type': kind, 'data': {'findings': findings}})
        if kind in fields and isinstance(data, dict):
            evidence.append({'seq': event.get('seq'), 'type': kind,
                             'data': {key: value for key, value in data.items()
                                      if key in fields[kind] and isinstance(value, (str, int, float, bool))}})
    return evidence


def fault_injection_evidence(access):
    fault = access.get('fault_injection')
    if fault is None:
        return None
    expected = {'kind': 'primary_http_503', 'scope': 'all_primary_calls_in_isolated_fixture',
                'synthetic_primary_failure': True, 'real_fallback': True}
    if not isinstance(fault, dict) or any(fault.get(key) != value for key, value in expected.items()):
        raise RuntimeError('Unrecognized isolated fault injection declaration')
    return expected


@contextmanager
def campaign_lock(output):
    descriptor = os.open(output / 'campaign.lock', os.O_CREAT | os.O_RDWR, 0o600)
    try:
        os.fchmod(descriptor, 0o600)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError('Another evaluation process already owns this evidence directory') from None
        yield
    finally:
        os.close(descriptor)


def turn_accounted_micro(turn):
    """Count known costs exactly, unknown costs by their retained reservation.

    A missing/incomplete event trail cannot establish the full paid call count.
    Keep the entire run ceiling until evidence is complete, even on recovery.
    """
    if not (turn.get('request_attempted') or turn.get('run_id') or turn.get('finished')):
        return 0
    events = turn.get('model_usage_events')
    no_model = turn.get('model_not_called') is True
    reliable = (turn.get('finished') and turn.get('status') == 'completed'
                and turn.get('model_usage_complete') and isinstance(events, list)
                and (bool(events) or no_model))
    if no_model and events:
        reliable = False
    total, seen = 0, {}
    for event in events or []:
        reservation = event.get('reservation_id')
        if not isinstance(reservation, str) or not reservation:
            reliable = False
            continue
        if reservation in seen:
            if seen[reservation] != event:
                reliable = False
            continue
        seen[reservation] = event
        if event.get('settle_result') == 'settled' and event.get('has_usage') is True:
            amount = event.get('estimated_actual_micro')
            valid = type(amount) is int and amount >= 0
        else:
            amount = event.get('reservation_micro')
            valid = type(amount) is int and amount > 0
            actual = event.get('estimated_actual_micro')
            if event.get('has_usage') is True and type(actual) is int and actual >= 0:
                # An over-reservation charge is recorded even when settlement
                # returns conflict. Never hide that known larger amount.
                amount = max(amount if valid else 0, actual)
                valid = amount > 0
            if event.get('settle_result') not in ('unknown', 'failed', 'conflict'):
                reliable = False
        if not valid:
            reliable = False
            continue
        total += amount
    return total if reliable else max(total, RUN_CEILING_MICRO)


def campaign_accounted_micro(output, exclude_request_id=None):
    total, seen = 0, set()
    for path in sorted(output.glob('A[0-9][0-9]-[123].json')):
        for turn in json.loads(path.read_text()).get('turns', []):
            request_id = turn.get('request_id')
            if exclude_request_id and request_id == exclude_request_id:
                continue
            identity = turn.get('run_id') or request_id
            if identity and identity in seen:
                raise RuntimeError('Duplicate paid run identity across campaign evidence; reconcile before continuing')
            if identity:
                seen.add(identity)
            total += turn_accounted_micro(turn)
    return total


def check_new_run_allowance(output, turn, limit_micro):
    # An uncertain POST already holds this request's full ceiling. Replaying
    # its same request ID must not reserve that ceiling a second time.
    others = campaign_accounted_micro(output, exclude_request_id=turn['request_id'])
    projected = others + max(RUN_CEILING_MICRO, turn_accounted_micro(turn))
    if projected > limit_micro:
        raise RuntimeError('Campaign allowance cannot cover another full run; evidence saved')


def parse_run_time(value):
    if not isinstance(value, str):
        raise RuntimeError('Server run timestamp missing; cannot claim an elapsed duration')
    # Go emits RFC3339Nano. Python 3.9 accepts only limited fractional widths;
    # normalize a parsing copy to microseconds and preserve original evidence.
    normalized = value.replace('Z', '+00:00')
    normalized = re.sub(r'\.(\d+)(?=[+-]\d{2}:\d{2}$)',
                        lambda match: '.' + match.group(1)[:6].ljust(6, '0'), normalized)
    parsed = datetime.datetime.fromisoformat(normalized)
    if parsed.tzinfo is None:
        raise RuntimeError('Server run timestamp lacks a timezone')
    return parsed


def run_duration_seconds(run):
    elapsed = (parse_run_time(run.get('finished_at')) - parse_run_time(run.get('created_at'))).total_seconds()
    if elapsed < 0:
        raise RuntimeError('Invalid server run time order')
    return round(elapsed, 3)


def stress_content_hash(manifest):
    """Independent Python generator for the declared business-row fingerprint.

    The Go harness hashes actual imported SQL rows. Comparing both with this
    versioned manifest prevents treating the seven-row fixture as a stress run.
    """
    recipe = manifest['generator']
    required = {'lead_id_min': 1, 'lead_id_max': 10001, 'followups_per_lead': 5,
                'odd_source': 'A', 'even_source': 'B', 'odd_status': 'converted',
                'even_status': 'following', 'odd_deal_yuan': 1, 'even_deal_yuan': 0,
                'refund_yuan': 0, 'employee_user_id': 'stress-sales-a',
                'created_at': '2026-10-08T09:00:00+08:00', 'followup_minutes': [10, 11, 12, 13, 14]}
    if manifest['version'] != 'agent-stress-fixture-v1' or recipe != required:
        raise RuntimeError('Unrecognized stress recipe; version and independent oracle must be reviewed')
    digest = hashlib.sha256(b'agent-stress-fixture-v1\n')
    for lead_id in range(1, 10002):
        source, status, cents = ('A', 'converted', 100) if lead_id % 2 else ('B', 'following', 0)
        digest.update((f'L\t{lead_id}\tSTRESS-{lead_id:05d}\t压力测试客户{lead_id}\t{source}\t'
                       f'{status}\t{cents}\t0\tstress-sales-a\t2026-10-08T09:00:00+08:00\n').encode())
    for followup_id in range(1, 50006):
        digest.update((f'F\t{followup_id}\t{(followup_id - 1) // 5 + 1}\tstress-sales-a\t压力测试跟进\t'
                       f'2026-10-08T09:{10 + (followup_id - 1) % 5:02d}:00+08:00\n').encode())
    return digest.hexdigest()


SEMANTIC_FILES = {
    'semantic-active-timestamp-v1': 'Agent-Semantic-Active-Timestamp-Fixture.json',
    'semantic-missing-timestamp-v1': 'Agent-Semantic-Missing-Timestamp-Fixture.json',
}
SEMANTIC_GOLD_HASH = 'bc3c9cc69c0bda41f2e5589fcc37611f1ffe996d1ef47ddabc309fdc82936c92'


def validate_semantic_dataset(dataset, manifest, access, docdir):
    base_raw = (docdir / 'Agent-Evaluation-Fixture.json').read_bytes()
    if (hashlib.sha256(base_raw).hexdigest() != SEMANTIC_GOLD_HASH
            or manifest.get('base_fixture_sha256') != SEMANTIC_GOLD_HASH
            or access.get('base_fixture_sha256') != SEMANTIC_GOLD_HASH
            or manifest.get('version') != 'agent-' + dataset
            or access.get('fixture_version') != manifest['version']
            or manifest.get('dataset') != dataset):
        raise RuntimeError('Semantic fixture identity mismatch; no paid request allowed')
    active = dataset == 'semantic-active-timestamp-v1'
    target, before, after = ((2, None, '2026-09-02T12:00:00+08:00') if active
                             else (3, '2026-09-06T16:00:00+08:00', None))
    semantic = manifest.get('semantic_variant', {})
    expected = {'current_converted_by_status': 2, 'current_converted_by_result_status': 2,
                'nonempty_converted_at': 3 if active else 1, 'status_or_nonempty_converted_at': 3 if active else 2,
                'total_leads': 7, 'current_conversion_rate_percent': 200 / 7,
                'source_current_converted': {'good_school': 1, 'staff_d_douyin': 1}}
    if (semantic.get('only_business_change') != {'lead_id': target, 'field': 'converted_at', 'before': before, 'after': after}
            or semantic.get('expected') != expected):
        raise RuntimeError('Semantic change declaration or independent expectations changed; no paid request allowed')
    # Undo only the declared business field and compare the entire document.
    restored = json.loads(json.dumps(manifest))
    targets = [row for row in restored['tables']['leads'] if row['id'] == target]
    if len(targets) != 1 or 'converted_at' not in targets[0] or targets[0]['converted_at'] != after:
        raise RuntimeError('Semantic business change mismatch; no paid request allowed')
    if before is None:
        del targets[0]['converted_at']
    else:
        targets[0]['converted_at'] = before
    for key in ('dataset', 'base_fixture_sha256', 'semantic_variant'):
        del restored[key]
    restored['version'] = 'agent-evaluation-fixture-v1'
    if restored != json.loads(base_raw):
        raise RuntimeError('Undeclared semantic fixture changes; no paid request allowed')


def validate_dataset(dataset, access, docdir):
    if access.get('dataset', 'gold-v1') != dataset:
        raise RuntimeError('Explicit dataset differs from the isolated server; no paid request allowed')
    if dataset not in ['gold-v1', 'stress-v1', *SEMANTIC_FILES]:
        raise RuntimeError('Unrecognized dataset; no paid request allowed')
    filename = SEMANTIC_FILES.get(dataset) or ('Agent-Stress-Fixture.json' if dataset == 'stress-v1' else 'Agent-Evaluation-Fixture.json')
    raw = (docdir / filename).read_bytes()
    if hashlib.sha256(raw).hexdigest() != access['fixture_hash']:
        raise RuntimeError('Fixture hash mismatch; no paid request allowed')
    manifest = json.loads(raw)
    if dataset == 'stress-v1':
        expected = stress_content_hash(manifest)
        if (expected != manifest['fingerprint']['content_sha256']
                or expected != access.get('fixture_content_sha256')
                or manifest['version'] != access.get('fixture_version')):
            raise RuntimeError('Stress content or version mismatch; no paid request allowed')
    if dataset in SEMANTIC_FILES:
        validate_semantic_dataset(dataset, manifest, access, docdir)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture-access', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--base-url')
    parser.add_argument('--dataset', choices=['gold-v1', 'stress-v1', *SEMANTIC_FILES], default='gold-v1')
    parser.add_argument('--cases', help='Default A01..A30 plus A37,A41,A42,A43 for gold; A44 long history requires explicit selection. A35,A36 for stress, A03,A07 for semantic variants')
    parser.add_argument('--repeat', type=int, help='Explicit repetitions (1..3); legacy default is 3')
    parser.add_argument('--repeat-policy', action='store_true', help='Use the manifest critical/noncritical repetition policy')
    parser.add_argument('--print-plan', action='store_true', help='Print an offline policy schedule and exit; no access file or paid opt-in required')
    parser.add_argument('--max-spend-yuan', type=float, default=60)
    parser.add_argument('--allow-paid-requests', action='store_true')
    args = parser.parse_args()
    docs = Path(__file__).resolve().parents[1] / 'docs'
    manifest = json.loads((docs / 'Agent-Evaluation-Cases.json').read_text())
    try:
        plan = evaluation_plan(manifest, args.dataset, args.cases.split(',') if args.cases else None,
                               args.repeat, args.repeat_policy or (args.print_plan and args.repeat is None))
    except RuntimeError as error:
        parser.error(str(error))
    if args.print_plan:
        print(json.dumps(plan, ensure_ascii=False, indent=2))
        return
    if not args.fixture_access or not args.output:
        parser.error('Paid execution requires --fixture-access and --output')
    if not args.allow_paid_requests or not 0 < args.max_spend_yuan <= 300:
        parser.error('Explicit paid opt-in, 1..3 repeats and a ceiling within 300 yuan are required')
    if args.fixture_access.stat().st_mode & 0o077:
        parser.error('Fixture access file must be private (0600)')
    access = json.loads(args.fixture_access.read_text())
    base = (args.base_url or access['base_url']).rstrip('/')
    url = urllib.parse.urlparse(base)
    if url.scheme != 'http' or url.hostname not in ('127.0.0.1', '::1') or url.username or url.password or url.path or url.query or url.fragment:
        parser.error('Only an explicit isolated loopback CRM is allowed')
    repo = Path(__file__).resolve().parents[3]
    output = args.output.resolve()
    if output == repo or repo in output.parents:
        parser.error('Evidence must be outside the repository')
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    output.chmod(0o700)
    with campaign_lock(output):
        run_campaign(args, parser, access, base, output, plan)


def run_campaign(args, parser, access, base, output, plan):
    from agent_evaluation_campaign import freeze_campaign
    candidate_id = freeze_campaign(output, access, args.dataset, [Path(__file__).parent.parent / 'docs' / 'Agent-Evaluation-Cases.json', Path(__file__), Path(__file__).with_name('agent_evaluation_campaign.py')])
    docdir = Path(__file__).resolve().parents[1] / 'docs'
    fixture = validate_dataset(args.dataset, access, docdir)
    fault_injection = fault_injection_evidence(access)
    case_bytes = (docdir / 'Agent-Evaluation-Cases.json').read_bytes()
    cases = {c['id']: c for c in json.loads(case_bytes)['cases']}
    case_hash = hashlib.sha256(case_bytes).hexdigest()

    # Proxy environment variables must not send the isolated session token
    # through a third-party proxy, even though the destination is loopback.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(path, method='GET', body=None):
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
        req = urllib.request.Request(base + path, data=data, method=method,
                                     headers={'Authorization': 'Bearer ' + access['token'], 'Content-Type': 'application/json'})
        try:
            with opener.open(req, timeout=25) as response:
                raw = response.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            # CRM errors are sanitized by the service; never include headers.
            detail = error.read(4096).decode(errors='replace')
            raise RuntimeError(f'CRM HTTP {error.code}: {detail}') from None

    def save(path, value):
        temporary = path.with_suffix('.tmp')
        descriptor = os.open(temporary, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, 'w') as file:
            json.dump(value, file, ensure_ascii=False, indent=2)
            file.write('\n')
        temporary.replace(path)

    manifest_path = output / 'campaign.json'
    settings = request('/api/agent/settings')
    if manifest_path.exists():
        campaign = json.loads(manifest_path.read_text())
        if (campaign['fixture_hash'] != access['fixture_hash'] or campaign['configured_model'] != access['model']
                or campaign.get('dataset', 'gold-v1') != args.dataset):
            parser.error('Existing campaign belongs to a different fixture or model')
        if campaign.get('evaluation_cases_sha256', case_hash) != case_hash:
            parser.error('Evaluation questions or expectations changed since campaign creation')
        if campaign.get('fault_injection') != fault_injection:
            parser.error('Fault injection mode changed since campaign creation')
    else:
        campaign = {'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                    'fixture_hash': access['fixture_hash'], 'configured_model': access['model'],
                    'dataset': args.dataset, 'fixture_version': fixture['version'],
                    'fixture_content_sha256': access.get('fixture_content_sha256'),
                    'base_fixture_sha256': access.get('base_fixture_sha256'),
                    'semantic_variant': fixture.get('semantic_variant'),
                    'initial_global_accounted_micro_observation': settings['spent_micro'] + settings['reserved_micro'],
                    'campaign_cost_basis': 'own run model_usage events; incomplete trail retains full run ceiling',
                    'price_basis': 'administrator configuration; supplier invoice not reconciled',
                    'semantic_acceptance': 'pending independent review', 'acceptance_passed': False}
    campaign['evaluation_cases_sha256'] = case_hash
    campaign['execution_plan'] = plan
    campaign['fault_injection'] = fault_injection
    campaign['provider_failure_basis'] = ('Synthetic primary HTTP 503 in isolated fixture; backup calls use the real provider.'
                                          if fault_injection else 'No declared fault injection; consult recorded provider events.')
    save(manifest_path, campaign)

    for scheduled in plan['schedule']:
        case_id = scheduled['case_id']
        case = cases[case_id]
        for repeat in range(1, scheduled['repetitions'] + 1):
            path = output / f'{case_id}-{repeat}.json'
            if path.exists():
                record = json.loads(path.read_text())
                if record.get('finished'):
                    print(f'{case_id}-{repeat}: recorded; no duplicate paid run', flush=True)
                    continue
            else:
                record = {'candidate_id': candidate_id, 'case': case, 'repeat': repeat, 'turns': [], 'finished': False,
                          'semantic_acceptance': 'pending independent review', 'acceptance_passed': False,
                          'fixture_hash': access['fixture_hash'], 'configured_model': access['model']}
                save(path, record)
            questions = case.get('turns') or [case['question']]
            if not record.get('conversation_id'):
                record['conversation_id'] = request('/api/agent/conversations', 'POST', {'title': f'真实验收 {case_id}-{repeat}'})['id']
                save(path, record)
            for index, question in enumerate(questions):
                if index < len(record['turns']) and record['turns'][index].get('finished'):
                    if record['turns'][index].get('status') != 'completed':
                        raise RuntimeError('Earlier paid run failed; use a new evidence directory only after reviewing the cause')
                    continue
                if index == len(record['turns']):
                    record['turns'].append({'question': question, 'request_id': 'eval-' + str(uuid.uuid4()),
                                            'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat()})
                    save(path, record)
                turn = record['turns'][index]
                if turn['question'] != question:
                    raise RuntimeError('Question changed since this request was saved; reconcile evidence before continuing')
                poll_started = time.monotonic()
                run_id = turn.get('run_id')
                if run_id:
                    # A known run never needs another POST on reconnection.
                    # Missing server state is an error, not permission to rerun.
                    detail = request('/api/agent/runs/' + run_id)
                else:
                    check_new_run_allowance(output, turn, round(args.max_spend_yuan * 1_000_000))
                    if case_id == 'A44' and index == 11 and not turn.get('request_attempted'):
                        conversation = request('/api/agent/conversations/' + record['conversation_id'])
                        turn['long_history_evidence'] = long_history_evidence(
                            conversation, [item['run_id'] for item in record['turns'][:11]])
                        save(path, record)
                    # Persist this conservative reservation before the network
                    # call. A lost response resumes with this same request ID.
                    turn['request_attempted'] = True
                    save(path, record)
                    run = request('/api/agent/conversations/' + record['conversation_id'] + '/runs', 'POST',
                                  {'message': question, 'request_id': turn['request_id']})
                    run_id = run.get('id') or run.get('run', {}).get('id')
                    if not run_id:
                        raise RuntimeError('Run ID missing; do not automatically create another request')
                    turn['run_id'] = run_id
                    save(path, record)
                    detail = request('/api/agent/runs/' + run_id)
                while True:
                    status = detail['run']['status']
                    if status not in ('queued', 'running'):
                        break
                    age = (datetime.datetime.now(datetime.timezone.utc) - parse_run_time(detail['run']['created_at'])).total_seconds()
                    if age > 205 or time.monotonic() - poll_started > 205:
                        request('/api/agent/runs/' + run_id + '/cancel', 'POST', {})
                        raise RuntimeError('Run exceeded its expected wall-clock bound; cancellation requested')
                    time.sleep(1)
                    detail = request('/api/agent/runs/' + run_id)
                turn.update(duration_seconds=run_duration_seconds(detail['run']),
                            collection_poll_seconds=round(time.monotonic() - poll_started, 3),
                            status=status, detail=detail, finished=True)
                req = urllib.request.Request(base + '/api/agent/runs/' + run_id + '/events',
                                             headers={'Authorization': 'Bearer ' + access['token']})
                with opener.open(req, timeout=25) as response:
                    events = [json.loads(line[5:].strip()) for line in response.read().decode().splitlines() if line.startswith('data:')]
                turn['model_usage_events'] = [event['data'] for event in events if event.get('type') == 'model_usage']
                turn['model_usage_complete'] = any(event.get('type') == 'done' for event in events)
                turn['model_not_called'] = any(event.get('type') == 'model_skipped' and event.get('data', {}).get('reason') == 'not_requested' for event in events)
                turn['operational_events'] = operational_event_evidence(events)
                if args.dataset == 'stress-v1' and case_id == 'A36':
                    # Preserve actual result endpoint pages separately from
                    # the run's default preview. Missing qualifying results
                    # stay visible as a failed/manual-review requirement.
                    turn['pagination_evidence'] = []
                    for result in detail.get('results', []):
                        if result.get('total_rows') == 10001:
                            result_id = result['id']
                            turn['pagination_evidence'].append({
                                'result_id': result_id,
                                'first_page': request('/api/agent/results/' + result_id + '?limit=20&offset=0'),
                                'second_page': request('/api/agent/results/' + result_id + '?limit=20&offset=20')})
                turn['accounted_micro'] = turn_accounted_micro(turn)
                save(path, record)
                print(f'{case_id}-{repeat} turn {index + 1}: {status}, {turn["duration_seconds"]}s, {len(detail.get("results", []))} saved results', flush=True)
                if status != 'completed':
                    raise RuntimeError(f'{case_id}-{repeat} did not complete: {detail["run"].get("error", status)}; review before spending more')
            record['finished'] = True
            save(path, record)
    latest = request('/api/agent/settings')
    campaign.update(runtime_collection_finished=True,
                    campaign_accounted_micro=campaign_accounted_micro(output),
                    final_global_spent_micro_observation=latest['spent_micro'],
                    final_global_reserved_micro_observation=latest['reserved_micro'])
    save(manifest_path, campaign)
    print('Evidence collection finished; numerical, privacy and semantic review remains mandatory.', flush=True)


if __name__ == '__main__':
    main()
