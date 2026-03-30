# 4-1 Security Auth Test (Password + TLS)

테스트 일시: `2026-02-18`  
네임스페이스/리소스: `default/mycluster`

## 목적
- 비밀번호 인증(`redisSecret`)이 정상 동작하는지 확인
- TLS 설정(`spec.TLS`)이 정상 동작하는지 확인

## 사전 조건
- 비밀번호 Secret: `redis-password-secret` (`password` 키)
- TLS Secret: `redis-tls-secret` (`ca.crt`, `tls.crt`, `tls.key`)
- `RedisCluster.spec.kubernetesConfig.redisSecret` 설정 완료
- `RedisCluster.spec.TLS` 설정 완료

### TLS 인증서/Secret 생성 (테스트용 self-signed)
아래처럼 self-signed 인증서를 만든 뒤 Kubernetes Secret으로 등록했습니다.

```bash
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 365 \
  -keyout /tmp/redis-tls.key -out /tmp/redis-tls.crt \
  -subj "/CN=redis-cluster"

kubectl create secret generic redis-tls-secret -n default \
  --from-file=ca.crt=/tmp/redis-tls.crt \
  --from-file=tls.crt=/tmp/redis-tls.crt \
  --from-file=tls.key=/tmp/redis-tls.key \
  --dry-run=client -o yaml | kubectl apply -f -
```

검증:
```bash
kubectl get secret redis-tls-secret -n default
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

## 4.1 Password 인증 검증

### 1) 무인증 접속 실패 확인
```bash
kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -h localhost -p 6379 ping
```

결과:
```text
NOAUTH Authentication required.
```

### 2) 비밀번호 인증 성공 확인
```bash
kubectl exec -n default mycluster-leader-0 -c redis -- \
  redis-cli -h localhost -p 6379 -a password ping
```

결과:
```text
PONG
```

### 3) Probe/Exporter 비밀번호 주입 확인
- readiness/liveness probe 명령에 `-a ${REDIS_PASSWORD}` 포함 확인
- exporter env에 `REDIS_PASSWORD`가 `redis-password-secret/password`에서 주입되는 것 확인

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
kubectl exec -it -n default mycluster-leader-0 -c redis -- \
  redis-cli -p 6379 -a password
```

결과 예:
```text
I/O error
Error: Connection reset by peer
```

## 최종 판정
- 비밀번호 인증: **성공**
- TLS 인증: **성공**
- TLS 모드에서 평문 접속 거부: **정상 동작**
