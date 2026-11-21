"""Run the deployment entry point in an isolated tree; no SSH or real build."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class DeploymentValidationTest(unittest.TestCase):
    def run_deploy(self, fail_build=False):
        with tempfile.TemporaryDirectory(prefix='kxone-deploy-test-') as directory:
            base = Path(directory)
            root = base / 'app'
            scripts = root / 'scripts'
            scripts.mkdir(parents=True)
            shutil.copy(Path(__file__).with_name('deploy_kxone.sh'), scripts)
            (root / 'web/dist').mkdir(parents=True)
            (root / '.gitignore').write_text('web/dist/\n')
            index = root / 'web/dist/index.html'
            index.write_text('STALE_DIST')
            for args in [('init', '-q'), ('add', '.'), ('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture')]:
                subprocess.run(['git', *args], cwd=root, check=True, capture_output=True)
            commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
            commands = base / 'commands'
            commands.mkdir()
            calls = base / 'calls'
            for name in ('go', 'npm', 'npx', 'ssh', 'scp', 'python3'):
                command = commands / name
                command.write_text('#!/bin/bash\n'
                    'printf "%s %s\\n" "$(basename "$0")" "$*" >> "$TEST_CALLS"\n'
                    'if [[ "$(basename "$0") $*" == "npm run build --prefix web" ]]; then\n'
                    '  [[ "$TEST_FAIL_BUILD" != true ]] || exit 42\n'
                    '  printf FRESH_DIST > web/dist/index.html\n'
                    'fi\n'
                    '[[ "$(basename "$0")" != ssh && "$(basename "$0")" != scp ]]\n')
                command.chmod(0o755)
            env = dict(os.environ, PATH=str(commands)+os.pathsep+os.environ['PATH'],
                TEST_CALLS=str(calls), TEST_FAIL_BUILD=str(fail_build).lower(),
                KXONE_SERVER_HOST='fixture.invalid', KXONE_SERVER_USER='fixture',
                KXONE_PUBLIC_URL='https://fixture.invalid', KXONE_DEPLOY_VALIDATE_ONLY='true',
                KXONE_DEPLOY_VALIDATED_COMMIT=commit)
            result = subprocess.run(['/bin/bash', str(scripts / 'deploy_kxone.sh')], cwd=root,
                env=env, text=True, capture_output=True, timeout=15)
            return result, index.read_text(), calls.read_text() if calls.exists() else ''

    def test_recorded_commit_does_not_bypass_checks_or_reuse_stale_dist(self):
        result, index, calls = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(index, 'FRESH_DIST')
        for command in ('go test ./... -count=1', 'go vet ./...', 'npm run lint --prefix web',
                        'npm test --prefix web -- --run', 'npm run build --prefix web', 'python3 scripts/test_frontend_release_baseline.py',
                        'npx playwright test --config playwright.regression.config.js'):
            self.assertIn(command, calls)
        self.assertNotIn('ssh ', calls)
        self.assertNotIn('scp ', calls)

    def test_failed_rebuild_is_rejected_even_for_a_recorded_commit(self):
        result, index, calls = self.run_deploy(fail_build=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(index, 'STALE_DIST')
        self.assertNotIn('ssh ', calls)
        self.assertNotIn('scp ', calls)


if __name__ == '__main__':
    unittest.main()
