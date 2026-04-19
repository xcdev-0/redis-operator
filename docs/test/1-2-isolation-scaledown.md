# 1-2-2 Downscale Guard Test (4 -> 3, overflow ordinal isolation)

테스트 일시: `2026-04-19 (KST)`  
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
- 전제:
  - 클러스터에 NetworkPolicy 구현체(Calico, Cilium 등)가 활성화되어 있어야 함

## 격리용 NetworkPolicy
overflow ordinal 3만 격리하기 위해 아래 정책을 사용했습니다.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: second5-overflow-ordinal-isolation
  namespace: default
spec:
  podSelector:
    matchExpressions:
      - key: statefulset.kubernetes.io/pod-name
        operator: In
        values:
          - second5-leader-3
          - second5-follower-3
  policyTypes:
    - Ingress
    - Egress
```

## 재현 절차
1. `charts/redis-cluster`로 `second5(clusterSize=4)` 설치
2. NetworkPolicy 적용
3. `CLUSTER NODES` 또는 컨트롤러 로그에서 `leader-3`, `follower-3`가 unhealthy로 전환되는지 확인
4. 다운스케일 요청: `spec.clusterSize 4 -> 3`
5. unhealthy 가드로 downscale 정리 보류 확인
6. NetworkPolicy 삭제
7. cleanup/reshard/del-node 재개 후 STS 축소 완료 확인
8. 데이터 무결성 검증

실행 명령 예시:

```bash
helm upgrade --install second5 ./charts/redis-cluster \
  -n default \
  -f sample-files/second5-isolation-scaledown-values.yaml

kubectl apply -f /tmp/second5-overflow-ordinal-isolation.yaml

kubectl exec -n default second5-leader-0 -c redis -- \
  redis-cli cluster nodes

kubectl patch rediscluster second5 -n default --type merge \
  -p '{"spec":{"clusterSize":3}}'

kubectl logs -n default deploy/rediscluster-controller-manager -c manager --tail=200 -f

kubectl delete networkpolicy second5-overflow-ordinal-isolation -n default
```

## 핵심 관찰 결과
### 1) unhealthy 가드 동작 확인
격리 상태에서 컨트롤러 로그:

```text
failed to check redis role, skipping pod ... second5-follower-3 ... i/o timeout
failed to check redis role, skipping pod ... second5-leader-3 ... i/o timeout
overflow ordinal still joined in cluster membership
downscale cleanup is not completed yet; delaying StatefulSet replica reconcile
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
second5-leader-0 dbsize=28
second5-leader-1 dbsize=37
second5-leader-2 dbsize=35
sum=100
```

## 결론
- downscale guard는 unhealthy overflow 노드가 있을 때 축소를 안전하게 보류함
- unhealthy 해소 후 cleanup/reshard/del-node가 재개되고 최종 `3/3`으로 정상 수렴함
- 테스트 데이터 `100`개가 모두 유지되어 데이터 유실이 없음을 확인함
