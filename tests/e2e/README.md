# Gateway to KXOne E2E

This harness starts a disposable, network-isolated stack containing Redis,
KXOne server/worker, Lead Gateway in `kxone-only` mode, a fake WeCom service,
and a one-shot test runner. It never publishes a host port and uses only
synthetic credentials, identities, SQLite databases, and notification targets.

Run the pull-request suite from the repository root:

```bash
./scripts/run-e2e.sh quick
```

Run the extended recovery suite:

```bash
./scripts/run-e2e.sh nightly
```

The quick suite verifies Gateway HMAC enforcement, Tencent normalization,
Gateway audit redaction, KXOne encrypted storage and contact hashing, account
source metadata, duplicate idempotency, and exactly one application card for
each active administrator/manager plus one group-robot reminder.

The nightly suite additionally verifies a disabled channel account, a simulated
KXOne outage, the real compiled replay command, post-replay PII redaction,
Worker consumption, a forced WeCom card failure followed by delayed retry, and
notification idempotency after recovery.

Machine-readable reports are copied to `tests/e2e/reports/`. Failure logs are
sanitized before being stored there. Containers and volumes are destroyed after
every run. Set `E2E_KEEP_STACK=1` only for local inspection, then remove it with:

```bash
docker compose -p xiangwu-pipeline-e2e -f tests/e2e/compose.yml down --volumes --remove-orphans
```
