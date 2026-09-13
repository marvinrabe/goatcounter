# Kubernetes deployment and verification

The app pods are stateless. All replicas must use the **same primary libSQL
endpoint**, with durable storage and credentials provided by your database
operator. Do not give each replica a SQLite file or point them at independently
replicated database snapshots. The queue, session decisions, and statistics
require a common transactional authority.

Create a `goatcounter-config` Secret with `GOATCOUNTER_DB`, select your release
image and site names in `app.yaml`, and apply it to your namespace. Optional
API/OIDC credentials can be included in the same Secret. The example runs
three replicas, spreads them across nodes, uses read-only root filesystems,
disables injected service environment variables, and permits no unavailable
replicas during a rollout. Configure ingress TLS and any cluster-wide request
limit in your ingress controller.

`/status` is the only health endpoint and checks readiness and database
connectivity. A TCP liveness probe checks the listener independently, so a
database outage removes traffic without restarting every app replica.
SIGTERM makes readiness fail immediately, allows five seconds for
endpoint removal, and drains requests/workers within 25 seconds. Kubernetes
allows 30 seconds before force termination. No session snapshot or final
pageview flush is necessary: accepted work is already in the database.

## Reproduce the verification

Requirements: Docker, Go (to install kind if needed), kubectl, kind, Python 3,
and a built local app image. Use a disposable cluster; the script creates a
labeled namespace and a test database with a PVC. It does not touch the local
Compose database, sample data, or your current kubectl context.

```sh
docker compose build
go install sigs.k8s.io/kind@v0.33.0
kind create cluster --name goatcounter-test --kubeconfig /tmp/goatcounter-kubeconfig --config deploy/kubernetes/kind.yaml
python3 scripts/test_cluster.py --kubeconfig /tmp/goatcounter-kubeconfig
```

Use `--kind /path/to/kind` if it is not on PATH. Use `--reset` to replace only
an existing verification namespace owned by this script. The proof needs at
least two worker nodes, as configured in `kind.yaml`.

The script performs real HTTP requests from a separate in-cluster pod:

It first checks that `/status` is the only health route, the unused `store`
table is absent, and startup logs are JSON without an opt-in flag. It then stops
the database for at least 35 seconds: all replicas must become unready, remain
running without restarts, and recover readiness when the database returns.

1. Contact all three app pod IPs as one visitor. Verify nine pageviews but only
   one session and one visit for the repeated path.
2. Inject a database write failure. Verify accepted work remains queued and no
   partial paths/hits/statistics commit; remove the failure and verify recovery.
3. Send uninterrupted requests through the Kubernetes Service while replacing
   the Deployment. Verify no HTTP failures, exact accepted/stored counts,
   matching aggregates, and one session.
4. Pause processing, durably enqueue 24 pageviews, forcibly delete all app
   pods with zero grace, and start replacements. Verify all 24 are recovered
   once, with the original visitor session.

Results, image ID, node placement, and rollout duration are written to
`.test-results/cluster-report.json`. Pods are left running for inspection.
Delete the disposable cluster when finished:

```sh
kind delete cluster --name goatcounter-test
```

This proves application replica replacement and rollback of unfinished work.
It does not certify database-node disaster recovery, ingress-specific draining,
or compatibility between different database schema versions. The test database
is a single unauthenticated primary reachable only inside the disposable
cluster; production should use an authenticated managed endpoint or an
appropriately secured database deployment. Client retries following an
ambiguous network response can duplicate a pageview because the tracker has
no client-supplied idempotency key.
