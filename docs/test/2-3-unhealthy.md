# 2.3 Unhealthy Cluster

테스트 일시: `2026-02-13`  
네임스페이스/리소스: `default/hello`  
대상 Pod: `hello-follower-1`

## 검증 항목
- [x] Simulate 1 unhealthy node -> verify auto-repair with retry
- [x] Simulate majority unhealthy (>= total-1) -> verify error: `manual intervention required`

## 사전 조건
- `test-redis-cluster.yaml`에 아래 capability를 적용한 상태
  - `spec.redisLeader.containerSecurityContext.capabilities.add: [NET_ADMIN, NET_RAW]`
  - `spec.redisFollower.containerSecurityContext.capabilities.add: [NET_ADMIN, NET_RAW]`

## 실행

```bash
# 1) 단일 노드 네트워크 단절 35초
kubectl exec -n default hello-follower-1 -c redis -- \
  sh -c 'echo START:$(date +%H:%M:%S); ip link set eth0 down; sleep 35; ip link set eth0 up; echo END:$(date +%H:%M:%S)'

# 2) 운영자 로그 확인
kubectl logs -n default rediscluster-controller-manager-6cd7d4ff69-nxf7w \
  --since-time=2026-02-13T14:08:30Z | \
  rg 'Failed Node Count|IsFailedOrDisconnected|CLUSTER MEET|repairing unhealthy masters'

# 3) 최종 정상 여부 확인
kubectl get rediscluster -n default hello -o wide
kubectl exec -n default hello-leader-0 -c redis -- redis-cli -p 6379 cluster info
kubectl exec -n default hello-leader-0 -c redis -- redis-cli -p 6379 cluster nodes
```

## 결과

### 1) 장애 유도 성공
- `hello-follower-1`에서 `eth0 down`/`up` 실행 완료
  - `START:14:08:42`
  - `END:14:09:17`

### 2) unhealthy 감지 및 복구 경로 실행 확인 (로그 근거)
- `2026-02-13T14:08:59Z`
  - `NodeID: a3aa...` (hello-follower-1)
  - `Flags: "slave,fail"`
  - `IsFailedOrDisconnected: true`
  - `Failed Node Count: 1`
- `2026-02-13T14:09:00Z`
  - `Using Pod IP-based CLUSTER MEET for all StatefulSet Pods`
  - `Successfully executed CLUSTER MEET` (6개 Pod IP)
- `2026-02-13T14:09:23Z`
  - `Failed Node Count: 0`
  - `repairing unhealthy masters successful, no unhealthy masters left`

참고:
- 복구 중 `handshake/disconnected` 엔트리가 일시적으로 늘며 `Failed Node Count`가 크게 보일 수 있음
  (이번 실행에서는 최대 7까지 관측).

### 3) 최종 상태
- RedisCluster:
  - `STATE: Ready`
  - `READYLEADERREPLICAS: 3`
  - `READYFOLLOWERREPLICAS: 3`
- Redis Cluster:
  - `cluster_state:ok`
  - `cluster_slots_ok:16384`
  - `cluster_slots_fail:0`
  - `cluster_known_nodes:6`
- `cluster nodes` 기준 모든 노드 `connected` 복귀 확인.

## 결론
- 단일 노드 비정상(`eth0 down`) 시나리오에서 컨트롤러가 unhealthy를 감지하고, `CLUSTER MEET` 기반 복구를 수행해 정상 상태로 회복됨을 확인했다.
- 다수 노드 비정상(`5/6`) 시나리오에서도 `cluster broken: 5/6 nodes unhealthy, manual intervention required` 에러를 확인했다.
- 네트워크 복구 후에는 자동 복구 로직으로 `Ready` 상태까지 복귀함을 확인했다.

---

## 2.3.2 Majority Unhealthy (>= total-1)

테스트 일시: `2026-02-13`  
기준 시각(UTC): `2026-02-13T14:13:52Z`

### 실행

```bash
# leader-0은 유지하고 나머지 5개 Pod를 55초간 동시에 단절
kubectl exec -n default hello-leader-1 -c redis -- sh -c 'ip link set eth0 down; sleep 55; ip link set eth0 up'
kubectl exec -n default hello-leader-2 -c redis -- sh -c 'ip link set eth0 down; sleep 55; ip link set eth0 up'
kubectl exec -n default hello-follower-0 -c redis -- sh -c 'ip link set eth0 down; sleep 55; ip link set eth0 up'
kubectl exec -n default hello-follower-1 -c redis -- sh -c 'ip link set eth0 down; sleep 55; ip link set eth0 up'
kubectl exec -n default hello-follower-2 -c redis -- sh -c 'ip link set eth0 down; sleep 55; ip link set eth0 up'

# 로그 확인
kubectl logs -n default rediscluster-controller-manager-6cd7d4ff69-nxf7w \
  --since-time=2026-02-13T14:14:05Z | \
  rg 'Failed Node Count|manual intervention required|cluster broken|repairing unhealthy masters successful'
```

### 결과 (로그 근거)

- 장애 주입 시작/종료:
  - `START:14:14:02`
  - `END:14:14:57`
- `2026-02-13T14:14:10Z`
  - `Failed Node Count: 5` 확인
- `2026-02-13T14:14:25Z` (및 14:14:41Z, 14:14:56Z 재발)
  - `error: "cluster broken: 5/6 nodes unhealthy, manual intervention required"`
  - `Reconciler error`에도 동일 문구 확인
- 복구 후:
  - `2026-02-13T14:15:02Z` `Failed Node Count: 0`
  - `repairing unhealthy masters successful, no unhealthy masters left`

참고:
- 복구 도중 `handshake/disconnected` 임시 노드가 생기며 `Failed Node Count`가 일시적으로 11까지 증가했다.
- 네트워크 복구 후 재조정 과정에서 마스터/슬레이브 역할 재배치가 발생할 수 있으나, 슬롯 커버리지는 유지된다.

### 최종 상태

- `kubectl get rediscluster -n default hello -o wide`
  - `STATE: Ready`
  - `READYLEADERREPLICAS: 3`
  - `READYFOLLOWERREPLICAS: 3`
- `cluster info`
  - `cluster_state:ok`
  - `cluster_slots_ok:16384`
  - `cluster_slots_fail:0`
  - `cluster_known_nodes:6`
