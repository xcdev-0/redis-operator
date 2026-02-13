# 2 Resilience Test (Pod Failure)

테스트 일시: `2026-02-13`  
네임스페이스/리소스: `default/hello`  
초기 상태: `clusterSize=3`, `leader=3`, `follower=3`, `cluster_state=ok`

## 2.1 Pod Failure
- [x] Kill a leader pod -> verify Redis auto-failover promotes follower  
  판정: **성공**
- [x] Kill a follower pod -> verify it rejoins cluster after restart  
  판정: **성공**

## 2번 결과: Leader 삭제 시 자동 승격

실행:

```bash
kubectl delete pod -n default hello-leader-2
```

삭제 전 핵심 상태:

```text
881e... hello-leader-2 master 10923-16383
3806... hello-follower-2 slave -> 881e...
cluster_current_epoch:3
```

삭제 후 핵심 상태:

```text
3806... hello-follower-2 master 10923-16383
881e... hello-leader-2 slave -> 3806...
cluster_current_epoch:4
cluster_state:ok
```

요약:
- `hello-follower-2`가 `master`로 승격됨.
- 재기동된 `hello-leader-2`는 `slave`로 재합류.

## 3번 결과: Follower 삭제 후 재조인

실행:

```bash
kubectl delete pod -n default hello-follower-1
kubectl wait --for=condition=Ready pod/hello-follower-1 -n default --timeout=120s
```

확인 상태:

```text
a3aa... hello-follower-1 slave -> hello-leader-1
cluster_state:ok
cluster_slots_ok:16384
cluster_slots_fail:0
```

요약:
- `hello-follower-1` 재생성 후 정상 `slave`로 재조인됨.
- 클러스터는 테스트 내내 정상 상태 유지.
