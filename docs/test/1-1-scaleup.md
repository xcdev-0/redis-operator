# 1-1 Scale Up Test (3 -> 6)

테스트 일시: `2026-02-13`  
네임스페이스/리소스: `default/ejcluster`

## 목적
- 스케일업 후 클러스터 상태가 정상인지 확인
- 스케일업 전 입력한 데이터가 유지되는지 확인
- 증가한 ordinal(3~5)의 PVC가 정상 생성되는지 확인

## 사전 상태
- `RedisCluster`: `clusterSize=3`, `readyLeaderReplicas=3`, `readyFollowerReplicas=3`, `state=Ready`
- `StatefulSet`: `ejcluster-leader 3/3`, `ejcluster-follower 3/3`
- Redis Cluster: `cluster_state:ok`, `cluster_known_nodes:6`, `cluster_size:3`

## 테스트 데이터 입력 및 사전 검증
실행 명령:

```bash
kubectl exec -n default ejcluster-leader-0 -c redis -- sh -lc '
  set -e
  for i in $(seq 1 180); do
    redis-cli -c SET qa:scaleup:20260213:$i value-$i >/dev/null
  done
  bad=0
  for i in $(seq 1 180); do
    v=$(redis-cli -c GET qa:scaleup:20260213:$i)
    if [ "$v" != "value-$i" ]; then
      echo "mismatch:$i:$v"
      bad=$((bad+1))
    fi
  done
  echo "precheck_total=180 bad=$bad"
'
```

결과:

```text
precheck_total=180 bad=0
```

## 스케일업 실행
실행 명령:

```bash
kubectl patch rediscluster ejcluster -n default --type merge -p '{"spec":{"clusterSize":6}}'
```

완료 추적:

```text
12:27:25Z spec=6 status=[6 3 Initializing RedisCluster is initializing followers] sts=[ejcluster-leader:6/6 ejcluster-follower:3/6 ]
12:27:45Z spec=6 status=[6 3 Initializing RedisCluster is initializing followers] sts=[ejcluster-leader:6/6 ejcluster-follower:5/6 ]
12:27:55Z spec=6 status=[6 6 Bootstrap RedisCluster is bootstrapping] sts=[ejcluster-leader:6/6 ejcluster-follower:6/6 ]
12:29:47Z spec=6 status=[6 6 Ready RedisCluster is ready] sts=[ejcluster-leader:6/6 ejcluster-follower:6/6 ]
```

## 스케일업 후 검증

### 1) 클러스터 상태
- `RedisCluster`: `6 6 6 Ready RedisCluster is ready`
- `StatefulSet`: `leader 6/6`, `follower 6/6`
- Redis Cluster:
  - `cluster_state:ok`
  - `cluster_known_nodes:12`
  - `cluster_size:6`
  - `cluster_slots_assigned:16384`

`redis-cli --cluster check` 결과 요약:
- `[OK] 300 keys in 6 masters.`
- `[OK] All nodes agree about slots configuration.`
- `[OK] All 16384 slots covered.`

### 2) 데이터 무결성
실행 명령:

```bash
kubectl exec -n default ejcluster-leader-0 -c redis -- sh -lc '
  set -e
  bad=0
  for i in $(seq 1 180); do
    v=$(redis-cli -c GET qa:scaleup:20260213:$i)
    if [ "$v" != "value-$i" ]; then
      echo "mismatch:$i:$v"
      bad=$((bad+1))
    fi
  done
  echo "postcheck_total=180 bad=$bad"
'
```

결과:

```text
postcheck_total=180 bad=0
```

### 3) PVC 생성 확인 (ordinal 3~5)
검증 대상:
- leader: `data-persistence`, `node-conf` for ordinal `3,4,5`
- follower: `data-persistence`, `node-conf` for ordinal `3,4,5`

결과:

```text
exists:data-persistence-ejcluster-leader-3
exists:node-conf-ejcluster-leader-3
exists:data-persistence-ejcluster-follower-3
exists:node-conf-ejcluster-follower-3
exists:data-persistence-ejcluster-leader-4
exists:node-conf-ejcluster-leader-4
exists:data-persistence-ejcluster-follower-4
exists:node-conf-ejcluster-follower-4
exists:data-persistence-ejcluster-leader-5
exists:node-conf-ejcluster-leader-5
exists:data-persistence-ejcluster-follower-5
exists:node-conf-ejcluster-follower-5
```

## 로그 관찰 요약
- follower 스케일업 구간에서 기존 follower(0~2)는 `already present`로 스킵됨.
- 새 follower(3~5)는 `add-node ... [OK] New node added correctly.`로 정상 추가됨.
- `Found Empty Redis Leader Node` 이후 `cluster slot fix completed` -> `rebalancing redis cluster empty masters completed` 순서로 진행됨.
- 최종적으로 `state=Ready`, `Cluster health check passed` 확인.

## 최종 판정
- 스케일업: **성공** (`3 -> 6`)
- 클러스터 상태: **정상**
- 데이터 유지: **성공** (180/180)
- PVC 생성: **성공** (ordinal 3~5 leader/follower PVC 생성 확인)
