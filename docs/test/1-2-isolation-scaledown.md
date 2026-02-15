# 1-2-2 Downscale Guard Test (4 -> 3, overflow ordinal isolation)

테스트 일시: `2026-02-15 (UTC)` / `2026-02-16 (KST)`  
네임스페이스/리소스: `default/second5`

## 목적
- unhealthy overflow 노드가 있을 때 downscale membership cleanup을 보류하고, StatefulSet 축소를 막는지 검증
- unhealthy 상태 해소 후 cleanup/reshard/del-node가 재개되어 최종 `3/3`으로 수렴하는지 검증
- 다운스케일 전 입력한 데이터가 유지되는지 검증

## 사전 상태
- `spec.clusterSize=4`
- `status.readyLeaderReplicas=4`, `status.readyFollowerReplicas=4`, `state=Ready`
- `CLUSTER NODES`: 8노드(leader 4 + follower 4), 모두 `connected`
- 테스트 키 입력:
  - prefix: `predown:*`
  - count: `100` (`predown:1` ~ `predown:100`)

## 재현 절차
1. overflow ordinal 3 격리용 NetworkPolicy 적용
2. `leader-0`에서 `leader-3`로의 `6379`, `16379` 연결 타임아웃 확인
3. 다운스케일 요청: `spec.clusterSize 4 -> 3`
4. unhealthy 가드로 downscale 정리 보류 확인
5. 격리 NetworkPolicy 삭제
6. cleanup/reshard/del-node 재개 후 STS 축소 완료 확인
7. 데이터 무결성 검증

## 핵심 관찰 결과
### 1) unhealthy 가드 동작 확인
격리 상태에서 컨트롤러 로그:

```text
cluster has unhealthy nodes, delaying membership change
Action="leader downscale cleanup", Unhealthy.Node.Count=1
cluster has unhealthy nodes, delaying membership change
Action="leader downscale cleanup", Unhealthy.Node.Count=2
```

동일 시점 상태:
- CR: `spec=3` 이지만 `status=4/4/Ready`
- STS: `second5-leader 4/4`, `second5-follower 4/4`
- `CLUSTER NODES`: `leader-3`, `follower-3`가 `fail`로 표시되고 축소는 보류됨

즉, unhealthy 노드가 존재하는 동안 membership change/STS 축소가 먼저 진행되지 않음을 확인했습니다.

### 2) 격리 해제 후 cleanup 재개
NetworkPolicy 삭제 후 컨트롤러 로그:

```text
attached follower node ids ... ["069dd70d99f827124571f4b1971422bc14f20281"]
node removal completed ... nodeID=069dd70d99f827124571f4b1971422bc14f20281
reshard completed ... from=79f15541e0b7167e533dfab94a0bd44812ab7076 to=5729beed9d74c1f8def13827de4fd02d13568f5d slots=1
node removal completed ... nodeID=79f15541e0b7167e533dfab94a0bd44812ab7076
completed one overflow leader ordinal cleanup pass ... Ordinal=3
```

최종 수렴 상태:
- CR: `spec=3`, `status=3/3/Ready`
- STS: `second5-leader 3/3`, `second5-follower 3/3`
- Pod: `leader/follower-0..2`만 남고 `-3`은 제거됨
- `CLUSTER NODES`: 6노드(3 master + 3 replica), 모두 `connected`

## 데이터 무결성 검증
검증 결과:

```text
predown_keys present=100 missing=0
predown:1=v1
predown:50=v50
predown:100=v100
```

마스터별 `dbsize` 합계:

```text
10.244.120.88:6379 dbsize=34
10.244.120.84:6379 dbsize=31
10.244.120.65:6379 dbsize=35
total_master_dbsize=100
```

## 결론
- downscale guard는 unhealthy overflow 노드가 있을 때 축소를 안전하게 보류함
- unhealthy 해소 후 cleanup/reshard/del-node가 재개되고 최종 `3/3`으로 정상 수렴함
- 테스트 데이터 `100`개가 모두 유지되어 데이터 유실이 없음을 확인함
