import unittest
from release_state import verify

class ReleaseStateTest(unittest.TestCase):
    def test_normal_business_writes_do_not_fail_deployment(self):
        before = dict(inode=123, flags={'appointment.enabled':'false'}, lead_count=100, lead_max_id=110, pending=['000026'])
        after = dict(before, lead_count=105, lead_max_id=116, pending=[])
        self.assertEqual(verify(before,after)['lead_count_delta'],5)
    def test_database_replacement_flag_change_and_pending_migration_fail(self):
        before = dict(inode=123, flags={'appointment.enabled':'false'}, lead_count=100, lead_max_id=110, pending=[])
        for change in [dict(inode=999),dict(flags={'appointment.enabled':'true'}),dict(pending=['000026'])]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                verify(before,dict(before,**change))
    def test_agent_switch_cannot_change_silently(self):
        before = dict(inode=123, flags={}, lead_count=1, lead_max_id=1, pending=[], agent_enabled=False)
        self.assertEqual(verify(before,dict(before))['lead_count_delta'],0)
        for enabled in (True,None):
            with self.subTest(enabled=enabled), self.assertRaises(ValueError):
                verify(before,dict(before,agent_enabled=enabled))
    def test_first_agent_structure_is_disabled(self):
        before = dict(inode=123, flags={}, lead_count=1, lead_max_id=1, pending=[], agent_enabled=None)
        self.assertEqual(verify(before,dict(before,agent_enabled=False))['lead_count_delta'],0)
        with self.assertRaises(ValueError):
            verify(before,dict(before,agent_enabled=True))

if __name__ == '__main__': unittest.main()
