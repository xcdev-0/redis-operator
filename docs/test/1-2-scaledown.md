# 1-2 Downscale Test (4 -> 3)

테스트 일시: `2026-02-16`  
네임스페이스/리소스: `default/second4`

## 목적
- 다운스케일 후 클러스터 상태가 정상인지 확인
- 다운스케일 전 입력한 데이터가 유지되는지 확인

## 사전 상태
- `spec.clusterSize=4`
- `status.readyLeaderReplicas=4`, `status.readyFollowerReplicas=4`
- `CLUSTER INFO`
  - `cluster_state:ok`
  - `cluster_known_nodes:8`
  - `cluster_size:4`
- `CLUSTER NODES`
  - leader 4개, follower 4개 모두 `connected`

## 테스트 데이터 준비
downscale 전 아래 키를 저장했습니다.
- prefix: `ds:downscale:*`
- count: 40개 (`ds:downscale:1` ~ `ds:downscale:40`)
- 샘플 검증 키:
  - `ds:downscale:1=value-1`
  - `ds:downscale:7=value-7`
  - `ds:downscale:13=value-13`
  - `ds:downscale:21=value-21`
  - `ds:downscale:34=value-34`
  - `ds:downscale:40=value-40`

## 실행
- `kubectl patch rediscluster -n default second4 --type=merge -p '{"spec":{"clusterSize":3}}'`

## 결과
- CR 상태
  - `spec.clusterSize=3`
  - `status.readyLeaderReplicas=3`
  - `status.readyFollowerReplicas=3`
  - `status.state=Ready`
- StatefulSet 상태
  - `second4-leader: 3/3`
  - `second4-follower: 3/3`
- Pod 상태
  - leader `0..2` Running
  - follower `0..2` Running

## 다운스케일 후 클러스터 검증
- `CLUSTER INFO`
  - `cluster_state:ok`
  - `cluster_known_nodes:6`
  - `cluster_size:3`
  - `cluster_slots_fail:0`
  - `cluster_slots_pfail:0`
- `CLUSTER NODES`
  - leader 3개, follower 3개
  - 모든 노드 `connected`
  - slot 16384개 정상 분배

## 데이터 무결성 검증
downscale 후 동일 샘플 키 조회 결과:
- `1=value-1`
- `7=value-7`
- `13=value-13`
- `21=value-21`
- `34=value-34`
- `40=value-40`

## 결론
이번 4->3 다운스케일 테스트에서:
- 클러스터 토폴로지 정상 수렴
- 노드 상태 정상(`connected`, `cluster_state:ok`)
- 샘플 데이터 유실 없음

즉, 현재 downscale 경로는 본 시나리오에서 정상 동작함을 확인했습니다.
