# Pod CrashLoopBackOff — Runbook

## Symptoms
- One or more pods in `CrashLoopBackOff` state
- `kubectl get pods` shows RESTARTS > 5
- OOMKilled or Exit Code 1/137 in pod events

## Diagnosis Steps
1. Get pod events: `kubectl describe pod <pod-name> -n <namespace>`
2. Tail logs from the crashing container:
   ```
   kubectl logs <pod-name> --previous -n <namespace>
   ```
3. Check if memory limit is too low (Exit Code 137 = OOMKilled)
4. Check if a config map or secret is missing

## Remediation

### Option A — Restart Pod (low risk)
```
kubectl delete pod <pod-name> -n <namespace>
```

### Option B — Rollback Deployment (medium risk)
If crash started after a recent deploy:
```
kubectl rollout undo deployment/<name> -n <namespace>
```

### Option C — Increase Memory Limits (medium risk)
Edit the deployment to raise memory limit, then re-apply:
```yaml
resources:
  limits:
    memory: "512Mi"  # increase as appropriate
```

### Option D — Fix Missing Config (low risk)
If logs show `env variable not found` or `secret missing`:
```
kubectl create secret generic <name> --from-literal=KEY=value -n <namespace>
```
Then restart the deployment.

## Verification
```
kubectl get pods -n <namespace> -w
```
All pods should reach `Running` state within 2 minutes.
