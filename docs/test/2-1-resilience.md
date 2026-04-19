# 2-1 Resilience Test (Pod Failure)

테스트 일시: `2026-04-19 (KST)`  
네임스페이스/리소스: `default/hello`  
초기 상태: `clusterSize=3`, `leader=3`, `follower=3`, `cluster_state=ok`

## 목적
- leader 장애 시 follower 승격과 원복 경로가 정상 동작하는지 확인
- follower 단독 재시작 시 membership이 유지되는지 확인
- 클러스터 상태(`cluster_state=ok`, slot coverage)가 계속 유지되는지 확인

## 2.1 Pod Failure
- [x] Kill a leader pod -> verify Redis auto-failover promotes follower  
  판정: **성공**
- [x] Kill a follower pod -> verify it rejoins cluster after restart  
  판정: **성공**

## 사전 데이터 입력

장애 전 데이터 연속성 확인용 key를 미리 저장했습니다.

```bash
kubectl exec -n default hello-leader-0 -c redis -- \
  redis-cli set failover:test value-20260419
```

저장 직후 확인:

```bash
kubectl exec -n default hello-leader-0 -c redis -- \
  redis-cli get failover:test
```

출력:

```text
value-20260419
```

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
- 장애 이후에도 `failover:test` 조회가 정상 동작함.

데이터 확인:

```bash
kubectl exec -n default hello-follower-2 -c redis -- \
  redis-cli get failover:test
```

출력:

```text
value-20260419
```

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
- follower 재시작 이후에도 `failover:test=value-20260419`가 유지됨.

최종 데이터 확인:

```bash
kubectl exec -n default hello-leader-0 -c redis -- \
  redis-cli get failover:test
```

출력:

```text
value-20260419
```

## 결론
- leader 장애 시 follower 승격과 기존 leader의 replica 재조인이 정상 동작함
- follower 단독 재시작 시 membership이 깨지지 않고 정상 복귀함
- 테스트 전후 `cluster_state:ok`, `cluster_slots_ok:16384`가 유지됨
- 사전 입력한 테스트 key `failover:test=value-20260419`가 leader/follower 장애 이후에도 유지됨
