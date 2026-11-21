#!/usr/bin/env python3
"""Render one incremental CRM HTTPS include without replacing live settings."""
import argparse
from pathlib import Path
import re

INCLUDE = 'include /etc/nginx/snippets/kxone-static-assets.conf;'


def configure(text):
    tokens = re.finditer(r'''"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\#[^\n]*|[{};]|[^\s{};"'\#]+''', text)
    stack, header, servers = [], [], []
    for token in tokens:
        word = token.group()
        if word.startswith('#'):
            continue
        if word == '{':
            stack.append((header[0][0] if header else '', token.start()))
            header = []
        elif word == '}':
            if not stack:
                raise ValueError('unbalanced configuration')
            kind, start = stack.pop()
            if kind == 'server' and not stack:
                servers.append((start, token.start()))
            header = []
        elif word == ';':
            header = []
        else:
            header.append((word, token.start()))
    if stack:
        raise ValueError('unbalanced configuration')
    candidates = []
    for start, end in servers:
        body = text[start:end]
        if re.search(r'(?m)^\s*server_name\s+[^;]*\bcrm\.example\.com(?:\s|;)', body) and re.search(r'(?m)^\s*listen\s+(?:\S+:)?443\b', body):
            candidates.append((start, end))
    if len(candidates) != 1:
        raise ValueError('exactly one CRM HTTPS server required')
    start, end = candidates[0]
    body = text[start:end]
    include_pattern = r'(?m)^\s*' + re.escape(INCLUDE)
    if re.search(include_pattern, body):
        if len(re.findall(include_pattern, text)) != 1:
            raise ValueError('duplicate static include')
        return text
    if re.search(include_pattern, text) or re.search(r'(?m)^\s*location\s+[^\n{]*\/assets', body):
        raise ValueError('existing static configuration needs explicit review')
    return text[:end] + '\n    ' + INCLUDE + '\n' + text[end:]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.input.resolve() == args.output.resolve():
        parser.error('output must be a separate candidate file')
    args.output.write_text(configure(args.input.read_text()))


if __name__ == '__main__':
    main()
