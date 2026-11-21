"""Run the real rollback trap with shell functions replacing every side effect."""
from pathlib import Path
import subprocess
import unittest


class RemoteFailureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = Path(__file__).with_name('deploy_remote.sh').read_text()
        start = source.index('failure() {\n')
        end = source.index('\ntrap failure EXIT', start)
        cls.failure_function = source[start:end]
        cls.shell_options = next(line for line in source.splitlines() if line.startswith('set -'))

    def run_failure(self, phase):
        # PATH is empty and all commands in the trap are shell functions. This
        # cannot invoke systemctl, touch links, inspect binaries, or use SSH.
        harness = self.shell_options + r'''
exec 3>&1
phase=$1
remote_root=/unit-only
release=/unit-only/candidate
previous=/unit-only/previous
backup=/unit-only/backup.db
go_binary=/unit-only/go
current_release=$release
finished=false
switched=true
previous_safe=true
[[ "$phase" != no_baseline ]] || previous_safe=false
[[ "$phase" != before_switch ]] || switched=false
record() { printf '%s\n' "$*" >&3; }
python3() { record verify; [[ "$phase" != verify ]]; }
ln() { record ln; [[ "$phase" != ln ]] || return 2; next_link=$2; }
mv() { record mv; [[ "$phase" != mv ]] || return 3; current_release=$next_link; }
systemctl() {
  record "systemctl $*"
  case "$1" in
    restart) [[ "$phase" != restart ]] ;;
    stop) return 0 ;;
    is-active) printf '%s\n' 'simulated service state'; return 3 ;;
    *) return 127 ;;
  esac
}
readlink() { printf '%s\n' "$current_release"; }
''' + self.failure_function + '\ntrap failure EXIT\nexit 42\n'
        return subprocess.run(['/bin/bash', '-c', harness, '--', phase],
                              env={'PATH': ''}, text=True, capture_output=True,
                              timeout=5, check=False)

    def test_rollback_failure_paths_stop_and_still_report(self):
        expected = {
            'ln': ['verify', 'ln'],
            'mv': ['verify', 'ln', 'mv'],
            'restart': ['verify', 'ln', 'mv', 'systemctl restart kxone-server kxone-worker'],
            'verify': ['verify'],
            'no_baseline': [],
        }
        for phase, calls in expected.items():
            with self.subTest(phase=phase):
                result = self.run_failure(phase)
                self.assertEqual(result.returncode, 42)
                self.assertEqual(result.stdout.splitlines(), calls + [
                    'systemctl stop kxone-server kxone-worker',
                    'systemctl is-active kxone-server kxone-worker',
                ])
                target = 'previous' if phase == 'restart' else 'candidate'
                self.assertIn('deployment_failed=42 current_release=/unit-only/' + target,
                              result.stderr)
                self.assertIn('database_backup=/unit-only/backup.db', result.stderr)
                self.assertIn('simulated service state', result.stderr)
                self.assertEqual('No verified secure rollback' in result.stderr,
                                 phase in ('verify', 'no_baseline'))

    def test_successful_rollback_restarts_without_stopping_and_reports(self):
        result = self.run_failure('success')
        self.assertEqual(result.returncode, 42)
        self.assertEqual(result.stdout.splitlines(), [
            'verify', 'ln', 'mv', 'systemctl restart kxone-server kxone-worker',
            'systemctl is-active kxone-server kxone-worker',
        ])
        self.assertIn('deployment_failed=42 current_release=/unit-only/previous',
                      result.stderr)
        self.assertIn('simulated service state', result.stderr)

    def test_failure_before_switch_does_not_touch_running_services(self):
        result = self.run_failure('before_switch')
        self.assertEqual(result.returncode, 42)
        self.assertEqual(result.stdout.splitlines(), [
            'systemctl is-active kxone-server kxone-worker',
        ])
        self.assertIn('deployment_failed=42 current_release=/unit-only/candidate',
                      result.stderr)


if __name__ == '__main__':
    unittest.main()
