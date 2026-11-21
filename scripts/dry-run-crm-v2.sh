#!/usr/bin/env bash
set -euo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
if [[ -n "$SCRIPT_SOURCE" ]]; then
  ROOT="$(cd "$(dirname "$SCRIPT_SOURCE")/.." && pwd)"
else
  ROOT="$(pwd)"
fi
DB_PATH="${DB_NAME:-$ROOT/.runtime/data/kxone.db}"

command -v sqlite3 >/dev/null 2>&1 || { echo "sqlite3 is required" >&2; exit 1; }
[[ -f "$DB_PATH" ]] || { echo "CRM database not found: $DB_PATH" >&2; exit 1; }

has_column() {
  local table="$1" column="$2"
  sqlite3 -readonly "$DB_PATH" "SELECT COUNT(*) FROM pragma_table_info('$table') WHERE name='$column';"
}

echo "CRM V2 migration dry-run (aggregate data only)"
echo "database: $DB_PATH"
echo "captured_at_utc: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo

sqlite3 -readonly -header -column "$DB_PATH" <<'SQL'
SELECT 'leads' AS metric, COUNT(*) AS value FROM leads
UNION ALL SELECT 'followups', COUNT(*) FROM lead_followups
UNION ALL SELECT 'dispatch_records', COUNT(*) FROM lead_dispatch_records
UNION ALL SELECT 'user_bindings', COUNT(*) FROM crm_wecom_user_bindings;

SELECT status AS legacy_status,
       COUNT(*) AS count,
       CASE WHEN status IN ('converted', 'invalid', 'closed') THEN 'finished' ELSE 'invitation' END AS mapped_business_stage,
       CASE
         WHEN status = 'pending_dispatch' THEN 'pending_assignment'
         WHEN status = 'assigned_pending_claim' THEN 'assigned_pending_claim'
         ELSE 'private'
       END AS mapped_ownership_state,
       CASE
         WHEN status = 'converted' THEN 'converted'
         WHEN status = 'invalid' THEN 'invalid'
         WHEN status = 'closed' THEN 'closed'
         ELSE 'active'
       END AS mapped_result_status
FROM leads
GROUP BY status
ORDER BY status;

SELECT role AS legacy_role, status, COUNT(*) AS count,
       CASE WHEN role = 'sales' THEN 'inviter' ELSE role END AS mapped_role
FROM crm_wecom_user_bindings
GROUP BY role, status
ORDER BY role, status;

SELECT 'pending_claim_without_assignee' AS anomaly, COUNT(*) AS count
FROM leads WHERE status = 'assigned_pending_claim' AND COALESCE(assigned_sales_user_id, '') = ''
UNION ALL
SELECT 'claimed_or_following_without_owner', COUNT(*)
FROM leads WHERE status IN ('claimed', 'following') AND COALESCE(owner_user_id, '') = ''
UNION ALL
SELECT 'unknown_legacy_status', COUNT(*)
FROM leads WHERE status NOT IN ('pending_dispatch', 'assigned_pending_claim', 'claimed', 'following', 'converted', 'invalid', 'closed')
UNION ALL
SELECT 'lead_owner_without_active_binding', COUNT(*)
FROM leads l
WHERE COALESCE(l.owner_user_id, '') <> ''
  AND NOT EXISTS (
    SELECT 1 FROM crm_wecom_user_bindings b
    WHERE b.wecom_userid = l.owner_user_id AND b.status = 'active'
  );

SELECT version, name, applied_at
FROM schema_migrations
ORDER BY version;

PRAGMA quick_check;
PRAGMA foreign_key_check;
SQL

echo
echo "Historical star mapping"
if [[ "$(has_column lead_followups star_rating)" == "1" ]]; then
  sqlite3 -readonly -header -column "$DB_PATH" <<'SQL'
SELECT CASE
         WHEN star_rating BETWEEN 1 AND 5 THEN CAST(star_rating AS TEXT)
         WHEN intention_level = 'high' THEN '5'
         WHEN intention_level = 'medium' THEN '3'
         WHEN intention_level = 'low' THEN '1'
         ELSE '3'
       END AS mapped_star_rating,
       COUNT(*) AS followup_count
FROM lead_followups
GROUP BY mapped_star_rating
ORDER BY mapped_star_rating;
SQL
else
  sqlite3 -readonly -header -column "$DB_PATH" <<'SQL'
SELECT CASE intention_level
         WHEN 'high' THEN '5'
         WHEN 'medium' THEN '3'
         WHEN 'low' THEN '1'
         ELSE '3'
       END AS mapped_star_rating,
       COUNT(*) AS followup_count
FROM lead_followups
GROUP BY mapped_star_rating
ORDER BY mapped_star_rating;
SQL
fi

if [[ "$(has_column leads business_stage)" == "1" ]]; then
  echo
  echo "Existing V2 projection checks"
  sqlite3 -readonly -header -column "$DB_PATH" <<'SQL'
SELECT 'blank_business_stage' AS anomaly, COUNT(*) AS count FROM leads WHERE COALESCE(business_stage, '') = ''
UNION ALL SELECT 'blank_ownership_state', COUNT(*) FROM leads WHERE COALESCE(ownership_state, '') = ''
UNION ALL SELECT 'blank_result_status', COUNT(*) FROM leads WHERE COALESCE(result_status, '') = ''
UNION ALL SELECT 'invalid_star_rating', COUNT(*) FROM leads WHERE star_rating NOT BETWEEN 1 AND 5
UNION ALL SELECT 'negotiation_in_public_pool', COUNT(*) FROM leads WHERE business_stage = 'negotiation' AND ownership_state = 'public_pool';
SQL
fi

echo
echo "Dry-run only: no rows were modified. No historical note text was used to infer arrival."
