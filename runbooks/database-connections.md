# Database Connection Exhaustion — Runbook

## Symptoms
- `too many connections` or `connection pool exhausted` errors in app logs
- Database CPU/memory normal but connections > 90% of max_connections
- Increased query latency

## Diagnosis Steps
1. Check current connection count:
   ```sql
   SELECT count(*) FROM pg_stat_activity;
   SELECT max_conn, used FROM (SELECT count(*) used FROM pg_stat_activity) t, (SELECT setting::int max_conn FROM pg_settings WHERE name='max_connections') t2;
   ```
2. Identify which app instances are holding connections: `SELECT client_addr, count(*) FROM pg_stat_activity GROUP BY client_addr;`
3. Check for long-running or idle transactions: `SELECT pid, state, query_start, query FROM pg_stat_activity WHERE state != 'idle' ORDER BY query_start;`

## Remediation

### Option A — Restart Application Pods (low risk)
Closes all existing connections from the affected service:
```
kubectl rollout restart deployment/<name> -n <namespace>
```

### Option B — Kill Idle Connections (medium risk)
```sql
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE state = 'idle' AND query_start < NOW() - INTERVAL '10 minutes';
```

### Option C — Reduce Connection Pool Size (medium risk)
Update the app's `DATABASE_POOL_MAX` env var and redeploy.

### Option D — PgBouncer Connection Pooler (high risk)
Deploy PgBouncer between app and DB to multiplex connections.
Requires planning and approval.

## Verification
Confirm connection count drops below 70% of max_connections within 5 minutes.
