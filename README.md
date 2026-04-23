# Redis Cluster Operator

Kubernetes 위에서 Redis Cluster를 생성하고 운영하기 위한 Operator 프로젝트입니다.  
`RedisCluster` Custom Resource를 기준으로 leader / follower StatefulSet, headless Service, 모니터링 리소스를 관리합니다.

- 클러스터 생성
- scale up / scale down
- leader / follower 재조인
- 장애 복구
- 인증 / TLS
- 모니터링 연동

## 핵심 기능

### 1. Redis Cluster 생명주기 관리
- `RedisCluster` CR 기준으로 leader / follower 리소스 생성
- 16384 slot 기준 클러스터 bootstrap
- scale up 시 `add-node` 및 rebalance 수행
- scale down 시 follower 정리, slot migration, node removal 순서 제어

### 2. 안정적인 downscale 처리
- unhealthy overflow 노드가 있으면 downscale cleanup을 먼저 끝낼 때까지 StatefulSet 축소를 보류
- membership cleanup과 StatefulSet 삭제 순서가 어긋나지 않도록 guard 로직 적용

### 3. 복구 및 재조인
- leader 장애 시 Redis auto-failover 이후 상태 수렴
- follower 재시작 후 자동 재조인
- Pod 재생성으로 IP가 바뀌어도 membership 복구 지원

### 4. 보안
- existing Secret 기반 비밀번호 인증 지원
- TLS 활성화 지원
- health probe / exporter / redis-cli 경로까지 인증 정보 반영

### 5. 모니터링
- Redis exporter sidecar 지원
- `ServiceMonitor`, `PrometheusRule` 생성 지원
- operator metrics scrape 지원

## 리소스 구조

이 Operator는 기본적으로 다음 리소스를 관리합니다.

- `RedisCluster` CRD
- leader StatefulSet
- follower StatefulSet
- headless Service
- exporter / monitoring 리소스

즉, Redis Cluster membership과 Kubernetes 리소스 상태를 함께 맞춰 가는 구조입니다.

## 빠른 시작

### 1. Operator 설치

```bash
helm upgrade --install redis-operator ./charts/redis-operator -n default
```

### 2. Redis Cluster 생성

```bash
helm upgrade --install mycluster ./charts/redis-cluster \
  -n default \
  -f sample-files/hello-resilience-values.yaml
```

### 3. 상태 확인

```bash
kubectl get rediscluster
kubectl get pod
kubectl exec -n default mycluster-leader-0 -c redis -- redis-cli cluster nodes
```

## 샘플 값 파일

자주 사용하는 예시는 `sample-files`에 정리해두었습니다.

- [기본 복구 테스트 값](./sample-files/hello-resilience-values.yaml)
- [downscale isolation 테스트 값](./sample-files/second5-isolation-scaledown-values.yaml)
- [비밀번호 인증 값](./sample-files/mycluster-security-auth-values.yaml)
- [모니터링 값](./sample-files/mycluster-monitoring-values.yaml)
- [operator metrics 값](./sample-files/redis-operator-monitoring-values.yaml)
- [kube-prometheus 최소 값](./sample-files/kube-prometheus-minimal-values.yaml)

## 검증 문서

기능별 검증 과정은 `docs/test`에 정리해두었습니다.

### 클러스터 생명주기
- [scale up](./docs/test/1-1-scaleup.md)
- [scale down](./docs/test/1-2-scaledown.md)
- [isolation scaledown guard](./docs/test/1-2-isolation-scaledown.md)

### 복구 / 장애 대응
- [pod failure / failover](./docs/test/2-1-resilience.md)
- [pod restart with IP change](./docs/test/2-2-restart.md)
- [unhealthy cluster 처리](./docs/test/2-3-unhealthy.md)

### 보안 / 모니터링
- [password authentication](./docs/test/4-1-security-auth.md)
- [redis exporter + prometheus](./docs/test/6-1-monitoring.md)
- [operator metrics](./docs/test/6-2-operator-metrics.md)

전체 체크리스트는 여기서 볼 수 있습니다.

- [test checklist](./docs/test-checklist.md)

## 참고

- CRD 정의: [config/crd/bases/ejlabs.in_redisclusters.yaml](./config/crd/bases/ejlabs.in_redisclusters.yaml)
- RedisCluster 타입: [api/v1beta2/rediscluster_types.go](./api/v1beta2/rediscluster_types.go)
- 메인 컨트롤러: [internal/controller/rediscluster_controller.go](./internal/controller/rediscluster_controller.go)
