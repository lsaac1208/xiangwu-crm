"""A deployed revision must identify the complete archive, including new files."""
import hashlib
import io
import pathlib
import subprocess
import tarfile
import tempfile
import unittest


class SourceIdentityTest(unittest.TestCase):
    def test_revision_tracks_untracked_archive_contents(self):
        source = pathlib.Path(__file__).with_name("deploy_kxone.sh").read_text()
        assignment = next(line for line in source.splitlines() if line.startswith("revision="))
        with tempfile.TemporaryDirectory() as directory:
            archive = pathlib.Path(directory) / "source.tgz"
            revisions = []
            for content in (b"first metric", b"changed metric"):
                with tarfile.open(archive, "w:gz") as tar:
                    member = tarfile.TarInfo("new_metric.go")
                    member.size = len(content)
                    tar.addfile(member, io.BytesIO(content))
                script = (
                    "git() { if [ \"$1\" = rev-parse ]; then printf '%s' "
                    + "a" * 40 + "; fi; }; " + assignment
                    + "; printf '%s' \"$revision\""
                )
                result = subprocess.run(
                    ["/bin/bash", "-c", script],
                    env={"PATH": "/usr/bin:/bin", "SRC_TGZ": str(archive)},
                    text=True, capture_output=True, check=True,
                )
                self.assertEqual(
                    result.stdout,
                    "a" * 40 + "-" + hashlib.sha256(archive.read_bytes()).hexdigest(),
                )
                revisions.append(result.stdout)
            self.assertNotEqual(*revisions)


if __name__ == "__main__":
    unittest.main()
