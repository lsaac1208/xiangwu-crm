import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash, createHmac } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const gatewayPackageJSON = process.env.GATEWAY_PACKAGE_JSON ?? '/app/package.json';
const require = createRequire(gatewayPackageJSON);
const Database = require('better-sqlite3');

const mode = process.env.E2E_MODE ?? 'quick';
assert.ok(['quick', 'nightly'].includes(mode), `unsupported E2E_MODE: ${mode}`);

const kxoneURL = process.env.KXONE_URL ?? 'http://kxone-server:8080';
const gatewayURL = process.env.GATEWAY_URL ?? 'http://gateway:3000';
const fakeWeComURL = process.env.FAKE_WECOM_URL ?? 'http://fake-wecom:8088';
const kxoneDBPath = process.env.KXONE_DB_PATH ?? '/e2e-data/kxone/kxone-e2e.db';
const gatewayDBPath = process.env.GATEWAY_DB_PATH ?? '/e2e-data/gateway/gateway-e2e.db';
const gatewayReplayScript = process.env.GATEWAY_REPLAY_SCRIPT ?? '/app/dist/scripts/replay-kxone-failures.js';
const gatewayAppDir = process.env.GATEWAY_APP_DIR ?? '/app';
const reportPath = process.env.E2E_REPORT_PATH ?? '/tmp/e2e-report.json';
const gatewayHMAC = process.env.KXONE_GATEWAY_HMAC_SECRET ?? '';

const primaryAccount = 'e2e-tencent-account';
const disabledAccount = 'e2e-disabled-account';
const fakePhone = '00000000000';
const recipients = ['e2e-admin', 'e2e-manager'];

const report = {
  mode,
  startedAt: new Date().toISOString(),
  finishedAt: null,
  status: 'running',
  checks: [],
};

let kxoneDB;
let gatewayDB;

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, {
    ...options,
    headers: {
      accept: 'application/json',
      ...(options.body ? { 'content-type': 'application/json' } : {}),
      ...options.headers,
    },
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      throw new Error(`non-JSON response from ${new URL(url).pathname}: HTTP ${response.status}`);
    }
  }
  return { response, payload };
}

async function waitFor(label, probe, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const result = await probe();
      if (result) return result;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`${label} did not become ready${lastError ? `: ${lastError.message}` : ''}`);
}

async function check(name, operation) {
  const startedAt = Date.now();
  await operation();
  report.checks.push({ name, status: 'passed', durationMs: Date.now() - startedAt });
}

function openDatabase(path) {
  const database = new Database(path);
  database.pragma('busy_timeout = 5000');
  database.pragma('journal_mode = WAL');
  return database;
}

function upsertChannel(database, accountName, enabled) {
  const now = new Date().toISOString();
  database.prepare(`
    INSERT INTO channel_integrations (
      platform, account_name, callback_path, auth_mode, secret_configured, enabled,
      created_by, updated_by, created_at, updated_at
    ) VALUES (
      'tencent', @accountName, '/api/leads/tencent', 'platform_compatible', 0, @enabled,
      'e2e-runner', 'e2e-runner', @now, @now
    )
    ON CONFLICT(platform, account_name) DO UPDATE SET
      enabled = excluded.enabled,
      updated_by = excluded.updated_by,
      updated_at = excluded.updated_at
  `).run({ accountName, enabled: enabled ? 1 : 0, now });
}

function upsertBinding(database, { crmUserID, wecomUserID, employeeName, role }) {
  const now = new Date().toISOString();
  database.prepare(`
    INSERT INTO crm_wecom_user_bindings (
      crm_user_id, wecom_userid, employee_name, role, department_id, status, created_at, updated_at
    ) VALUES (
      @crmUserID, @wecomUserID, @employeeName, @role, 1, 'active', @now, @now
    )
    ON CONFLICT(wecom_userid) DO UPDATE SET
      employee_name = excluded.employee_name,
      role = excluded.role,
      status = excluded.status,
      updated_at = excluded.updated_at
  `).run({ crmUserID, wecomUserID, employeeName, role, now });
}

function upsertSetting(database, key, value, description, group = 'notification') {
  const now = new Date().toISOString();
  database.prepare(`
    INSERT INTO system_settings (
      setting_key, setting_value, value_type, setting_group, description,
      is_sensitive, updated_by, created_at, updated_at
    ) VALUES (
      @key, @value, 'string', @group, @description,
      0, 'e2e-runner', @now, @now
    )
    ON CONFLICT(setting_key) DO UPDATE SET
      setting_value = excluded.setting_value,
      value_type = excluded.value_type,
      setting_group = excluded.setting_group,
      description = excluded.description,
      is_sensitive = excluded.is_sensitive,
      updated_by = excluded.updated_by,
      updated_at = excluded.updated_at
  `).run({ key, value, group, description, now });
}

function seedKXOne(database) {
  upsertChannel(database, primaryAccount, true);
  upsertChannel(database, disabledAccount, false);
  upsertBinding(database, {
    crmUserID: 9001,
    wecomUserID: 'e2e-admin',
    employeeName: 'E2E Admin',
    role: 'admin',
  });
  upsertBinding(database, {
    crmUserID: 9002,
    wecomUserID: 'e2e-manager',
    employeeName: 'E2E Manager',
    role: 'manager',
  });
  upsertSetting(database, 'notification.group_robot_enabled', 'true', 'E2E-only group robot switch');
  upsertSetting(
    database,
    'notification.group_robot_webhook_url',
    `${fakeWeComURL}/cgi-bin/webhook/send?key=e2e-only`,
    'E2E-only fake group robot endpoint',
  );
  upsertSetting(database, 'card.retry_seconds', '1,1,1', 'E2E-only card retry delays', 'business');
}

function payloadFor(identity, overrides = {}) {
  return {
    leads_id: Number(identity),
    leads_name: `E2E Synthetic Lead ${identity}`,
    leads_tel: fakePhone,
    leads_wechat: 'e2e-wechat',
    leads_tags: 'e2e-only',
    tel_location: 'E2E Region',
    campaign_name: 'E2E Campaign',
    adgroup_name: 'E2E Ad Group',
    creative_name: 'E2E Creative',
    component_name: 'E2E Form',
    page_name: 'E2E Page',
    page_url: 'http://example.invalid/e2e',
    leads_create_time: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function leadNo(identity) {
  return `tencent:${identity}`;
}

function gatewayEndpoint(accountName = primaryAccount) {
  const endpoint = new URL('/api/leads/tencent', gatewayURL);
  endpoint.searchParams.set('account', accountName);
  return endpoint;
}

async function postGatewayLead(identity, overrides = {}, accountName = primaryAccount) {
  return fetchJSON(gatewayEndpoint(accountName), {
    method: 'POST',
    body: JSON.stringify(payloadFor(identity, overrides)),
  });
}

async function fakeRequests() {
  const result = await fetchJSON(`${fakeWeComURL}/_test/requests`);
  assert.equal(result.response.status, 200);
  return result.payload?.requests ?? [];
}

async function setFakeState(state) {
  const result = await fetchJSON(`${fakeWeComURL}/_test/state`, {
    method: 'POST',
    body: JSON.stringify(state),
  });
  assert.equal(result.response.status, 200);
  return result.payload?.faults;
}

function requestCounts(requests) {
  return {
    group: requests.filter((item) => item.kind === 'group-webhook').length,
    message: requests.filter((item) => item.kind === 'message-send').length,
    proxy: requests.filter((item) => item.kind === 'kxone-proxy').length,
  };
}

function countDelta(after, before, key) {
  return requestCounts(after)[key] - requestCounts(before)[key];
}

function queryLead(no) {
  return kxoneDB.prepare(`
    SELECT id, lead_no, customer_mobile, customer_mobile_masked, customer_mobile_hash,
           source_channel, source_campaign, status, dispatch_manager_user_id
    FROM leads WHERE lead_no = ?
  `).get(no);
}

async function waitForLead(no) {
  return waitFor(`KXOne lead ${no}`, () => queryLead(no));
}

function assertProtectedLead(lead) {
  assert.match(lead.customer_mobile, /^enc:v1:/);
  assert.equal(lead.customer_mobile.includes(fakePhone), false);
  assert.equal(lead.customer_mobile_masked, '000****0000');
  assert.match(lead.customer_mobile_hash, /^hmac:v1:[a-f0-9]{64}$/);
  assert.equal(lead.customer_mobile_hash.includes(fakePhone), false);
}

async function waitForSuccessfulCards(leadID) {
  return waitFor(`two successful cards for lead ${leadID}`, () => {
    const rows = kxoneDB.prepare(`
      SELECT receiver_user_id, card_scene, card_status, retry_count, msg_id,
             response_code, next_retry_at
      FROM wecom_card_tasks
      WHERE lead_id = ?
      ORDER BY receiver_user_id, id
    `).all(leadID);
    if (rows.length !== 2 || rows.some((row) => row.card_status !== 'sent')) return null;
    return rows;
  });
}

function assertSuccessfulCards(rows) {
  assert.deepEqual(rows.map((row) => row.receiver_user_id).sort(), recipients);
  for (const row of rows) {
    assert.equal(row.card_scene, 'supervisor_pending_dispatch');
    assert.equal(row.card_status, 'sent');
    assert.equal(row.retry_count, 0);
    assert.ok(row.msg_id);
    assert.ok(row.response_code);
    assert.equal(row.next_retry_at, null);
  }
}

function gatewayDelivery(no) {
  return gatewayDB.prepare(`
    SELECT status, attempt_count, payload_json, payload_sha256, last_error,
           kxone_lead_id, kxone_lead_no, first_attempt_at, delivered_at
    FROM kxone_forward_deliveries WHERE dedupe_key = ?
  `).get(no);
}

async function runReplay() {
  const { stdout } = await execFileAsync(
    process.execPath,
    [gatewayReplayScript, '--apply', '--limit', '100'],
    { cwd: gatewayAppDir, env: process.env, timeout: 30_000, maxBuffer: 1024 * 1024 },
  );
  const matches = stdout.match(/\{\s*"mode":\s*"apply"[\s\S]*?"invalid":\s*\d+\s*\}/g);
  assert.ok(matches?.length, 'replay command did not emit its JSON report');
  return JSON.parse(matches.at(-1));
}

async function signedKXOneRequest(body) {
  assert.ok(gatewayHMAC, 'KXONE_GATEWAY_HMAC_SECRET is required by the E2E runner');
  const raw = JSON.stringify(body);
  const timestamp = String(Math.floor(Date.now() / 1000));
  const signature = createHmac('sha256', gatewayHMAC).update(`${timestamp}.${raw}`).digest('hex');
  return fetchJSON(`${kxoneURL}/api/internal/leads/gateway`, {
    method: 'POST',
    body: raw,
    headers: {
      'x-kxone-gateway-timestamp': timestamp,
      'x-kxone-gateway-signature': signature,
    },
  });
}

async function runQuickPath() {
  const identity = '910000001';
  const no = leadNo(identity);
  let lead;

  await check('KXOne rejects missing and invalid Gateway HMAC signatures', async () => {
    const endpoint = `${kxoneURL}/api/internal/leads/gateway`;
    const unsigned = await fetchJSON(endpoint, { method: 'POST', body: JSON.stringify({}) });
    assert.equal(unsigned.response.status, 401);
    assert.equal(unsigned.payload?.error, 'invalid_gateway_signature');

    const invalid = await fetchJSON(endpoint, {
      method: 'POST',
      body: JSON.stringify({}),
      headers: {
        'x-kxone-gateway-timestamp': String(Math.floor(Date.now() / 1000)),
        'x-kxone-gateway-signature': 'invalid-e2e-signature',
      },
    });
    assert.equal(invalid.response.status, 401);
    assert.equal(invalid.payload?.error, 'invalid_gateway_signature');
  });

  const requestsBefore = await fakeRequests();
  await check('Tencent callback reaches KXOne with normalized and encrypted fields', async () => {
    const first = await postGatewayLead(identity);
    assert.equal(first.response.status, 200);
    assert.equal(first.payload?.ok, true);
    assert.equal(first.payload?.duplicated, false);
    assert.equal(first.payload?.lead_no, no);
    assert.equal(typeof first.payload?.lead_id, 'number');

    lead = await waitForLead(no);
    assert.equal(lead.id, first.payload.lead_id);
    assertProtectedLead(lead);
    assert.equal(lead.source_channel, `ad:tencent:${primaryAccount}`);
    assert.equal(lead.status, 'pending_dispatch');
    assert.equal(lead.dispatch_manager_user_id, 'e2e-manager');
    assert.match(lead.source_campaign, /\u8ba1\u5212:E2E Campaign/);
    assert.match(lead.source_campaign, /\u5e7f\u544a\u7ec4:E2E Ad Group/);

    const channel = kxoneDB.prepare(`
      SELECT last_status, last_lead_id FROM channel_integrations
      WHERE platform = 'tencent' AND account_name = ?
    `).get(primaryAccount);
    assert.equal(channel.last_status, 'accepted');
    assert.equal(channel.last_lead_id, lead.id);
  });

  await check('admin, manager, and group robot each receive one new-lead notification', async () => {
    const cards = await waitForSuccessfulCards(lead.id);
    assertSuccessfulCards(cards);

    const requests = await waitFor('all primary fake WeCom notifications', async () => {
      const items = await fakeRequests();
      return countDelta(items, requestsBefore, 'group') === 1 &&
        countDelta(items, requestsBefore, 'message') === 2 ? items : null;
    });
    const newRequests = requests.slice(requestsBefore.length);
    const group = newRequests.find((item) => item.kind === 'group-webhook');
    const messages = newRequests.filter((item) => item.kind === 'message-send');
    assert.equal(group.body?.msgtype, 'markdown');
    assert.match(group.body?.markdown?.content ?? '', /E2E Synthetic Lead 910000001/);
    assert.deepEqual(messages.map((item) => item.body?.touser).sort(), recipients);
    assert.ok(messages.every((item) => item.body?.msgtype === 'template_card'));
  });

  const beforeDuplicate = await fakeRequests();
  await check('duplicate callback is idempotent across storage and notifications', async () => {
    const duplicate = await postGatewayLead(identity);
    assert.equal(duplicate.response.status, 200);
    assert.equal(duplicate.payload?.ok, true);
    assert.equal(duplicate.payload?.duplicated, true);
    assert.equal(duplicate.payload?.lead_id, lead.id);
    assert.equal(kxoneDB.prepare('SELECT COUNT(*) AS count FROM leads WHERE lead_no = ?').get(no).count, 1);
    await new Promise((resolve) => setTimeout(resolve, 750));
    assert.deepEqual(requestCounts(await fakeRequests()), requestCounts(beforeDuplicate));
    assert.equal(kxoneDB.prepare('SELECT COUNT(*) AS count FROM wecom_card_tasks WHERE lead_id = ?').get(lead.id).count, 2);
  });

  await check('Gateway stores only a redacted audit and no legacy lead or notification', async () => {
    const events = gatewayDB.prepare(`
      SELECT status, raw_payload FROM lead_events WHERE dedupe_key = ? ORDER BY id
    `).all(no);
    assert.equal(events.length, 2);
    for (const event of events) {
      assert.equal(event.status, 'kxone_forwarded');
      const audit = JSON.parse(event.raw_payload);
      assert.equal(audit.payloadRedacted, true);
      assert.equal(event.raw_payload.includes(fakePhone), false);
    }

    const delivery = gatewayDelivery(no);
    assert.equal(delivery.status, 'delivered');
    assert.equal(delivery.attempt_count, 1);
    assert.equal(delivery.payload_json, null);
    assert.equal(delivery.last_error, null);
    assert.equal(delivery.kxone_lead_id, lead.id);
    assert.equal(delivery.kxone_lead_no, no);
    assert.ok(delivery.first_attempt_at);
    assert.ok(delivery.delivered_at);
    assert.equal(gatewayDB.prepare('SELECT COUNT(*) AS count FROM leads').get().count, 0);
    assert.equal(gatewayDB.prepare('SELECT COUNT(*) AS count FROM notification_logs').get().count, 0);
  });
}

async function runNightlyPath() {
  await check('disabled channel account is rejected and audited without creating a lead', async () => {
    const identity = '910000004';
    const no = leadNo(identity);
    const result = await signedKXOneRequest({
      schemaVersion: 1,
      source: 'tencent',
      externalLeadId: identity,
      dedupeKey: no,
      accountName: disabledAccount,
      customerName: 'E2E Disabled Account Lead',
      phone: fakePhone,
      leadCreatedAt: '2026-01-01T00:00:00Z',
    });
    assert.equal(result.response.status, 422);
    assert.equal(result.payload?.error, 'channel_account_disabled_or_unknown');
    assert.equal(queryLead(no), undefined);
    const channel = kxoneDB.prepare(`
      SELECT enabled, last_status, last_lead_id, last_error
      FROM channel_integrations WHERE platform = 'tencent' AND account_name = ?
    `).get(disabledAccount);
    assert.equal(channel.enabled, 0);
    assert.equal(channel.last_status, 'rejected_disabled');
    assert.equal(channel.last_lead_id, null);
    assert.ok(channel.last_error);
  });

  await check('Gateway retains a verifiable outbox payload while KXOne is unavailable', async () => {
    const identity = '910000002';
    const no = leadNo(identity);
    await setFakeState({ kxoneUnavailable: true });
    const failed = await postGatewayLead(identity);
    assert.equal(failed.response.status, 503);
    assert.equal(failed.payload?.ok, false);
    assert.equal(queryLead(no), undefined);

    const delivery = gatewayDelivery(no);
    assert.equal(delivery.status, 'failed');
    assert.equal(delivery.attempt_count, 1);
    assert.ok(delivery.payload_json);
    assert.equal(createHash('sha256').update(delivery.payload_json).digest('hex'), delivery.payload_sha256);
    assert.ok(delivery.last_error);
    assert.ok(delivery.first_attempt_at);
    assert.equal(delivery.delivered_at, null);
    assert.equal(delivery.kxone_lead_id, null);
    assert.equal(delivery.kxone_lead_no, null);
    const event = gatewayDB.prepare(`
      SELECT status, raw_payload FROM lead_events WHERE dedupe_key = ? ORDER BY id DESC LIMIT 1
    `).get(no);
    assert.equal(event.status, 'failed');
    assert.equal(event.raw_payload.includes(fakePhone), true);
  });

  await check('real replay command restores KXOne and redacts the failed Gateway receipt', async () => {
    const no = leadNo('910000002');
    const before = await fakeRequests();
    await setFakeState({ kxoneUnavailable: false });
    const replay = await runReplay();
    assert.deepEqual(replay, { mode: 'apply', queued: 1, replayed: 1, failed: 0, invalid: 0 });

    const lead = await waitForLead(no);
    assertProtectedLead(lead);
    assertSuccessfulCards(await waitForSuccessfulCards(lead.id));
    await waitFor('replayed lead notifications', async () => {
      const after = await fakeRequests();
      return countDelta(after, before, 'group') === 1 && countDelta(after, before, 'message') === 2;
    });

    const delivery = gatewayDelivery(no);
    assert.equal(delivery.status, 'delivered');
    assert.equal(delivery.attempt_count, 2);
    assert.equal(delivery.payload_json, null);
    assert.equal(delivery.last_error, null);
    assert.equal(delivery.kxone_lead_id, lead.id);
    assert.equal(delivery.kxone_lead_no, no);
    assert.ok(delivery.delivered_at);

    const event = gatewayDB.prepare(`
      SELECT status, raw_payload, error_message FROM lead_events
      WHERE dedupe_key = ? ORDER BY id DESC LIMIT 1
    `).get(no);
    assert.equal(event.status, 'kxone_forwarded');
    assert.equal(event.error_message, null);
    assert.equal(event.raw_payload.includes(fakePhone), false);
    const audit = JSON.parse(event.raw_payload);
    assert.equal(audit.payloadRedacted, true);
    assert.equal(audit.replayed, true);
  });

  await check('replay is idempotent after recovery', async () => {
    const before = await fakeRequests();
    const replay = await runReplay();
    assert.deepEqual(replay, { mode: 'apply', queued: 0, replayed: 0, failed: 0, invalid: 0 });
    await new Promise((resolve) => setTimeout(resolve, 750));
    assert.deepEqual(requestCounts(await fakeRequests()), requestCounts(before));
  });

  await check('worker retries one failed application card without duplicating the group reminder', async () => {
    const identity = '910000003';
    const no = leadNo(identity);
    const before = await fakeRequests();
    await setFakeState({ messageSendFailures: 1 });
    const created = await postGatewayLead(identity);
    assert.equal(created.response.status, 200);
    assert.equal(created.payload?.duplicated, false);
    const lead = await waitForLead(no);

    const rows = await waitFor(`retried cards for lead ${lead.id}`, () => {
      const tasks = kxoneDB.prepare(`
        SELECT receiver_user_id, card_scene, card_status, retry_count, msg_id,
               response_code, next_retry_at
        FROM wecom_card_tasks WHERE lead_id = ? ORDER BY id
      `).all(lead.id);
      const sent = tasks.filter((task) => task.card_status === 'sent');
      const replaced = tasks.filter((task) => task.card_status === 'replaced');
      return tasks.length === 3 && sent.length === 2 && replaced.length === 1 ? tasks : null;
    }, 45_000);
    assert.equal(rows.filter((row) => row.card_status === 'update_failed').length, 0);
    assert.equal(rows.filter((row) => row.card_status === 'updating').length, 0);
    assert.deepEqual(
      [...new Set(rows.filter((row) => row.card_status === 'sent').map((row) => row.receiver_user_id))].sort(),
      recipients,
    );
    assert.ok(rows.filter((row) => row.card_status === 'sent').every((row) => row.msg_id && row.response_code));
    assert.equal(rows.find((row) => row.card_status === 'replaced').next_retry_at, null);

    const after = await waitFor('retry notification request counts', async () => {
      const requests = await fakeRequests();
      return countDelta(requests, before, 'group') === 1 && countDelta(requests, before, 'message') === 3
        ? requests
        : null;
    }, 45_000);
    assert.equal(countDelta(after, before, 'group'), 1);
    assert.equal(countDelta(after, before, 'message'), 3);

    const beforeDuplicate = await fakeRequests();
    const duplicate = await postGatewayLead(identity);
    assert.equal(duplicate.response.status, 200);
    assert.equal(duplicate.payload?.duplicated, true);
    await new Promise((resolve) => setTimeout(resolve, 750));
    assert.deepEqual(requestCounts(await fakeRequests()), requestCounts(beforeDuplicate));
  });

  await check('Gateway remains kxone-only throughout nightly recovery scenarios', async () => {
    assert.equal(gatewayDB.prepare('SELECT COUNT(*) AS count FROM leads').get().count, 0);
    assert.equal(gatewayDB.prepare('SELECT COUNT(*) AS count FROM notification_logs').get().count, 0);
  });
}

async function main() {
  await waitFor('KXOne', async () => (await fetch(`${kxoneURL}/health`)).ok);
  await waitFor('Gateway', async () => (await fetch(`${gatewayURL}/readyz`)).ok);
  await waitFor('fake WeCom', async () => (await fetch(`${fakeWeComURL}/healthz`)).ok);

  kxoneDB = openDatabase(kxoneDBPath);
  gatewayDB = openDatabase(gatewayDBPath);
  seedKXOne(kxoneDB);

  const reset = await fetch(`${fakeWeComURL}/_test/reset`, { method: 'POST' });
  assert.equal(reset.status, 204);

  await runQuickPath();
  if (mode === 'nightly') await runNightlyPath();
}

try {
  await main();
  report.status = 'passed';
  process.stdout.write(`E2E PASS (${mode}): Gateway -> KXOne -> SQLite/Redis -> fake WeCom\n`);
} catch (error) {
  report.status = 'failed';
  report.error = error instanceof Error ? error.message : String(error);
  process.stderr.write(`E2E FAIL (${mode}): ${report.error}\n`);
  process.exitCode = 1;
} finally {
  gatewayDB?.close();
  kxoneDB?.close();
  report.finishedAt = new Date().toISOString();
  await writeFile(reportPath, `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600 });
}
