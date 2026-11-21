import http from 'node:http';

const host = process.env.HOST ?? '0.0.0.0';
const port = Number.parseInt(process.env.PORT ?? '8088', 10);
const kxoneUpstreamURL = process.env.KXONE_UPSTREAM_URL ?? 'http://kxone-server:8080';
const requests = [];
let sequence = 0;
const faults = {
  kxoneUnavailable: false,
  messageSendFailures: 0,
  messageUpdateFailures: 0,
  groupWebhookFailures: 0,
};

function sendJSON(response, status, payload) {
  const body = JSON.stringify(payload);
  response.writeHead(status, {
    'content-type': 'application/json; charset=utf-8',
    'content-length': Buffer.byteLength(body),
  });
  response.end(body);
}

async function readBody(request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 1024 * 1024) throw new Error('request body too large');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

async function readJSON(request) {
  const body = await readBody(request);
  if (body.length === 0) return null;
  return JSON.parse(body.toString('utf8'));
}

function record(kind, request, url, body = null) {
  requests.push({
    id: ++sequence,
    kind,
    method: request.method,
    path: url.pathname,
    queryKeys: [...url.searchParams.keys()].sort(),
    body,
  });
}

function resetState() {
  requests.length = 0;
  sequence = 0;
  faults.kxoneUnavailable = false;
  faults.messageSendFailures = 0;
  faults.messageUpdateFailures = 0;
  faults.groupWebhookFailures = 0;
}

function consumeFailure(name) {
  if (faults[name] <= 0) return false;
  faults[name] -= 1;
  return true;
}

async function proxyKXOne(request, response, url) {
  record('kxone-proxy', request, url);
  if (faults.kxoneUnavailable) {
    return sendJSON(response, 503, { ok: false, error: 'e2e_kxone_unavailable' });
  }

  const body = await readBody(request);
  const upstream = new URL(`${url.pathname}${url.search}`, kxoneUpstreamURL);
  const headers = {};
  for (const name of ['content-type', 'x-kxone-gateway-timestamp', 'x-kxone-gateway-signature']) {
    const value = request.headers[name];
    if (typeof value === 'string') headers[name] = value;
  }
  const upstreamResponse = await fetch(upstream, {
    method: request.method,
    headers,
    body: body.length > 0 ? body : undefined,
  });
  const responseBody = Buffer.from(await upstreamResponse.arrayBuffer());
  response.writeHead(upstreamResponse.status, {
    'content-type': upstreamResponse.headers.get('content-type') ?? 'application/json; charset=utf-8',
    'content-length': responseBody.length,
  });
  response.end(responseBody);
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url ?? '/', 'http://fake-wecom.local');

  try {
    if (request.method === 'GET' && (url.pathname === '/health' || url.pathname === '/healthz')) {
      return sendJSON(response, 200, { ok: true });
    }
    if (request.method === 'GET' && (url.pathname === '/requests' || url.pathname === '/_test/requests')) {
      return sendJSON(response, 200, { requests });
    }
    if ((request.method === 'DELETE' && url.pathname === '/requests') ||
        (request.method === 'POST' && url.pathname === '/_test/reset')) {
      resetState();
      response.writeHead(204);
      return response.end();
    }
    if (request.method === 'GET' && url.pathname === '/_test/state') {
      return sendJSON(response, 200, { faults });
    }
    if (request.method === 'POST' && url.pathname === '/_test/state') {
      const body = await readJSON(request);
      if (body && typeof body === 'object') {
        if (typeof body.kxoneUnavailable === 'boolean') faults.kxoneUnavailable = body.kxoneUnavailable;
        for (const name of ['messageSendFailures', 'messageUpdateFailures', 'groupWebhookFailures']) {
          if (Number.isInteger(body[name]) && body[name] >= 0) faults[name] = body[name];
        }
      }
      return sendJSON(response, 200, { faults });
    }
    if (url.pathname.startsWith('/api/internal/')) {
      return proxyKXOne(request, response, url);
    }
    if (request.method === 'GET' && url.pathname === '/cgi-bin/gettoken') {
      record('access-token', request, url);
      return sendJSON(response, 200, {
        errcode: 0,
        errmsg: 'ok',
        access_token: 'local',
        expires_in: 7200,
      });
    }
    if (request.method === 'GET' && url.pathname === '/cgi-bin/get_jsapi_ticket') {
      record('jsapi-ticket', request, url);
      return sendJSON(response, 200, {
        errcode: 0,
        errmsg: 'ok',
        ticket: 'local-ticket',
        expires_in: 7200,
      });
    }
    if (request.method === 'GET' && url.pathname === '/cgi-bin/auth/getuserinfo') {
      record('get-user-info', request, url);
      return sendJSON(response, 200, {
        errcode: 0,
        errmsg: 'ok',
        UserId: 'e2e-user',
      });
    }
    if (request.method === 'POST' && url.pathname === '/cgi-bin/message/send') {
      const body = await readJSON(request);
      record('message-send', request, url, body);
      if (consumeFailure('messageSendFailures')) {
        return sendJSON(response, 200, { errcode: 45009, errmsg: 'e2e forced send failure' });
      }
      return sendJSON(response, 200, {
        errcode: 0,
        errmsg: 'ok',
        msgid: `local-message-${sequence}`,
        response_code: `local-response-${sequence}`,
      });
    }
    if (request.method === 'POST' && url.pathname === '/cgi-bin/message/update_template_card') {
      const body = await readJSON(request);
      record('message-update', request, url, body);
      if (consumeFailure('messageUpdateFailures')) {
        return sendJSON(response, 200, { errcode: 45009, errmsg: 'e2e forced update failure' });
      }
      return sendJSON(response, 200, { errcode: 0, errmsg: 'ok' });
    }
    if (request.method === 'POST' && url.pathname === '/cgi-bin/webhook/send') {
      const body = await readJSON(request);
      record('group-webhook', request, url, body);
      if (consumeFailure('groupWebhookFailures')) {
        return sendJSON(response, 200, { errcode: 45009, errmsg: 'e2e forced webhook failure' });
      }
      return sendJSON(response, 200, { errcode: 0, errmsg: 'ok' });
    }

    return sendJSON(response, 404, { errcode: 404, errmsg: 'not found' });
  } catch (error) {
    return sendJSON(response, 400, {
      errcode: 400,
      errmsg: error instanceof Error ? error.message : 'bad request',
    });
  }
});

server.listen(port, host, () => {
  process.stdout.write(`fake WeCom listening on ${host}:${port}\n`);
});

function shutdown() {
  server.close(() => process.exit(0));
}

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
