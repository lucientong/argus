# High Error Rate (5xx) — Runbook

## Symptoms
- HTTP 5xx error rate > 5% sustained for > 2 minutes
- Spike in `http_requests_total{status="500"}` or `5xx` metrics
- User-facing service degradation

## Diagnosis Steps
1. Check pod logs for stack traces: `kubectl logs -l app=<service> --tail=100 -n <namespace>`
2. Look for recent deployments: `kubectl rollout history deployment/<name> -n <namespace>`
3. Verify database connectivity if errors mention DB timeouts
4. Check downstream dependencies for cascading failures

## Remediation

### Option A — Rollback Recent Deploy (medium risk)
If a deploy occurred < 30 minutes ago and errors began shortly after:
```
kubectl rollout undo deployment/<name> -n <namespace>
kubectl rollout status deployment/<name> -n <namespace>
```

### Option B — Restart Unhealthy Pods (low risk)
If specific pods are in CrashLoopBackOff:
```
kubectl delete pod <pod-name> -n <namespace>
```
The deployment controller will reschedule a fresh pod.

### Option C — Circuit Breaker / Traffic Drain (high risk)
If the service is completely broken, temporarily redirect traffic:
```
kubectl annotate ingress <name> nginx.ingress.kubernetes.io/service-weight="0" -n <namespace>
```
Requires approval before execution.

## Verification
Monitor error rate for 5 minutes after remediation. Target: < 1% 5xx.

## Escalation
If errors persist after rollback, page the on-call lead immediately.
