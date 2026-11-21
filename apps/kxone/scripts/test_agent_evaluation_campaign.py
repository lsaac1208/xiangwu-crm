import copy,json,tempfile,unittest
from pathlib import Path
from agent_evaluation_campaign import freeze_campaign,recorded_metric_spend,save_record,collect_paid_turn
from agent_live_evaluate import RUN_CEILING_MICRO

class CampaignIdentityTests(unittest.TestCase):
    def test_rejects_changed_binary_rules_frontend_model_data_and_expectations(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);output=root/'evidence';question=root/'cases.json';question.write_text('{"expected":4}')
            access={'fixture_hash':'fixture-a','candidate_identity':{'code_revision':'revision-a',**{k:'a'*64 for k in ('binary_sha256','frontend_sha256','rules_sha256','model_config_sha256')}}}
            first=freeze_campaign(output,access,'gold-v1',[question]);self.assertEqual(first,freeze_campaign(output,access,'gold-v1',[question]))
            for key in access['candidate_identity']:
                changed=copy.deepcopy(access);changed['candidate_identity'][key]='b'*64
                with self.assertRaises(RuntimeError):freeze_campaign(output,changed,'gold-v1',[question])
            changed=copy.deepcopy(access);changed['fixture_hash']='fixture-b'
            with self.assertRaises(RuntimeError):freeze_campaign(output,changed,'gold-v1',[question])
            question.write_text('{"expected":5}')
            with self.assertRaises(RuntimeError):freeze_campaign(output,access,'gold-v1',[question])
    def test_rejects_a_foreign_pass_inside_an_already_bound_campaign(self):
        with tempfile.TemporaryDirectory() as d:
            output=Path(d)
            access={'fixture_hash':'a','candidate_identity':{'code_revision':'a',**{k:'a'*64 for k in ('binary_sha256','frontend_sha256','rules_sha256','model_config_sha256')}}}
            candidate=freeze_campaign(output,access,'gold-v1',[])
            path=output/'M31-1.json';path.write_text(json.dumps({'candidate_id':'foreign','passed':True,'micro':0}))
            with self.assertRaisesRegex(RuntimeError,'another or unbound candidate'):freeze_campaign(output,access,'gold-v1',[])
            path.write_text(json.dumps({'candidate_id':candidate,'passed':True,'micro':0}))
            self.assertEqual(freeze_campaign(output,access,'gold-v1',[]),candidate)
    def test_daily_costs_and_failed_runs_survive_resume(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d);(p/'M08-1.json').write_text(json.dumps({'passed':True,'micro':10}));(p/'D01-1.json').write_text(json.dumps({'passed':False,'micro':20}));(p/'candidate.json').write_text('{}')
            self.assertEqual(recorded_metric_spend(p),30)
    def test_incomplete_identity_and_legacy_passes_cannot_bypass_gate(self):
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(RuntimeError):freeze_campaign(d,{},'gold-v1',[])

    def test_lost_post_response_resumes_same_request_and_preserves_unknown_cost(self):
        with tempfile.TemporaryDirectory() as d:
            output=Path(d);path=output/'M31-1.json'
            record={'conversation_id':'conversation','turns':[{'question':'question','request_id':'saved-request'}]}
            save_record(path,record)
            calls=[]
            def lost(path,method='GET',body=None,raw=False):
                calls.append(body['request_id'])
                self.assertTrue(json.loads((output/'M31-1.json').read_text())['turns'][0]['request_attempted'])
                self.assertEqual(recorded_metric_spend(output),RUN_CEILING_MICRO)
                raise OSError('response lost after server accepted POST')
            with self.assertRaises(OSError):collect_paid_turn(output,path,record,record['turns'][0],lost,10_000_000)
            resumed=json.loads(path.read_text())
            def recovered(path,method='GET',body=None,raw=False):
                if method=='POST':calls.append(body['request_id']);return {'id':'same-run'}
                if raw:return 'data: '+json.dumps({'type':'model_usage','data':{'reservation_id':'r','has_usage':True,'settle_result':'settled','estimated_actual_micro':0}})+'\ndata: '+json.dumps({'type':'done','data':{}})
                return {'run':{'status':'completed'},'messages':[],'results':[]}
            collect_paid_turn(output,path,resumed,resumed['turns'][0],recovered,10_000_000)
            self.assertEqual(calls,['saved-request','saved-request'])
            self.assertEqual(recorded_metric_spend(output),0)

    def test_events_failure_resumes_get_only_and_keeps_failed_settlement_reserve(self):
        with tempfile.TemporaryDirectory() as d:
            output=Path(d);path=output/'D01-1.json'
            record={'conversation_id':'c','turns':[{'question':'q','request_id':'request','run_id':'known-run','request_attempted':True}]}
            save_record(path,record)
            def interrupted(path,method='GET',body=None,raw=False):
                self.assertEqual(method,'GET')
                if raw:raise OSError('event stream interrupted')
                return {'run':{'status':'completed'}}
            with self.assertRaises(OSError):collect_paid_turn(output,path,record,record['turns'][0],interrupted,10_000_000)
            self.assertEqual(recorded_metric_spend(output),RUN_CEILING_MICRO)
            def recovered(path,method='GET',body=None,raw=False):
                self.assertEqual(method,'GET')
                if raw:return 'data: '+json.dumps({'type':'model_usage','data':{'reservation_id':'r','has_usage':True,'settle_result':'failed','reservation_micro':2000,'estimated_actual_micro':1000}})+'\ndata: '+json.dumps({'type':'done','data':{}})
                return {'run':{'status':'completed'},'messages':[],'results':[]}
            collect_paid_turn(output,path,record,record['turns'][0],recovered,10_000_000)
            self.assertEqual(recorded_metric_spend(output),2000)

    def test_full_ceiling_is_required_before_a_new_attempt(self):
        with tempfile.TemporaryDirectory() as d:
            output=Path(d);path=output/'M31-1.json'
            record={'conversation_id':'c','turns':[{'question':'q','request_id':'r'}]};save_record(path,record)
            def forbidden(*a,**kw):self.fail('network called without allowance')
            with self.assertRaisesRegex(RuntimeError,'ceiling'):collect_paid_turn(output,path,record,record['turns'][0],forbidden,100)
            self.assertEqual(recorded_metric_spend(output),0)

    def test_no_model_evidence_is_collected_before_zero_cost(self):
        with tempfile.TemporaryDirectory() as d:
            output=Path(d);path=output/'M31-1.json'
            record={'conversation_id':'c','turns':[{'question':'q','request_id':'r','run_id':'direct'}]}
            def completed(path,method='GET',body=None,raw=False):
                self.assertEqual(method,'GET')
                if raw:
                    return '\n'.join('data: '+json.dumps(e) for e in [
                        {'type':'model_skipped','data':{'reason':'not_requested'}},
                        {'type':'done','data':{}}])
                return {'run':{'status':'completed'},'messages':[],'results':[]}
            collect_paid_turn(output,path,record,record['turns'][0],completed,10_000_000)
            self.assertTrue(record['turns'][0]['model_not_called'])
            self.assertEqual(recorded_metric_spend(output),0)
if __name__=='__main__':unittest.main()
