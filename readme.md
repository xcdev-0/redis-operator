

## TODO (현재 문제상황)

- [ ] `GetExecutionPodName()`의 `leader-0` 고정 의존 제거
  - 대상: `internal/k8sutils/k8smeta/names.go`, `internal/k8sutils/cluster/exec_cluster.go`
  - 목표: 실행 Pod를 `healthy leader` 우선으로 동적 선택 (fallback: leader-1, leader-2 ...)

- [ ] `RepairDisconnectedNodes()`에서 실행 Pod 장애 시 fallback 적용
  - 대상: `internal/k8sutils/cluster/exec_cluster.go`
  - 목표: `leader-0` 단절/장애 상황에서도 `CLUSTER MEET` 복구 수행 가능하게 개선

- [ ] `ConfigureRedisClient()` nil 반환 안전 처리
  - 대상: `internal/k8sutils/redisservice/client.go`, `internal/k8sutils/cluster/exec_cluster.go`
  - 목표: `defer redisClient.Close()` nil panic 방지 및 명확한 에러 반환

- [ ] `--cluster fix/rebalance` 경로도 실행 Pod fallback 공통화
  - 대상: `FixRedisClusterOpenSlots`, `RebalanceRedisCluster`, `RebalanceRedisClusterEmptyMasters`
  - 목표: 특정 leader 단일 장애로 운영 명령이 멈추지 않게 보강

- [ ] E2E 테스트 추가 (자동화)
  - `leader-0` 단절 시 `RepairDisconnectedNodes` 성공
  - `majority unhealthy` 시 `"manual intervention required"` 로그 검증
  - 복구 후 `cluster_state:ok`, `cluster_slots_fail:0` 검증
