# High CPU Usage — Runbook

## Symptoms
- CPU utilisation > 90% on one or more nodes
- Pod evictions or OOMKilled events
- Increased request latency

## Diagnosis Steps
1. Identify the process consuming the most CPU: `kubectl top pods -n <namespace>`
2. Check for recent deployments that may have introduced a CPU-intensive code path
3. Review Prometheus metrics: `rate(process_cpu_seconds_total[5m])`
4. Check for runaway cron jobs or batch workloads

## Remediation

### Option A — Horizontal Scale-Out (low risk)
```
kubectl scale deployment <name> --replicas=<current+2> -n <namespace>
```

### Option B — Rollback Recent Deploy (medium risk)
If a recent deployment correlates with the spike:
```
kubectl rollout undo deployment/<name> -n <namespace>
kubectl rollout status deployment/<name> -n <namespace>
```

### Option C — Resource Limits Adjustment (medium risk)
Increase CPU limits in the deployment manifest and re-apply.

## Verification
After remediation, confirm CPU drops below 70% within 5 minutes:
```
kubectl top pods -n <namespace>
```

## Escalation
If CPU remains > 90% after scaling, escalate to the on-call lead.
