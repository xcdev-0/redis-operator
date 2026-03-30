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
- [x] Scale leader from 3 -> 5 (update CR `redisLeader.replicaCount`)
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

### 4.2 TLS
- [x] Create cluster with TLS config (ca.crt, tls.crt, tls.key)
- [x] Verify Redis starts with TLS enabled
- [x] Verify cluster bus uses TLS
- [x] Verify redis-cli commands include `--tls --cert --key --cacert`

---

## 5. Configuration

### 5.1 Redis Config
- [ ] Set `maxMemoryPercentOfLimit` -> verify `maxmemory` is calculated correctly
- [ ] Apply `additionalRedisConfig` via ConfigMap -> verify settings applied
- [ ] Apply `dynamicConfig` -> verify CONFIG SET runs on all nodes after Ready

### 5.2 Resource Limits
- [ ] Verify container resource requests/limits match CR spec
- [ ] Verify role-specific resources (leader vs follower) if set differently

---

## 6. Monitoring

### 6.1 Redis Exporter
- [ ] Enable exporter -> verify sidecar container is injected
- [ ] Verify exporter port (default 9121) is exposed
- [ ] Curl `localhost:9121/metrics` inside pod -> verify Prometheus metrics
- [ ] Verify exporter custom environment variables are set

---

## 7. Service Configuration

### 7.1 Service Types
- [ ] ClusterIP (default) -> verify internal access
- [ ] Verify headless service for StatefulSet DNS
