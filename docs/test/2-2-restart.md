# 2.2 Pod Restart with IP Change

테스트 일시: `2026-02-13`  
네임스페이스/리소스: `default/hello`  
대상 Pod: `hello-follower-1`

## 검증 항목
- [x] Delete a pod (StatefulSet recreates it with new IP)
- [x] Verify `nodes.conf` preserves cluster membership
- [x] Verify pod rejoins cluster automatically
- [x] Verify `RepairDisconnectedNodes` (CLUSTER MEET) runs for disconnected pods

## 실행 절차

```bash
# 삭제 전 기준 확인
kubectl get pod -n default hello-follower-1 -o wide --no-headers
kubectl exec -n default hello-leader-1 -- redis-cli -h 127.0.0.1 -p 6379 cluster nodes | rg hello-follower-1
kubectl exec -n default hello-follower-1 -- sh -c 'grep -n "myself" /node-conf/nodes.conf'

# Pod 재시작
kubectl delete pod -n default hello-follower-1
kubectl wait --for=condition=Ready pod/hello-follower-1 -n default --timeout=120s

# 삭제 후 확인
kubectl get pod -n default hello-follower-1 -o wide --no-headers
kubectl exec -n default hello-follower-1 -- sh -c 'grep -n "myself" /node-conf/nodes.conf'
kubectl exec -n default hello-leader-1 -- redis-cli -h 127.0.0.1 -p 6379 cluster nodes | rg hello-follower-1
kubectl exec -n default hello-leader-1 -- redis-cli -h 127.0.0.1 -p 6379 cluster info
```

## 결과

### 1) Pod 재생성 + IP 변경
- 변경 전 IP: `10.244.2.87`
- 변경 후 IP: `10.244.2.88`
- StatefulSet이 동일 ordinal Pod(`hello-follower-1`)를 재생성했고 IP가 변경됨.

### 2) `nodes.conf` 클러스터 멤버십 보존
- 삭제 전 `nodes.conf myself`:
  - `a3aaedae59c835cfb8ae7cc5034cf89f57269865 10.244.2.87:6379 ... myself,slave ...`
- 삭제 후 `nodes.conf myself`:
  - `a3aaedae59c835cfb8ae7cc5034cf89f57269865 10.244.2.88:6379 ... myself,slave ...`
- `node ID(a3aaedae...)`는 유지되고 IP만 갱신됨.

### 3) 자동 재조인
- `cluster nodes`에서 동일 node ID(`a3aaedae...`)가 `connected slave`로 복귀 확인.
- `cluster info`:
  - `cluster_state:ok`
  - `cluster_slots_ok:16384`
  - `cluster_slots_fail:0`

### 4) `RepairDisconnectedNodes (CLUSTER MEET)` 실행 여부
- 이번 “단일 follower 빠른 재시작” 시나리오에서는 관련 로그 미관측:
  - `RepairDisconnectedNodes`
  - `Using Pod IP-based CLUSTER MEET`
  - `Successfully executed CLUSTER MEET`
- 동시에 로그상 `Failed Node Count: 0` 유지.
- 해석:
  - 재시작 노드는 `nodes.conf`의 기존 peer 정보로 빠르게 재연결되고,
  - 살아있는 다른 노드들이 gossip으로 최신 주소를 전파해 클러스터가 먼저 정상화됨.
  - 그래서 컨트롤러의 `if int(desiredTotalReplicas) > 1 && unhealthyNodeCount > 0` 분기 진입 전에
    `unhealthyNodeCount`가 0이 되어 `CLUSTER MEET` 복구 경로가 실행되지 않음.

## 추가 검증: 클러스터 CR 삭제/재생성 (전체 Pod IP 변경)

실행:

```bash
kubectl delete rediscluster -n default hello
kubectl wait --for=delete rediscluster/hello -n default --timeout=180s
kubectl apply -f test-redis-cluster.yaml
```

IP 비교 결과:

```text
OLD
hello-leader-0   10.244.2.86
hello-leader-1   10.244.2.80
hello-leader-2   10.244.2.85
hello-follower-0 10.244.2.82
hello-follower-1 10.244.2.88
hello-follower-2 10.244.2.84

NEW
hello-leader-0   10.244.2.89
hello-leader-1   10.244.2.90
hello-leader-2   10.244.2.91
hello-follower-0 10.244.2.92
hello-follower-1 10.244.2.93
hello-follower-2 10.244.2.94
```

검증 결과:
- 모든 `hello` Pod IP가 변경된 상태에서 `RedisCluster`는 `Ready(3/3)`로 복구됨.
- `cluster_state:ok`, `cluster_slots_ok:16384`, `cluster_slots_fail:0` 확인.
- node ID 집합은 동일하게 유지되어(`nodes.conf` 기반) 멤버십 연속성 확인.
- 이번 시나리오에서는 `RepairDisconnectedNodes` 경로 실행 로그를 확인:
  - `Using Pod IP-based CLUSTER MEET for all StatefulSet Pods`
  - `Successfully executed CLUSTER MEET` (`10.244.2.89` ~ `10.244.2.94`)
  - `no unhealthy nodes found after repairing disconnected masters`

## 결론
- Pod 재시작(IP 변경) 상황에서 `nodes.conf` 기반 멤버십 보존 및 자동 재조인은 정상 동작함.
- 단일 Pod 재시작에서는 `RepairDisconnectedNodes`가 미발생할 수 있으나,
  클러스터 CR 삭제/재생성(전체 IP 변경) 상황에서는 `CLUSTER MEET` 기반 복구 경로가 실제 실행됨.
