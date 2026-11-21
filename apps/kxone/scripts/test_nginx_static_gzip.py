import unittest

import nginx_static_gzip


class StaticGzipConfigTest(unittest.TestCase):
    source = '''log_format safe '{"quoted":"brace }"}';
server {
    listen 80;
    server_name crm.example.com;
    location / { return 301 https://crm.example.com$request_uri; }
}
server {
    listen 443 ssl;
    server_name another.example;
    location / { try_files $uri /index.html; }
}
server {
    listen 443 ssl;
    server_name crm.example.com;
    add_header Content-Security-Policy "default-src 'self'" always;
    location /api/events { gzip off; proxy_buffering off; }
    location / { try_files $uri /index.html; }
}
'''

    def test_only_adds_the_static_include_to_crm_https_and_is_idempotent(self):
        output = nginx_static_gzip.configure(self.source)
        insertion = '\n    include /etc/nginx/snippets/kxone-static-assets.conf;\n'
        self.assertEqual(output.replace(insertion, '', 1), self.source)
        self.assertEqual(output.count(insertion), 1)
        self.assertGreater(output.index(insertion), output.rindex('server_name crm.example.com'))
        self.assertEqual(nginx_static_gzip.configure(output), output)

    def test_ambiguous_or_existing_asset_configuration_is_rejected(self):
        with self.assertRaises(ValueError):
            nginx_static_gzip.configure(self.source + self.source)
        with self.assertRaises(ValueError):
            nginx_static_gzip.configure(self.source.replace('location /api/events', 'location /assets/'))

    def test_commented_include_does_not_disable_configuration(self):
        source = self.source.replace('    listen 443 ssl;', '    # ' + nginx_static_gzip.INCLUDE + '\n    listen 443 ssl;')
        output = nginx_static_gzip.configure(source)
        self.assertIn('\n    ' + nginx_static_gzip.INCLUDE + '\n}', output)


if __name__ == '__main__':
    unittest.main()
