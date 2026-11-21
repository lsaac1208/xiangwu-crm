import unittest
from verify_release_binary import validate_build_info

class ReleaseBinaryTest(unittest.TestCase):
    def metadata(self, version='v1.14.52', extra=''):
        return '/release/bin/kxone-server: go1.26.10\n\tpath\tgithub.com/lsaac128/kxone/cmd/server\n\tdep\tgithub.com/mattn/go-sqlite3\t'+version+'\th1:example\n'+extra+'\tbuild\tCGO_ENABLED=1\n'
    def test_safe_pinned_binary(self):
        self.assertEqual(validate_build_info(self.metadata())['sqlite_driver'],'v1.14.52')
    def test_old_missing_or_replaced_driver_is_not_safe(self):
        for metadata in (self.metadata('v1.14.22'),self.metadata().replace('github.com/mattn/go-sqlite3','example.com/other'),self.metadata(extra='\t=>\t../unverified-driver\t(devel)\n'),self.metadata().replace('go1.26.10','go1.18'),self.metadata().replace('CGO_ENABLED=1','CGO_ENABLED=0')):
            with self.subTest(metadata=metadata), self.assertRaises(ValueError):
                validate_build_info(metadata)
    def test_system_linked_sqlite_cannot_claim_embedded_fix(self):
        for tags in ('-tags=libsqlite3','-tags=sqlite_fts5,libsqlite3','-tags="sqlite_fts5 libsqlite3"'):
            with self.subTest(tags=tags), self.assertRaises(ValueError):
                validate_build_info(self.metadata(extra='\tbuild\t'+tags+'\n'))

if __name__ == '__main__': unittest.main()
