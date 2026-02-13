# 1-2 Downscale Test (5 -> 4)

테스트 일시: `2026-02-13`  
네임스페이스/리소스: `default/ejcluster`

## 목적
- 다운스케일 후 클러스터 상태가 정상인지 확인
- 다운스케일 전 입력한 데이터가 유지되는지 확인
- 제거 대상 ordinal PVC가 자동 삭제되는지 확인

## 사전 상태
- `RedisCluster`: `clusterSize=5`, `readyLeaderReplicas=5`, `readyFollowerReplicas=5`, `state=Ready`
- `StatefulSet`: `ejcluster-leader 5/5`, `ejcluster-follower 5/5`
- Redis Cluster: `cluster_state:ok`, `cluster_known_nodes:10`, `cluster_size:5`
- PVC: leader/follower 각 ordinal `0~4`의 `data-persistence`, `node-conf` 존재

## 테스트 데이터 입력 및 사전 검증
실행 명령:

```bash
kubectl exec -n default ejcluster-leader-0 -c redis -- sh -lc '
  set -e
  for i in $(seq 1 120); do
    redis-cli -c SET qa:downscale:20260213:$i value-$i >/dev/null
  done
  bad=0
  for i in $(seq 1 120); do
    v=$(redis-cli -c GET qa:downscale:20260213:$i)
    if [ "$v" != "value-$i" ]; then
      echo "mismatch:$i:$v"
      bad=$((bad+1))
    fi
  done
  echo "precheck_total=120 bad=$bad"
'
```

결과:

```text
precheck_total=120 bad=0
```

## 다운스케일 실행
실행 명령:

```bash
kubectl patch rediscluster ejcluster -n default --type merge -p '{"spec":{"clusterSize":4}}'
```

완료 추적:

```text
12:21:05Z spec=4 status=[5 5 Ready] sts=[ejcluster-leader:5/5 ejcluster-follower:5/5 ]
12:21:16Z spec=4 status=[5 5 Initializing] sts=[ejcluster-leader:4/4 ejcluster-follower:5/5 ]
12:21:26Z spec=4 status=[4 4 Ready] sts=[ejcluster-leader:4/4 ejcluster-follower:4/4 ]
```

## 다운스케일 후 검증

### 1) 클러스터 상태
- `StatefulSet`: `leader 4/4`, `follower 4/4`
- `RedisCluster`: `4 4 4 Ready RedisCluster is ready`
- Redis Cluster:
  - `cluster_state:ok`
  - `cluster_known_nodes:8`
  - `cluster_size:4`
  - `cluster_slots_assigned:16384`

`redis-cli --cluster check` 결과 요약:
- `[OK] 120 keys in 4 masters.`
- `[OK] All nodes agree about slots configuration.`
- `[OK] All 16384 slots covered.`

### 2) 데이터 무결성
실행 명령:

```bash
kubectl exec -n default ejcluster-leader-0 -c redis -- sh -lc '
  set -e
  bad=0
  for i in $(seq 1 120); do
    v=$(redis-cli -c GET qa:downscale:20260213:$i)
    if [ "$v" != "value-$i" ]; then
      echo "mismatch:$i:$v"
      bad=$((bad+1))
    fi
  done
  echo "postcheck_total=120 bad=$bad"
'
```

결과:

```text
postcheck_total=120 bad=0
```

### 3) PVC 삭제 확인 (ordinal 4)
검증 대상:
- `data-persistence-ejcluster-leader-4`
- `node-conf-ejcluster-leader-4`
- `data-persistence-ejcluster-follower-4`
- `node-conf-ejcluster-follower-4`

결과:

```text
deleted:data-persistence-ejcluster-leader-4
deleted:node-conf-ejcluster-leader-4
deleted:data-persistence-ejcluster-follower-4
deleted:node-conf-ejcluster-follower-4
```

## 최종 판정
- 다운스케일: **성공** (`5 -> 4`)
- 클러스터 상태: **정상**
- 데이터 유지: **성공** (120/120)
- PVC 정리: **성공** (ordinal 4 leader/follower PVC 삭제 확인)
