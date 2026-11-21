#!/usr/bin/env python3
"""Driver safety tests only; no production or provider network requests."""
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest

import agent_live_evaluate as driver


def settled_turn(amount=1000):
    return {'run_id': 'known-run', 'request_id': 'known-request', 'finished': True,
            'status': 'completed', 'model_usage_complete': True,
            'model_usage_events': [{'reservation_id': 'reservation-one', 'has_usage': True,
                                    'settle_result': 'settled', 'estimated_actual_micro': amount}]}


class CampaignAccountingTests(unittest.TestCase):
    def test_long_history_is_explicit_and_keeps_default_campaign_unchanged(self):
        docs = Path(driver.__file__).parent.parent / 'docs'
        manifest = json.loads((docs / 'Agent-Evaluation-Cases.json').read_text())
        case = next(case for case in manifest['cases'] if case['id'] == 'A44')
        default = driver.evaluation_plan(manifest, 'gold-v1', use_policy=True)
        self.assertNotIn('A44', [item['case_id'] for item in default['schedule']])
        self.assertEqual(default['scheduled_model_runs'], 102)
        plan = driver.evaluation_plan(manifest, 'gold-v1', ['A44'], use_policy=True)
        self.assertEqual(plan['scheduled_model_runs'], 12)
        self.assertEqual(plan['maximum_run_ceiling_micro'], 36_000_000)
        self.assertEqual(len(case['turns']), 12)
        self.assertFalse(plan['acceptance_passed'])
        for dataset in ['stress-v1', *driver.SEMANTIC_FILES]:
            with self.assertRaises(RuntimeError):
                driver.evaluation_plan(manifest, dataset, ['A44'], use_policy=True)

    def test_long_history_requires_real_persisted_message_pairs(self):
        runs = ['run-' + str(i) for i in range(11)]
        messages = [{'id': f'{run}-{role}', 'run_id': run, 'role': role,
                     'status': 'completed', 'content': 'must-not-retain'}
                    for run in runs for role in ['user', 'assistant']]
        conversation = {'messages': messages, 'has_more': False}
        evidence = driver.long_history_evidence(conversation, runs)
        self.assertEqual(evidence['prior_message_count'], 22)
        self.assertEqual(evidence['prior_completed_run_ids'], runs)
        self.assertNotIn('must-not-retain', json.dumps(evidence))
        for invalid in [messages[:-1], messages[:-1] + [messages[0]],
                        [dict(message, status='interrupted') if i == 3 else message
                         for i, message in enumerate(messages)]]:
            with self.assertRaisesRegex(RuntimeError, 'history'):
                driver.long_history_evidence({'messages': invalid, 'has_more': False}, runs)

    def test_long_history_oracle_is_independent_of_model_and_snapshot(self):
        docs = Path(driver.__file__).parent.parent / 'docs'
        case = next(case for case in json.loads((docs / 'Agent-Evaluation-Cases.json').read_text())['cases'] if case['id'] == 'A44')
        fixture = json.loads((docs / 'Agent-Evaluation-Fixture.json').read_text())['tables']
        with sqlite3.connect(':memory:') as database:
            database.execute('CREATE TABLE leads(id INTEGER PRIMARY KEY, source_channel TEXT, current_owner_user_id TEXT, status TEXT)')
            database.executemany('INSERT INTO leads VALUES(?,?,?,?)', [(row['id'], row['source_channel'], row['current_owner_user_id'], row['status']) for row in fixture['leads']])
            database.execute('CREATE TABLE lead_followups(id INTEGER PRIMARY KEY, lead_id INTEGER, sales_user_id TEXT, created_at TEXT, next_followup_at TEXT, business_stage TEXT, remark TEXT)')
            database.executemany('INSERT INTO lead_followups VALUES(?,?,?,?,?,?,?)',
                                 [(row['id'], row['lead_id'], row['sales_user_id'], row['created_at'], row.get('next_followup_at'), row['business_stage'], row['remark']) for row in fixture['lead_followups']])
            for turn in case['expected']['turns']:
                actual = list(database.execute(case['independent_sql'], turn['params']))
                self.assertEqual([row[0] for row in actual], turn['followup_ids'])
                self.assertEqual(len({row[1] for row in actual}), turn['distinct_leads'])
                self.assertEqual(sum(row[2] is not None for row in actual), turn['historical_plan_records'])
                if 'matching_record' in turn:
                    expected = turn['matching_record']
                    self.assertEqual(actual[0][3:], tuple(expected[key] for key in
                                                         ['business_stage', 'current_owner', 'current_status', 'shanghai_created_at', 'remark']))
        self.assertEqual(case['expected']['turns'][-1]['followup_ids'], [5, 7])

    def test_privacy_diagnostics_never_retain_fragments_or_unknown_labels(self):
        finding = {'field': 'reasoning', 'rule': 'unknown_name', 'count': 1, 'bytes': 120, 'fragment': 'must-not-save'}
        events = [{'seq': 7, 'type': 'provider_privacy_rejection', 'data': {'findings': [finding, dict(finding, field='private text')], 'raw': 'must-not-save'}}]
        retained = driver.operational_event_evidence(events)
        self.assertNotIn('must-not-save', json.dumps(retained))
        self.assertNotIn('private text', json.dumps(retained))
        self.assertEqual(retained[0]['data']['findings'], [{k: finding[k] for k in ('field', 'rule', 'count', 'bytes')}])

    def test_offline_plan_uses_authoritative_critical_policy_and_adds_coverage_case(self):
        manifest = json.loads((Path(driver.__file__).parent.parent / 'docs/Agent-Evaluation-Cases.json').read_text())
        plan = driver.evaluation_plan(manifest, 'gold-v1', use_policy=True)
        cases = {item['case_id']: item for item in plan['schedule']}
        self.assertEqual(len(cases), 34)
        # These case-level repeat values are stale; the stronger shared policy wins.
        for case_id in ['A03', 'A13', 'A21', 'A37', 'A41', 'A42', 'A43']:
            self.assertEqual(cases[case_id]['repetitions'], 3)
        self.assertEqual(cases['A11']['repetitions'], 1)
        self.assertEqual(cases['A26']['turns_per_repetition'], 3)
        self.assertEqual(cases['A27']['turns_per_repetition'], 2)
        self.assertEqual(cases['A43']['turns_per_repetition'], 2)
        self.assertEqual(len(plan['critical_cases']), 24)
        self.assertEqual(plan['scheduled_model_runs'], 102)
        self.assertEqual(plan['maximum_run_ceiling_micro'], 306_000_000)
        self.assertFalse(plan['paid_requests_authorized'])
        self.assertFalse(plan['acceptance_passed'])
        focused = driver.evaluation_plan(manifest, 'gold-v1', ['A08'], repeat=1)
        self.assertFalse(focused['schedule'][0]['meets_repetition_policy'])
        for dataset in ['stress-v1', *driver.SEMANTIC_FILES]:
            self.assertEqual(driver.evaluation_plan(manifest, dataset, use_policy=True)['scheduled_model_runs'], 6)

    def test_plan_rejects_duplicate_or_unsupported_cases_and_changed_policy(self):
        manifest = json.loads((Path(driver.__file__).parent.parent / 'docs/Agent-Evaluation-Cases.json').read_text())
        for cases in [[], ['A08', 'A08'], ['A31'], ['A35'], ['A38']]:
            with self.assertRaises(RuntimeError):
                driver.evaluation_plan(manifest, 'gold-v1', cases, use_policy=True)
        with self.assertRaisesRegex(RuntimeError, 'not both'):
            driver.evaluation_plan(manifest, 'gold-v1', repeat=1, use_policy=True)
        manifest['live_repeat_policy']['critical_cases'].append('A38')
        with self.assertRaisesRegex(RuntimeError, 'critical repetition'):
            driver.evaluation_plan(manifest, 'gold-v1', use_policy=True)

    def test_dedup_variants_independently_reconcile_original_fixture(self):
        docs = Path(driver.__file__).parent.parent / 'docs'
        manifest = json.loads((docs / 'Agent-Evaluation-Cases.json').read_text())
        cases = {case['id']: case for case in manifest['cases']}
        rows = json.loads((docs / 'Agent-Evaluation-Fixture.json').read_text())['tables']['lead_followups']
        # Independent raw-fixture SQLite, never the application's entity index,
        # snapshot projection, measurement tool or model-provided SQL output.
        with sqlite3.connect(':memory:') as database:
            database.execute('CREATE TABLE lead_followups(id INTEGER PRIMARY KEY, lead_id INTEGER, sales_user_id TEXT)')
            database.executemany('INSERT INTO lead_followups VALUES(?,?,?)',
                                 [(row['id'], row['lead_id'], row['sales_user_id']) for row in rows])
            for case_id in driver.DEDUP_CASES:
                case = cases[case_id]
                actual = list(database.execute(case['independent_sql']))
                groups = {actor: (count, distinct) for scope, actor, count, distinct in actual if scope == 'group'}
                total = [(count, distinct) for scope, _, count, distinct in actual if scope == 'global']
                self.assertEqual(groups, {'sales-a': (6, 3), 'sales-b': (1, 1)}, case_id)
                self.assertEqual(total, [(7, 3)], case_id)
                expected = case['expected']
                self.assertEqual(expected['followups_by_actor'], {actor: counts[0] for actor, counts in groups.items()})
                self.assertEqual(expected['distinct_leads_by_actor'], {actor: counts[1] for actor, counts in groups.items()})
                self.assertEqual((expected['global_followup_count'], expected['global_distinct_leads']), total[0])
                lead_ids = [row[0] for row in database.execute('SELECT DISTINCT lead_id FROM lead_followups ORDER BY lead_id')]
                self.assertEqual(expected['global_lead_ids'], lead_ids)
                if 'independent_overlap_sql' in case:
                    self.assertEqual(expected['overlap_lead_ids'], [row[0] for row in database.execute(case['independent_overlap_sql'])])
                    self.assertEqual(expected['overlap_lead_ids'], [1])
        plan = driver.evaluation_plan(manifest, 'gold-v1', driver.DEDUP_CASES, use_policy=True)
        self.assertEqual(plan['scheduled_model_runs'], 12)
        self.assertEqual([item['repetitions'] for item in plan['schedule']], [3, 3, 3])
        self.assertFalse(cases['A42']['expected']['proposed_global_count_is_correct'])
        self.assertEqual(len(cases['A43']['turns']), 2)
        self.assertNotIn('A41', manifest['live_repeat_policy']['critical_cases'])

    def test_dedup_variants_cannot_weaken_repetitions_or_use_another_dataset(self):
        docs = Path(driver.__file__).parent.parent / 'docs'
        manifest = json.loads((docs / 'Agent-Evaluation-Cases.json').read_text())
        for dataset in ['stress-v1', *driver.SEMANTIC_FILES]:
            with self.assertRaises(RuntimeError):
                driver.evaluation_plan(manifest, dataset, ['A41'], use_policy=True)
        diagnostic = driver.evaluation_plan(manifest, 'gold-v1', ['A41'], repeat=1)
        self.assertFalse(diagnostic['schedule'][0]['meets_repetition_policy'])
        next(case for case in manifest['cases'] if case['id'] == 'A41')['repeat'] = 1
        with self.assertRaisesRegex(RuntimeError, 'critical repetition'):
            driver.evaluation_plan(manifest, 'gold-v1', ['A41'], use_policy=True)

    def test_print_plan_requires_neither_credential_file_nor_paid_opt_in(self):
        result = subprocess.run([sys.executable, str(Path(driver.__file__)), '--print-plan',
                                 '--cases', 'A08,A13,A21,A37'], capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        plan = json.loads(result.stdout)
        self.assertEqual(plan['scheduled_model_runs'], 12)
        self.assertEqual(plan['repeat_source'], 'manifest_policy')
        self.assertFalse(plan['paid_requests_authorized'])

    def test_operational_evidence_retains_switch_and_context_but_never_draft_or_reasoning(self):
        events = [
            {'seq': 1, 'type': 'provider_switch', 'data': {'from_model': 'primary', 'model': 'backup', 'reason': 'primary_unavailable', 'secret': 'must-not-save'}},
            {'seq': 2, 'type': 'context_usage', 'data': {'request_bytes': 48000, 'segment': 1, 'reasoning_content': 'must-not-save'}},
            {'seq': 3, 'type': 'context_checkpoint', 'data': {'segment': 2, 'reason': 'capacity', 'result_count': 3}},
            {'seq': 4, 'type': 'delta', 'data': {'text': 'must-not-save'}},
            {'seq': 5, 'type': 'done', 'data': {'status': 'completed'}},
        ]
        retained = driver.operational_event_evidence(events)
        self.assertEqual([event['seq'] for event in retained], [1, 2, 3, 5])
        self.assertNotIn('must-not-save', json.dumps(retained))

    def test_fault_injection_must_identify_synthetic_primary_and_real_backup(self):
        fault = {'kind': 'primary_http_503', 'scope': 'all_primary_calls_in_isolated_fixture',
                 'synthetic_primary_failure': True, 'real_fallback': True}
        self.assertIsNone(driver.fault_injection_evidence({}))
        self.assertEqual(driver.fault_injection_evidence({'fault_injection': dict(fault, private='must-not-save')}), fault)
        with self.assertRaisesRegex(RuntimeError, 'fault injection'):
            driver.fault_injection_evidence({'fault_injection': dict(fault, real_fallback=False)})

    def test_stress_dataset_requires_explicit_mode_version_and_actual_content_hash(self):
        docs = Path(driver.__file__).parent.parent / 'docs'
        raw = (docs / 'Agent-Stress-Fixture.json').read_bytes()
        manifest = json.loads(raw)
        expected = driver.stress_content_hash(manifest)
        self.assertEqual(expected, manifest['fingerprint']['content_sha256'])
        access = {'dataset': 'stress-v1', 'fixture_version': manifest['version'],
                  'fixture_hash': hashlib.sha256(raw).hexdigest(), 'fixture_content_sha256': expected}
        self.assertEqual(driver.validate_dataset('stress-v1', access, docs), manifest)
        with self.assertRaisesRegex(RuntimeError, 'Explicit dataset'):
            driver.validate_dataset('gold-v1', access, docs)
        with self.assertRaisesRegex(RuntimeError, 'content or version'):
            driver.validate_dataset('stress-v1', dict(access, fixture_content_sha256='0' * 64), docs)
        manifest['generator']['lead_id_max'] = 7
        with self.assertRaisesRegex(RuntimeError, 'Unrecognized stress recipe'):
            driver.stress_content_hash(manifest)

    def test_semantic_dataset_is_pinned_and_rejects_undeclared_business_changes(self):
        docs = Path(driver.__file__).parent.parent / 'docs'
        for dataset, filename in [
            ('semantic-active-timestamp-v1', 'Agent-Semantic-Active-Timestamp-Fixture.json'),
            ('semantic-missing-timestamp-v1', 'Agent-Semantic-Missing-Timestamp-Fixture.json')]:
            raw = (docs / filename).read_bytes()
            manifest = json.loads(raw)
            access = {'dataset': dataset, 'fixture_version': manifest['version'],
                      'fixture_hash': hashlib.sha256(raw).hexdigest(),
                      'base_fixture_sha256': manifest['base_fixture_sha256']}
            self.assertEqual(driver.validate_dataset(dataset, access, docs), manifest)
            with self.assertRaises(RuntimeError):
                driver.validate_dataset('gold-v1', access, docs)
            with tempfile.TemporaryDirectory() as directory:
                altered_docs = Path(directory)
                (altered_docs / 'Agent-Evaluation-Fixture.json').write_bytes((docs / 'Agent-Evaluation-Fixture.json').read_bytes())
                for change in ['amount', 'expected', 'change_declaration', 'version']:
                    altered = json.loads(raw)
                    if change == 'amount': altered['tables']['leads'][0]['deal_amount'] = 1
                    if change == 'expected': altered['semantic_variant']['expected']['current_converted_by_status'] = 3
                    if change == 'change_declaration': altered['semantic_variant']['only_business_change']['lead_id'] = 1
                    if change == 'version': altered['version'] = 'another-version'
                    changed = json.dumps(altered).encode()
                    (altered_docs / filename).write_bytes(changed)
                    with self.assertRaises(RuntimeError, msg=change):
                        driver.validate_dataset(dataset, dict(access, fixture_hash=hashlib.sha256(changed).hexdigest()), altered_docs)

    def test_uses_actual_and_unknown_reservation_once(self):
        turn = settled_turn()
        unknown = {'reservation_id': 'reservation-two', 'has_usage': False,
                   'settle_result': 'unknown', 'reservation_micro': 120_000}
        turn['model_usage_events'].extend([unknown, dict(unknown)])
        self.assertEqual(driver.turn_accounted_micro(turn), 121_000)

    def test_missing_or_incomplete_usage_never_becomes_free(self):
        self.assertEqual(driver.turn_accounted_micro({'request_attempted': True}), 3_000_000)
        turn = settled_turn()
        turn['model_usage_complete'] = False
        self.assertEqual(driver.turn_accounted_micro(turn), 3_000_000)
        turn['model_usage_complete'] = True
        turn['model_usage_events'][0].pop('estimated_actual_micro')
        self.assertEqual(driver.turn_accounted_micro(turn), 3_000_000)
        self.assertEqual(driver.turn_accounted_micro({'request_id': 'prepared-not-attempted'}), 0)

    def test_complete_explicit_no_model_run_is_free_but_missing_usage_is_not(self):
        turn = {'run_id': 'direct', 'finished': True, 'status': 'completed',
                'model_usage_complete': True, 'model_usage_events': [], 'model_not_called': True}
        self.assertEqual(driver.turn_accounted_micro(turn), 0)
        for change in ({'model_not_called': False}, {'model_usage_complete': False}, {'status': 'failed'}):
            self.assertEqual(driver.turn_accounted_micro(dict(turn, **change)), 3_000_000)
        contradictory = dict(turn, model_usage_events=settled_turn()['model_usage_events'])
        self.assertEqual(driver.turn_accounted_micro(contradictory), 3_000_000)

    def test_never_hides_larger_known_actual_cost(self):
        turn = settled_turn(3_500_000)
        turn['status'] = 'failed'
        self.assertEqual(driver.turn_accounted_micro(turn), 3_500_000)
        turn['model_usage_events'][0].update(settle_result='conflict', reservation_micro=1_000_000)
        self.assertEqual(driver.turn_accounted_micro(turn), 3_500_000)

    def test_pending_request_ceiling_is_not_counted_twice(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'A01-1.json').write_text(json.dumps({'turns': [settled_turn(1_000_000)]}))
            pending = {'request_id': 'uncertain-post', 'request_attempted': True}
            (output / 'A02-1.json').write_text(json.dumps({'turns': [pending]}))
            self.assertEqual(driver.campaign_accounted_micro(output), 4_000_000)
            driver.check_new_run_allowance(output, pending, 4_000_000)
            with self.assertRaisesRegex(RuntimeError, 'allowance'):
                driver.check_new_run_allowance(output, pending, 3_999_999)

    def test_server_duration_accepts_go_nanos_and_not_poll_duration(self):
        self.assertEqual(driver.run_duration_seconds({
            'created_at': '2026-10-08T13:52:19.804471132+00:00',
            'finished_at': '2026-10-08T13:52:24.904471132Z'}), 5.1)
        self.assertEqual(driver.parse_run_time('2026-10-08T13:52:19.8+08:00').microsecond, 800_000)

    def test_exclusive_campaign_lock_rejects_second_process(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            with driver.campaign_lock(output):
                result = subprocess.run([sys.executable, '-c',
                    'from pathlib import Path; from agent_live_evaluate import campaign_lock; '
                    'lock=campaign_lock(Path(__import__("sys").argv[1])); lock.__enter__()', str(output)],
                    cwd=Path(driver.__file__).parent, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Another evaluation process', result.stderr)
            with driver.campaign_lock(output):
                pass


class CampaignRecoveryHTTPTests(unittest.TestCase):
    def run_driver(self, known_run):
        case = json.loads((Path(driver.__file__).parent.parent / 'docs/Agent-Evaluation-Cases.json').read_text())['cases'][0]
        fixture_bytes = (Path(driver.__file__).parent.parent / 'docs/Agent-Evaluation-Fixture.json').read_bytes()
        posts = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                if self.path == '/api/agent/settings':
                    payload = {'spent_micro': 999, 'reserved_micro': 0}
                elif self.path.endswith('/events'):
                    events = [
                        {'seq': 1, 'type': 'context_usage', 'data': {'request_bytes': 10000, 'segment': 1}},
                        {'seq': 2, 'type': 'provider_switch', 'data': {'from_model': 'primary', 'model': 'backup', 'reason': 'primary_unavailable'}},
                        {'type': 'model_usage', 'data': settled_turn()['model_usage_events'][0]},
                        {'type': 'done', 'data': {'status': 'completed'}}]
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(''.join('data: ' + json.dumps(event) + '\n\n' for event in events).encode())
                    return
                elif self.path == '/api/agent/runs/known-run':
                    payload = {'run': {'id': 'known-run', 'status': 'completed',
                                      'created_at': '2026-10-08T13:52:19.804471132Z',
                                      'finished_at': '2026-10-08T13:52:24.904471132Z'},
                               'messages': [], 'results': []}
                else:
                    self.send_error(404)
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps(payload).encode())

            def do_POST(self):
                posts.append(self.path)
                self.send_error(500, 'This recovery test must not issue a paid POST')

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                output = root / 'evidence'
                output.mkdir()
                access = root / 'fixture-access.json'
                access.write_text(json.dumps({'base_url': 'http://127.0.0.1:' + str(server.server_port),
                    'token': 'synthetic-isolated-token', 'model': 'fixture-only',
                    'fixture_hash': hashlib.sha256(fixture_bytes).hexdigest()}))
                access.chmod(0o600)
                access_value=json.loads(access.read_text())
                access_value['candidate_identity']={'code_revision':'fixture-recovery-test',**{key:'a'*64 for key in ('binary_sha256','frontend_sha256','rules_sha256','model_config_sha256')}}
                access.write_text(json.dumps(access_value))
                from agent_evaluation_campaign import freeze_campaign
                candidate_id = freeze_campaign(output, access_value, 'gold-v1', [Path(driver.__file__).parent.parent / 'docs' / 'Agent-Evaluation-Cases.json', Path(driver.__file__), Path(driver.__file__).with_name('agent_evaluation_campaign.py')])

                turn = {'question': case['question'], 'request_id': 'existing-request', 'request_attempted': True}
                if known_run:
                    turn['run_id'] = 'known-run'
                record = {'candidate_id':candidate_id, 'case': case, 'repeat': 1, 'turns': [turn], 'conversation_id': 'known-conversation', 'finished': False}
                (output / 'A01-1.json').write_text(json.dumps(record))
                completed = subprocess.run([sys.executable, str(Path(driver.__file__)),
                    '--fixture-access', str(access), '--output', str(output), '--cases', 'A01',
                    '--repeat', '1', '--max-spend-yuan', '0.01', '--allow-paid-requests'],
                    capture_output=True, text=True, timeout=10)
                saved = json.loads((output / 'A01-1.json').read_text())
                return completed, posts, saved
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)

    def test_known_run_recovers_read_only_and_retains_real_duration(self):
        result, posts, saved = self.run_driver(known_run=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(posts, [])
        self.assertEqual(saved['turns'][0]['duration_seconds'], 5.1)
        self.assertEqual(saved['turns'][0]['accounted_micro'], 1000)
        self.assertEqual([event['type'] for event in saved['turns'][0]['operational_events']],
                         ['context_usage', 'provider_switch', 'done'])
        self.assertFalse(saved.get('acceptance_passed', False))

    def test_uncertain_post_rechecks_allowance_before_idempotent_replay(self):
        result, posts, _ = self.run_driver(known_run=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(posts, [])
        self.assertIn('allowance', result.stderr)


if __name__ == '__main__':
    unittest.main()
