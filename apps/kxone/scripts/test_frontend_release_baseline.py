"""Exercise real frontend release metadata and binary verifier in temporary releases."""
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('verify_release', SCRIPTS / 'verify_release_binary.py')
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class FrontendBaselineTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.go = shutil.which('go')
        if not cls.go:
            raise unittest.SkipTest('Go toolchain required for real release binary test')
        cls.build = tempfile.TemporaryDirectory()
        source = Path(cls.build.name) / 'main.go'
        source.write_text('package main\nimport _ "github.com/mattn/go-sqlite3"\nfunc main(){}\n')
        cls.binary = Path(cls.build.name) / 'fixture'
        subprocess.run([cls.go, 'build', '-o', str(cls.binary), str(source)], cwd=SCRIPTS.parent, check=True, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        if hasattr(cls, 'build'):
            cls.build.cleanup()

    def test_frontend_and_rollback_keep_verified_session_baseline(self):
        script = (SCRIPTS / 'deploy_frontend_remote.sh').read_text()
        start = script.index('for destination in "$release" "$rollback_release"; do')
        end = script.index('\ndone', start) + len('\ndone')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            previous = root / 'previous'
            for name in ('bin', 'web', 'src', 'evidence'):
                (previous / name).mkdir(parents=True)
            for name in ('kxone-server', 'kxone-worker'):
                shutil.copy2(self.binary, previous / 'bin' / name)
            shutil.copy2(SCRIPTS / 'verify_release_binary.py', previous / 'src/verify_release_binary.py')
            (previous / 'SECURITY_BASELINE').write_text('verified\n')
            (previous / 'SESSION_SECURITY_BASELINE').write_text('revocable-sid-v1\n')
            verify.inspect(self.go, previous, True)
            release, rollback = root / 'frontend', root / 'rollback'
            for target in (release, rollback):
                shutil.copytree(previous, target, ignore=shutil.ignore_patterns('*BASELINE'))
                (target / 'src/scripts').mkdir()
                shutil.copy2(SCRIPTS / 'verify_release_binary.py', target / 'src/scripts/verify_release_binary.py')
            harness = 'set -euo pipefail\nprevious=$1; release=$2; rollback_release=$3; go_binary=$4\nchown(){ :; }\n' + script[start:end]
            result = subprocess.run(['bash', '-c', harness, '--', str(previous), str(release), str(rollback), self.go], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            for target in (release, rollback):
                self.assertEqual((target / 'SESSION_SECURITY_BASELINE').read_text(), 'revocable-sid-v1\n')
                verify.inspect(self.go, target, True)

            # Exercise the subsequent full release failure with the actual
            # verified frontend predecessor. Only service commands are recorded;
            # symlink replacement uses the OS primitive for macOS/Linux parity.
            candidate = root / 'full-candidate'
            shutil.copytree(previous, candidate)
            (candidate / 'src/scripts').mkdir()
            shutil.copy2(SCRIPTS / 'verify_release_binary.py', candidate / 'src/scripts/verify_release_binary.py')
            (root / 'current').symlink_to(candidate)
            full = (SCRIPTS / 'deploy_remote.sh').read_text()
            start = full.index('failure() {\n')
            end = full.index('\ntrap failure EXIT', start)
            failure = full[start:end]
            commands = root / 'service-commands'
            harness = """set -Eeuo pipefail
previous=$1; release=$2; remote_root=$3; go_binary=$4; command_log=$5
backup=synthetic-backup; switched=true; finished=false; previous_safe=false
if python3 "$release/src/scripts/verify_release_binary.py" "$go_binary" "$previous" --require-security-baseline >/dev/null; then previous_safe=true; fi
systemctl() { printf '%s\\n' "$*" >> "$command_log"; }
mv() { python3 -c 'import os,sys; os.replace(sys.argv[1],sys.argv[2])' "$2" "$3"; }
readlink() { python3 -c 'import pathlib,sys; print(pathlib.Path(sys.argv[1]).resolve())' "$2"; }
""" + failure + '\ntrap failure EXIT\nexit 73\n'
            result = subprocess.run(['bash', '-c', harness, '--', str(release), str(candidate), str(root), self.go, str(commands)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 73, result.stderr)
            self.assertEqual((root / 'current').resolve(), release.resolve())
            self.assertIn('restart kxone-server kxone-worker', commands.read_text())
            self.assertNotIn('stop kxone-server kxone-worker', commands.read_text())


if __name__ == '__main__':
    unittest.main()
