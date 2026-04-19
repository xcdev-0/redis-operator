# 6-1 Monitoring Test (Redis Exporter + Prometheus)

테스트 일시: `2026-04-19 (KST)`  
Redis 네임스페이스/리소스: `default/mycluster`  
Prometheus 네임스페이스: `monitoring`

## 목적
- `redis_exporter` sidecar가 leader/follower Pod에 정상 주입되는지 확인
- headless Service가 exporter 포트(`9121`)와 scrape label을 노출하는지 확인
- `ServiceMonitor`와 `PrometheusRule`가 생성되는지 확인
- Prometheus target discovery와 실제 Redis metric 조회가 성공하는지 확인

## 사전 조건
- `redisCluster.spec.redisExporter.enabled=true`
- `serviceMonitor.enabled=true`
- `prometheusRule.enabled=true`
- `kube-prometheus-stack`가 `monitoring` 네임스페이스에 설치되어 있음
- Prometheus가 `release: prometheus` 라벨이 붙은 `ServiceMonitor`를 선택하도록 설정되어 있음

이번 검증에서 사용한 값 파일:

```text
sample-files/kube-prometheus-minimal-values.yaml
sample-files/mycluster-monitoring-values.yaml
```

설치/업그레이드:

```bash
helm upgrade --install prometheus ./kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f sample-files/kube-prometheus-minimal-values.yaml

helm upgrade --install mycluster ./charts/redis-cluster \
  -n default \
  -f sample-files/mycluster-security-auth-values.yaml \
  -f sample-files/mycluster-monitoring-values.yaml
```

## 1) Exporter sidecar 주입 확인
확인 명령:

```bash
kubectl get pod -n default mycluster-leader-0 \
  -o jsonpath='{.spec.containers[*].name}{"\n"}'
```

판정 기준:
- `redis`
- `redis-exporter`

두 컨테이너가 모두 보여야 함

## 2) Headless Service 메트릭 포트/라벨 확인
확인 명령:

```bash
kubectl get svc -n default mycluster-leader-headless mycluster-follower-headless --show-labels
```

핵심 확인 포인트:
- 포트에 `6379/TCP,9121/TCP`가 함께 노출되는지
- 라벨에 `redis.ej.com/metrics-scrape=true`가 포함되는지

의미:
- exporter는 headless Service를 통해 Prometheus scrape 대상이 됨
- 일반 role Service(`mycluster-leader`, `mycluster-follower`)는 exporter 포트를 노출하지 않음

## 3) ServiceMonitor / PrometheusRule 생성 확인
확인 명령:

```bash
kubectl get servicemonitor -n monitoring mycluster-exporter
kubectl get prometheusrule -n default mycluster-alerts
```

확인 결과:
- `ServiceMonitor`: `monitoring/mycluster-exporter`
- `PrometheusRule`: `default/mycluster-alerts`

## 4) Prometheus target discovery 확인
Prometheus API로 확인:

```bash
kubectl get --raw \
  '/api/v1/namespaces/monitoring/services/http:prometheus-operated:9090/proxy/api/v1/targets'
```

확인 결과:
- `mycluster-leader-headless`: 3 target `up`
- `mycluster-follower-headless`: 3 target `up`

즉 leader 3개 + follower 3개, 총 6개 exporter endpoint가 모두 scrape 성공 상태였음

## 5) Prometheus query 확인
Prometheus API에서 아래 쿼리를 실행:

```promql
redis_cluster_connections{job=~"mycluster-(leader|follower)-headless"}
```

결과 예:

```text
redis_cluster_connections{pod="mycluster-leader-0",service="mycluster-leader-headless"} 10
redis_cluster_connections{pod="mycluster-leader-1",service="mycluster-leader-headless"} 10
redis_cluster_connections{pod="mycluster-leader-2",service="mycluster-leader-headless"} 10
redis_cluster_connections{pod="mycluster-follower-0",service="mycluster-follower-headless"} 10
redis_cluster_connections{pod="mycluster-follower-1",service="mycluster-follower-headless"} 10
redis_cluster_connections{pod="mycluster-follower-2",service="mycluster-follower-headless"} 10
```

판정:
- 총 `6` series가 조회됨
- 모든 Redis 인스턴스에서 exporter metric이 수집되고 있음을 확인

## 6) Redis alert rule 생성 확인
`mycluster-alerts` 규칙에는 아래 알람이 포함됩니다.

- `RedisInstanceDown`
- `RedisClusterStateFail`
- `RedisMemoryUsageHigh`
- `RedisRejectedConnectionsHigh`
- `RedisEvictedKeysHigh`

즉, exporter scrape뿐 아니라 기본 Redis 운영 알람 템플릿도 함께 배포됨

## 현재 한계
- Redis chart는 `ServiceMonitor`와 `PrometheusRule`까지 생성함
- 반면 operator chart는 controller-manager metrics Service만 만들고, Helm 기준 `ServiceMonitor`는 만들지 않음
- 따라서 operator metrics를 Prometheus로 수집하려면 별도 `ServiceMonitor` 또는 수동 scrape 설정이 필요함

## 최종 판정
- Redis exporter sidecar 주입: **성공**
- headless Service 메트릭 노출: **성공**
- ServiceMonitor / PrometheusRule 생성: **성공**
- Prometheus target discovery: **성공**
- Prometheus metric query: **성공**
