# Redis Cluster Operator Test Checklist

## 1. Cluster Lifecycle

### 1.1 Initial Creation
- [x] 3 leader + 3 follower cluster creation
- [x] Verify StatefulSets are created (leader, follower)
- [x] Verify headless Services are created
- [x] Verify status transitions: Initializing -> Bootstrap -> Ready
- [x] Verify all 16384 slots are assigned across leaders
- [x] Verify follower -> leader replication mapping is correct
- [x] Verify Pod role labels match actual Redis roles

### 1.2 Scale Up
- [x] Scale cluster from 3 -> 5 (update CR `spec.clusterSize`)
- [x] Verify new leader pods are added to the cluster via `CLUSTER ADD-NODE`
- [x] Verify slots are rebalanced to new leaders (`--cluster-use-empty-masters`)
- [x] Scale follower from 3 -> 5
- [x] Verify new followers attach to correct leaders

### 1.3 Scale Down
- [x] Scale leader from 5 -> 3
- [x] Verify slot migration from removed shards to remaining leaders
- [x] Verify followers of removed leaders are deleted first
- [x] Verify cluster rebalance after downscale
- [x] Verify no data loss (write keys before, read after)

---

## 2. Resilience & Recovery

### 2.1 Pod Failure
- [x] Kill a leader pod -> verify Redis auto-failover promotes follower
- [x] Kill a follower pod -> verify it rejoins cluster after restart
- [x] Write keys before leader failure and verify reads after promotion/rejoin

### 2.2 Pod Restart with IP Change
- [x] Delete a pod (StatefulSet recreates it with new IP)
- [x] Verify `nodes.conf` preserves cluster membership
- [x] Verify pod rejoins cluster automatically
- [x] Verify `RepairDisconnectedNodes` (CLUSTER MEET) runs for disconnected pods

### 2.3 Unhealthy Cluster
- [x] Simulate 1 unhealthy node -> verify auto-repair with retry (3 attempts, 5s delay)
- [x] Simulate majority unhealthy (>= total-1) -> verify error: "manual intervention required"

---

## 4. Security

### 4.1 Password Authentication
- [x] Create cluster with `kubernetesConfig.existingPasswordSecret`
- [x] Verify redis-cli commands include `-a <password>`
- [x] Verify exporter connects with password
- [x] Verify health probes use password
- [x] Verify password auth does not break Redis exporter sidecar startup

### 4.2 TLS
- [x] Create cluster with TLS config (ca.crt, tls.crt, tls.key)
- [x] Verify Redis starts with TLS enabled
- [x] Verify cluster bus uses TLS
- [x] Verify redis-cli commands include `--tls --cert --key --cacert`
- [x] Verify exporter TLS environment is rendered (`REDIS_ADDR=rediss://...`, TLS cert paths)
- [x] Verify Prometheus targets remain `up` after TLS is enabled

---

## 6. Monitoring

### 6.1 Redis Exporter + Prometheus
- [x] Enable exporter -> verify sidecar container is injected
- [x] Verify headless Services expose exporter port (default `9121`)
- [x] Verify headless Services carry `redis.ej.com/metrics-scrape=true`
- [x] Verify `ServiceMonitor` is created in `monitoring` namespace
- [x] Verify `PrometheusRule` is created for Redis alerts
- [x] Verify Prometheus targets for leader/follower headless Services are `up`
- [x] Query Redis metrics in Prometheus (`redis_cluster_connections`)
- [x] Verify exporter custom environment variables are rendered

### 6.2 Operator Metrics
- [x] Verify controller-manager metrics Service is scraped by Prometheus
- [x] Add Helm-managed `ServiceMonitor` or equivalent scrape config for operator metrics
