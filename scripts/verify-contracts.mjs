#!/usr/bin/env node
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const read = (path) => readFile(resolve(root, path), 'utf8');
const schema = JSON.parse(await read('contracts/lead-gateway-v1.schema.json'));
const kxoneDTO = await read('apps/kxone/internal/transport/http/gateway_lead_handler.go');

assert.equal(schema.$schema, 'https://json-schema.org/draft/2020-12/schema');
assert.equal(schema.properties?.schemaVersion?.const, 1);
assert.deepEqual(schema.properties?.source?.enum, ['baidu', 'tencent', 'xiaohongshu', 'form', 'manual']);
for (const field of ['schemaVersion', 'source', 'phone', 'leadCreatedAt']) {
  assert(schema.required.includes(field), `lead schema must require ${field}`);
}
assert.match(kxoneDTO, /SchemaVersion\s+int\s+`json:"schemaVersion"`/);

console.log('CRM inbound gateway contract v1 verified; external clients are outside this repository');
