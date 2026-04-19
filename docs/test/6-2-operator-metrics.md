# 6-2 Operator Metrics Test (controller-manager + Prometheus)

테스트 일시: `2026-04-19 (KST)`  
Operator 네임스페이스/리소스: `default/redis-operator`  
Prometheus 네임스페이스: `monitoring`

## 목적
- `redis-operator` controller-manager metrics Service가 실제로 Prometheus에 scrape 되는지 확인
- operator chart에서 `ServiceMonitor`를 Helm으로 관리할 수 있도록 정리
- `controller_runtime_*` 계열 메트릭 조회가 가능한지 확인

## 사전 상태
- `kube-prometheus-stack` 설치 완료
- Prometheus Service는 `NodePort`로 노출
  - `http://192.168.0.57:30090`
- Redis operator는 Helm release `redis-operator`로 배포 중

## 이번 검증에서 추가한 내용

### 1) operator chart에 Helm-managed ServiceMonitor 추가
추가 파일:

```text
charts/redis-operator/templates/service-monitor.yaml
sample-files/redis-operator-monitoring-values.yaml
```

핵심 values:

```yaml
serviceMonitor:
  enabled: true
  namespace: monitoring
  port: http
  interval: 30s
  path: /metrics
  scheme: http
  bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
  additionalLabels:
    release: prometheus
  namespaceSelector:
    matchNames:
      - default
```

### 2) metrics Service 포트 이름 수정
초기 상태에서는 metrics endpoint가 실제로는 HTTP인데 Service port 이름이 `https`로 되어 있어,
Prometheus generated config가 `scheme: https`로 고정되는 문제가 있었습니다.

수정:

```yaml
metricsService:
  ports:
    - name: http
      port: 8443
      targetPort: 8443
```

즉:
- 포트는 그대로 `8443`
- 실제 scheme은 `http`
- `ServiceMonitor`도 `port: http`, `scheme: http`로 맞춤

## 적용 명령

```bash
helm upgrade --install redis-operator ./charts/redis-operator \
  -n default \
  -f sample-files/redis-operator-monitoring-values.yaml \
  --set controllerManager.manager.image.repository=docker.io/spotifyyyyy/redis-operator \
  --set controllerManager.manager.image.tag=dev-20260419133716 \
  --set controllerManager.manager.env.initContainerImage=docker.io/spotifyyyyy/redis-operator:dev-20260419133716
```

## 생성 리소스 확인

```bash
kubectl get svc rediscluster-controller-manager-metrics-service -n default
kubectl get servicemonitor -n monitoring
```

확인 결과:
- Service: `default/rediscluster-controller-manager-metrics-service`
- ServiceMonitor: `monitoring/rediscluster-controller-manager-metrics-servicemonitor`

## Prometheus scrape 확인

Prometheus target API:

```bash
kubectl get --raw \
  '/api/v1/namespaces/monitoring/services/http:prometheus-operated:9090/proxy/api/v1/targets'
```

결과:

```text
instance=10.244.194.27:8443
scrapeUrl=http://10.244.194.27:8443/metrics
health=up
lastError=""
```

`up` query:

```promql
up{job="rediscluster-controller-manager-metrics-service"}
```

결과:

```text
10.244.194.27:8443 => 1
```

## 메트릭 조회 확인

예시 쿼리:

```promql
controller_runtime_reconcile_total
```

결과 예:

```text
controller="cluster", result="success"       60
controller="cluster", result="requeue_after" 265
controller="cluster", result="error"         0
controller="cluster", result="requeue"       0
```

즉 operator controller-runtime 메트릭이 실제로 수집되고 있음을 확인했습니다.

## 결론
- operator metrics Service scrape: **성공**
- Helm-managed ServiceMonitor 추가: **성공**
- Prometheus target `up=1` 확인: **성공**
- `controller_runtime_reconcile_total` 조회: **성공**
