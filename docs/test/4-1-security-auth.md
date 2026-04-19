# 4-1 Security Auth Test (Password + TLS)

테스트 일시: `2026-04-19 (KST)`  
네임스페이스/리소스: `default/mycluster`

## 목적
- 비밀번호 인증(`redisSecret`)이 정상 동작하는지 확인
- TLS 설정(`spec.TLS`)이 정상 동작하는지 확인
- 인증 정보 적용 후 exporter/probe 경로가 함께 유지되는지 확인

## 사전 조건
- 비밀번호 Secret: `redis-password-secret` (`password` 키)
- TLS Secret: `redis-tls-secret` (`ca.crt`, `tls.crt`, `tls.key`)
- `RedisCluster.spec.kubernetesConfig.redisSecret` 설정 완료
- `RedisCluster.spec.TLS` 설정 완료

### TLS 인증서/Secret 생성 (테스트용 self-signed)
이번 검증에서는 repo 내 스크립트를 사용해 self-signed 인증서를 만들고 Secret으로 등록했습니다.

```bash
kubectl apply -f sample-files/redis-password-secret.yaml

./sample-files/create-redis-tls-secret.sh default redis-tls-secret redis-cluster
```

검증:
```bash
kubectl get secret redis-password-secret redis-tls-secret -n default
```

### 인증서 검증 방식 정리 (이번 테스트 vs 운영)
- 이번 테스트는 **self-signed 1장**으로 검증했습니다.
  - `ca.crt`와 `tls.crt`에 같은 인증서를 사용
  - 목적: TLS 경로/인증 기능 동작 확인
- 운영(일반 CA 체인)에서는 아래처럼 구성하는 것을 권장합니다.
  - `ca.crt`: 신뢰할 루트 CA(또는 필요한 CA 번들)
  - `tls.crt`: 서버 인증서(필요 시 intermediate 포함 체인)
  - `tls.key`: 서버 개인키
- 참고:
  - 서버는 인증서를 클라이언트에 제시하고, 클라이언트는 `--cacert`로 서버 체인을 검증합니다.
  - 현재 설정(`tls-auth-clients optional`)에서는 mTLS가 아니므로 클라이언트 인증서(`--cert/--key`)는 필수가 아닙니다.

## 테스트 클러스터 배포

Helm values:
- `sample-files/mycluster-security-auth-values.yaml`

배포:

```bash
helm upgrade --install mycluster ./charts/redis-cluster \
  -n default \
  -f sample-files/mycluster-security-auth-values.yaml
```

최종 상태:

```bash
kubectl get rediscluster mycluster -n default -o wide
```

출력:

```text
NAME        CLUSTERSIZE   READYLEADERREPLICAS   READYFOLLOWERREPLICAS   STATE   AGE   REASON
mycluster   3             3                     3                       Ready   ...   RedisCluster is ready
```

## 4.1 Password 인증 검증

### 1) 무인증 접속 실패 확인
```bash
kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -h localhost -p 6379 --tls --cacert /tls/ca.crt ping
```

결과:
```text
NOAUTH Authentication required.
```

### 2) 비밀번호 인증 성공 확인
```bash
kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -h localhost -p 6379 -a password \
  --tls --cacert /tls/ca.crt ping
```

결과:
```text
PONG
```

### 3) Probe/Exporter 비밀번호 주입 확인
- readiness/liveness probe 명령에 `-a ${REDIS_PASSWORD}` 포함 확인
- exporter env에 `REDIS_PASSWORD`가 `redis-password-secret/password`에서 주입되는 것 확인
- exporter 로그에 `NOAUTH`, `WRONGPASS`가 발생하지 않는 것 확인

실제 probe 명령:

```text
["sh","-c","redis-cli -h $(hostname) -p ${REDIS_PORT} -a ${REDIS_PASSWORD} --tls --cert ${REDIS_TLS_CERT} --key ${REDIS_TLS_KEY} --cacert ${REDIS_TLS_CA_CERT} ping"]
```

exporter env 확인:

```text
REDIS_ADDR=rediss://localhost:6379
REDIS_EXPORTER_SKIP_TLS_VERIFICATION=true
REDIS_EXPORTER_TLS_CA_CERT_FILE=/tls/ca.crt
REDIS_EXPORTER_TLS_CLIENT_CERT_FILE=/tls/tls.crt
REDIS_EXPORTER_TLS_CLIENT_KEY_FILE=/tls/tls.key
REDIS_PASSWORD secret=redis-password-secret key=password
```

exporter 로그:

```text
Providing metrics at :9121/metrics
```

## 4.2 TLS 검증

### 1) TLS 스펙 및 환경 변수 확인
- `spec.TLS.secret.secretName=redis-tls-secret`
- Redis 컨테이너 env:
  - `TLS_MODE=true`
  - `REDIS_TLS_CA_CERT=/tls/ca.crt`
  - `REDIS_TLS_CERT=/tls/tls.crt`
  - `REDIS_TLS_KEY=/tls/tls.key`

### 2) Redis 설정 파일 확인
`/etc/redis/redis.conf` 핵심 설정:

```text
tls-cert-file /tls/tls.crt
tls-key-file /tls/tls.key
tls-ca-cert-file /tls/ca.crt
tls-replication yes
tls-cluster yes
port 0
tls-port 6379
```

### 2-1) Exporter TLS 환경 확인
Redis exporter도 TLS Redis에 붙을 수 있도록 아래 환경 변수가 렌더되는 것을 확인했습니다.

```text
REDIS_ADDR=rediss://localhost:6379
REDIS_EXPORTER_TLS_CLIENT_CERT_FILE=/tls/tls.crt
REDIS_EXPORTER_TLS_CLIENT_KEY_FILE=/tls/tls.key
REDIS_EXPORTER_TLS_CA_CERT_FILE=/tls/ca.crt
REDIS_EXPORTER_SKIP_TLS_VERIFICATION=true
```

### 3) TLS redis-cli 인증 성공
```bash
kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -h localhost -p 6379 -a password \
  --tls --cert /tls/tls.crt --key /tls/tls.key --cacert /tls/ca.crt ping

kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -h localhost -p 6379 -a password \
  --tls --cacert /tls/ca.crt ping
```

결과:
```text
PONG
```

### 4) 평문(비TLS) 접속 실패 확인
TLS 적용 후 `6379`는 TLS 포트이므로, 평문 접속은 실패하는 것이 정상:

```bash
kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -p 6379 -a password
```

결과 예:
```text
I/O error
Error: Connection reset by peer
```

## 연계 확인 포인트
- 비밀번호/TLS 적용 이후 exporter sidecar가 계속 떠 있어야 함
- `docs/test/6-1-monitoring.md` 기준으로 TLS 적용 후에도 Prometheus target `6/6 up`을 확인함
- 즉 password + TLS 설정이 exporter scrape 경로를 깨뜨리지 않음을 검증함

## 최종 판정
- 비밀번호 인증: **성공**
- TLS 인증: **성공**
- TLS 모드에서 평문 접속 거부: **정상 동작**
- exporter/probe 경로 유지: **성공**
- TLS 적용 후 Prometheus scrape 유지: **성공**
