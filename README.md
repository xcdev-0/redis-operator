# Redis Cluster Operator for Kubernetes

> **Production-Ready Redis Cluster Management**  
> Kubernetes Operator for automating Redis Cluster deployment, scaling, and lifecycle management.

[![Go Version](https://img.shields.io/badge/Go-1.25-blue.svg)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.35-blue.svg)](https://kubernetes.io/)
[![Redis](https://img.shields.io/badge/Redis-7.x-red.svg)](https://redis.io/)
[![Controller Runtime](https://img.shields.io/badge/Controller--Runtime-0.22-green.svg)](https://github.com/kubernetes-sigs/controller-runtime)

---

## 📖 목차

- [개요](#-개요)
- [핵심 기능](#-핵심-기능)
- [Redis 클러스터 기술](#-redis-클러스터-기술)
- [아키텍처](#-아키텍처)
- [기술 스택](#-기술-스택)
- [빠른 시작](#-빠른-시작)
- [설정 가이드](#-설정-가이드)
- [고급 기능](#-고급-기능)

---

## 🎯 개요

**Redis Cluster Operator**는 Kubernetes에서 Redis 클러스터를 자동으로 배포하고 관리하는 Operator입니다. Kubernetes의 **CustomResourceDefinition (CRD)**를 사용하여 Redis 클러스터를 선언적으로 정의하고, Operator가 자동으로 클러스터의 **생성, 스케일링, 페일오버, 복구**를 담당합니다.

### ✨ 주요 특징

- 🚀 **완전 자동화된 클러스터 관리** - 복잡한 Redis 클러스터 운영을 간단한 YAML로 해결
- 📈 **무중단 스케일링** - 슬롯 리샤딩과 자동 리밸런싱으로 안전한 스케일 아웃/인
- 🔄 **자동 페일오버** - 장애 노드 감지 및 자동 복구
- 🔐 **엔터프라이즈 보안** - TLS 암호화, ACL 인증, Password 보호
- 💾 **데이터 영속화** - RDB 스냅샷 + AOF 로그를 통한 완벽한 데이터 보호
- 📊 **모니터링 지원** - Prometheus Exporter 통합
- 🎛️ **세밀한 제어** - Leader/Follower 역할별 독립적인 리소스 및 설정 관리

---

## 🚀 핵심 기능

### 1. 자동 클러스터 생성 및 부트스트랩

Operator가 Redis 클러스터 초기화를 완전 자동화합니다:

- **StatefulSet 생성**: Leader/Follower 역할별로 분리된 StatefulSet 배포
- **네트워크 설정**: Headless Service를 통한 안정적인 DNS 기반 통신
- **클러스터 초기화**: `redis-cli --cluster create` 자동 실행
- **슬롯 할당**: 16384개 슬롯을 Master 노드에 자동 분배
- **Replication 구성**: Follower 노드를 자동으로 Master에 연결

```yaml
apiVersion: ejlabs.in/v1beta2
kind: RedisCluster
metadata:
  name: my-redis-cluster
spec:
  clusterSize: 3                    # 3 Master 노드
  redisLeader:
    replicaCount: 3
  redisFollower:
    replicaCount: 3                 # 각 Master당 1개의 Replica
```

위 설정만으로 **3-Master, 3-Replica 클러스터**가 자동 생성됩니다!

---

### 2. 스케일 아웃 (Scale Out)

**동적으로 Master 노드를 추가하고 슬롯을 자동 재분배**합니다.

#### 📐 스케일 아웃 프로세스

```yaml
# 3개 → 5개로 확장
spec:
  clusterSize: 5
  redisLeader:
    replicaCount: 5
```

**자동 실행 단계:**

1. **새 Master 노드 추가**
   ```bash
   redis-cli --cluster add-node <new-node> <existing-node>
   ```
   - 새로운 StatefulSet Pod 생성
   - 클러스터에 Empty Master로 참여

2. **슬롯 리밸런싱**
   ```bash
   redis-cli --cluster rebalance <endpoint> --cluster-use-empty-masters
   ```
   - 기존 Master 노드들의 슬롯을 새 노드로 재분배
   - **Round-robin 방식**으로 균등하게 분산
   - 예: 3→5 확장 시 각 노드가 ~3277개 슬롯 보유

3. **Follower 자동 연결**
   - 새 Master에 Replica 노드 자동 연결
   - Replication 구성 완료

#### 💡 기술적 특징

- **무중단 마이그레이션**: 슬롯 이동 중에도 서비스 지속
- **점진적 재분배**: 한 번에 하나의 슬롯씩 안전하게 이동
- **자동 리밸런싱**: Empty Master 감지 시 자동으로 슬롯 분배

---

### 3. 스케일 인 (Scale Down)

**안전하게 Master 노드를 제거하고 슬롯을 재분배**합니다.

#### 📉 스케일 인 프로세스

```yaml
# 5개 → 3개로 축소
spec:
  clusterSize: 3
  redisLeader:
    replicaCount: 3
```

**자동 실행 단계:**

1. **역할 검증 및 Failover**
   ```go
   // StatefulSet의 "leader" Pod이지만 실제 Redis에서 Replica일 수 있음
   if !isLeaderNode(pod) {
       clusterFailover(pod)  // Replica를 Master로 승격
   }
   ```
   - 제거할 Pod의 실제 Redis 역할 확인
   - Replica인 경우 **Cluster Failover**로 Master 승격

2. **Follower 제거**
   ```bash
   redis-cli --cluster del-node <endpoint> <follower-node-id>
   ```
   - 해당 샤드에 연결된 모든 Replica 노드 먼저 제거
   - Master 제거 전 안전성 확보

3. **슬롯 리샤딩 (Resharding)**
   ```bash
   redis-cli --cluster reshard <endpoint> \
     --cluster-from <remove-node-id> \
     --cluster-to <target-node-id> \
     --cluster-slots <slot-count> \
     --cluster-yes
   ```
   - 제거할 노드의 모든 슬롯을 남은 노드로 이동
   - **Round-robin 타겟 선택**: `shardIdx % remainingNodes`
   - 예: 샤드 4 제거 시 → 노드 1 (4%3=1)로 슬롯 이동

4. **Master 제거**
   ```bash
   redis-cli --cluster del-node <endpoint> <master-node-id>
   ```
   - 슬롯이 없는 상태에서 Master 노드 제거

5. **최종 리밸런싱**
   ```bash
   redis-cli --cluster rebalance <endpoint>
   ```
   - 전체 클러스터 슬롯 재분배로 균형 복구
   - 각 Master가 동일한 수의 슬롯 보유

#### ⚠️ 안전 장치

- **StatefulSet Ready 확인**: 불안정한 상태에서는 다운스케일 차단
- **노드 수 일치 검증**: 실제 클러스터 Master 수 ≠ StatefulSet Replicas → 스킵
- **역순 제거**: 마지막 샤드부터 제거하여 인덱스 충돌 방지

---

### 4. 자동 페일오버 및 복구

#### 🔄 장애 감지 및 복구

**비정상 노드 감지:**
```go
unhealthyNodeCount := checkUnhealthyNodes()
if unhealthyNodeCount > 0 {
    // 1. Disconnected Master 복구 시도
    repairDisconnectedNodes()
    
    // 2. 복구 검증 (3회 재시도, 5초 간격)
    retry.Do(func() error {
        return verifyClusterHealth()
    }, retry.Attempts(3), retry.Delay(5*time.Second))
}
```

**Disconnected 노드 복구:**
```bash
# 모든 Pod IP에 CLUSTER MEET 재실행
redis-cli CLUSTER MEET <pod-ip> <port>
```
- Pod IP 변경 시 자동으로 클러스터 재연결
- 네트워크 분할(Network Partition) 복구

#### 🚨 Cluster Health Check

```bash
redis-cli --cluster check 127.0.0.1:6379
```

**정상 상태 검증 기준:**
- ✅ `[OK] xxx keys in xxx masters`
- ✅ `[OK] All nodes agree about slots configuration`
- ✅ `[OK] All 16384 slots covered`

3개의 `[OK]` 메시지 확인 시 Ready 상태로 전환

#### 💔 심각한 장애 처리

```go
// 대부분의 노드가 실패한 경우 (전체 - 1개 이상)
if unhealthyCount >= totalNodes - 1 {
    // 자동 복구 불가 → Manual Intervention Required
    return error("cluster broken: manual intervention required")
}
```

---

### 5. 슬롯 리밸런싱

#### ⚖️ 자동 리밸런싱 트리거

1. **Empty Master 감지**
   ```go
   // 슬롯이 없는 Master 발견 시 자동 리밸런싱
   if emptyMasterExists() {
       rebalanceCluster()
   }
   ```

2. **다운스케일 후**
   - 슬롯 리샤딩 후 불균등 분배 해소

3. **수동 리밸런싱**
   ```bash
   kubectl annotate rediscluster my-cluster rebalance=true
   ```

#### 📊 리밸런싱 알고리즘

```bash
redis-cli --cluster rebalance <endpoint>
```

**동작 원리:**
- 각 Master의 슬롯 수 계산
- 목표: `16384 / masterCount` 슬롯씩 균등 분배
- 슬롯이 많은 노드 → 적은 노드로 이동
- 최소한의 슬롯 이동으로 균형 달성

---

### 6. TLS 암호화 지원

#### 🔐 End-to-End TLS

```yaml
spec:
  TLS:
    enabled: true
    secret:
      secretName: redis-tls-certs
```

**적용 범위:**
- **Client ↔ Redis**: 클라이언트 연결 암호화
- **Master ↔ Replica**: Replication 트래픽 암호화
- **Cluster 노드 간 통신**: Gossip Protocol 암호화

**설정 자동 적용:**
```conf
# /etc/redis/redis.conf
tls-cert-file /tls/tls.crt
tls-key-file /tls/tls.key
tls-ca-cert-file /tls/ca.crt
tls-replication yes
tls-cluster yes
```

---

### 7. ACL 인증

#### 👥 사용자별 권한 관리

```yaml
spec:
  acl:
    enabled: true
    secret:
      secretName: redis-acl-config
```

**ACL 설정 예시:**
```acl
# user.acl
user default on >default_password ~* &* +@all
user readonly on >readonly_pass ~* +@read -@write
user admin on >admin_pass ~* &* +@all
```

**권한 제어:**
- 키 패턴별 접근 제어 (`~pattern`)
- Pub/Sub 채널 제어 (`&channel`)
- 명령어 카테고리 제어 (`+@read`, `-@write`)

---

### 8. 데이터 영속화

#### 💾 RDB + AOF 듀얼 보호

```yaml
spec:
  storage:
    data:
      enabled: true
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi
```

**RDB 스냅샷:**
```conf
save 900 1      # 15분 동안 1개 이상 변경 시
save 300 10     # 5분 동안 10개 이상 변경 시
save 60 10000   # 1분 동안 10000개 이상 변경 시
```

**AOF (Append Only File):**
```conf
appendonly yes
appendfilename "appendonly.aof"
```

**영속화 전략:**
- **RDB**: 주기적인 전체 스냅샷 (빠른 복구)
- **AOF**: 모든 쓰기 명령 로깅 (데이터 손실 최소화)
- **Combined**: 최대 내구성 보장

---

### 9. 동적 설정 관리

#### ⚙️ 런타임 설정 변경 (재시작 불필요)

```yaml
spec:
  redisConfig:
    dynamicConfig:
      - "maxmemory-policy allkeys-lru"
      - "slowlog-log-slower-than 5000"
      - "timeout 300"
```

**적용 과정:**
```bash
redis-cli CONFIG SET maxmemory-policy allkeys-lru
```

**지원 설정:**
- `maxmemory-policy`: 메모리 eviction 정책
- `slowlog-log-slower-than`: Slow Query 임계값
- `timeout`: 클라이언트 타임아웃
- `maxmemory`: 최대 메모리 제한

---

### 10. Prometheus 모니터링

#### 📊 Redis Exporter 통합

```yaml
spec:
  redisExporter:
    enabled: true
    image: oliver006/redis_exporter:latest
    port: 9121
    resources:
      requests:
        cpu: 50m
        memory: 64Mi
```

**수집 메트릭:**
- `redis_up`: Redis 인스턴스 상태
- `redis_connected_clients`: 연결된 클라이언트 수
- `redis_used_memory_bytes`: 메모리 사용량
- `redis_commands_total`: 명령어 실행 횟수
- `redis_keyspace_hits_total`: 캐시 히트율

---

## 🏗️ Redis 클러스터 기술

### 슬롯 기반 샤딩 (Slot-based Sharding)

Redis 클러스터는 **16384개의 Hash Slot**을 사용하여 데이터를 분산합니다.

#### 📍 슬롯 할당 방식

```
CRC16(key) mod 16384 = slot_number
```

**예시: 3-Master 클러스터**
```
Master 0: slots 0-5460    (5461 slots)
Master 1: slots 5461-10922 (5462 slots)
Master 2: slots 10923-16383 (5461 slots)
```

#### 🔄 슬롯 마이그레이션 과정

1. **MIGRATE 명령 실행**
   ```bash
   MIGRATE target-host target-port key 0 5000 COPY REPLACE
   ```

2. **슬롯 소유권 업데이트**
   ```bash
   CLUSTER SETSLOT <slot> NODE <target-node-id>
   ```

3. **클러스터 상태 동기화**
   - Gossip Protocol로 모든 노드에 변경사항 전파
   - 클라이언트는 `-MOVED` 리다이렉션 수신

---

### Gossip Protocol

#### 💬 노드 간 통신 메커니즘

**Gossip 메시지 타입:**
- `MEET`: 새 노드를 클러스터에 추가
- `PING/PONG`: 노드 상태 확인 (Health Check)
- `FAIL`: 노드 장애 전파
- `UPDATE`: 슬롯 할당 정보 업데이트

**통신 포트:**
- **Client Port**: 6379 (데이터 통신)
- **Cluster Bus Port**: 16379 (노드 간 통신)

---

### 고가용성 (High Availability)

#### 🛡️ 자동 Failover 메커니즘

1. **장애 감지**
   - `cluster-node-timeout` 동안 응답 없으면 `PFAIL` 상태
   - 과반수 노드가 동의하면 `FAIL` 상태로 전환

2. **Replica 승격**
   ```bash
   CLUSTER FAILOVER
   ```
   - Replica가 자동으로 Master로 승격
   - 슬롯 소유권 인계

3. **클러스터 재구성**
   - 새 Master를 클러스터에 반영
   - 다른 Replica들이 새 Master에 재연결

---

## 🏛️ 아키텍처

### Kubernetes Operator Pattern

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes API Server                     │
└────────────────┬────────────────────────────────────────────┘
                 │
                 │ Watch RedisCluster CRD
                 │
┌────────────────▼────────────────────────────────────────────┐
│                  Redis Cluster Controller                    │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Reconcile Loop (10초 주기)                           │  │
│  │  1. Desired State 분석                                │  │
│  │  2. Current State 확인                                │  │
│  │  3. 차이 해소 (Scale/Heal/Update)                     │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                 │
    ┌────────────┼────────────┐
    │            │            │
    ▼            ▼            ▼
┌─────────┐ ┌─────────┐ ┌─────────┐
│ Leader  │ │Follower │ │ Service │
│  STS    │ │  STS    │ │         │
└─────────┘ └─────────┘ └─────────┘
```

### StatefulSet 아키텍처

```
┌──────────────────────────────────────────────────────────────┐
│                      Leader StatefulSet                       │
├──────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   leader-0   │  │   leader-1   │  │   leader-2   │      │
│  │ (Master)     │  │ (Master)     │  │ (Master)     │      │
│  │ Slots:       │  │ Slots:       │  │ Slots:       │      │
│  │ 0-5460       │  │ 5461-10922   │  │ 10923-16383  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└──────────────────────────────────────────────────────────────┘
                         ▲
                         │ Replication
                         │
┌──────────────────────────────────────────────────────────────┐
│                     Follower StatefulSet                      │
├──────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ follower-0   │  │ follower-1   │  │ follower-2   │      │
│  │ (Replica of  │  │ (Replica of  │  │ (Replica of  │      │
│  │  leader-0)   │  │  leader-1)   │  │  leader-2)   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└──────────────────────────────────────────────────────────────┘
```

### Pod 초기화 과정

```
┌──────────────────────────────────────────────────────────────┐
│                         Redis Pod                             │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  1️⃣ Init Container (bootstrap-agent)                        │
│     ├─ 환경 변수 읽기                                         │
│     ├─ /etc/redis/redis.conf 생성                           │
│     ├─ cluster-announce-ip 설정 (Pod IP)                    │
│     ├─ cluster-announce-hostname 설정 (FQDN)                │
│     ├─ nodes.conf IP 업데이트                                │
│     └─ TLS/ACL/Persistence 설정 적용                         │
│                                                               │
│  2️⃣ Main Container (redis-server)                           │
│     ├─ redis-server /etc/redis/redis.conf 실행              │
│     ├─ Cluster 모드로 시작                                    │
│     └─ Ready Probe: redis-cli PING                           │
│                                                               │
│  3️⃣ Sidecar Container (redis-exporter) - Optional           │
│     └─ Prometheus 메트릭 노출 (:9121/metrics)                │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

---

## 🛠️ 기술 스택

### Core Technologies

| 컴포넌트 | 기술 | 버전 | 설명 |
|---------|------|------|------|
| **언어** | Go | 1.25 | 고성능 시스템 프로그래밍 |
| **프레임워크** | Kubebuilder | - | Operator 스캐폴딩 |
| **Controller** | controller-runtime | 0.22.4 | Kubernetes Controller 엔진 |
| **Kubernetes** | client-go | 0.35.0 | Kubernetes API 클라이언트 |
| **Redis** | go-redis | 9.17.2 | Redis 클라이언트 라이브러리 |

### Supporting Libraries

- **retry-go**: 재시도 로직
- **samber/lo**: 함수형 유틸리티
- **spf13/cobra**: CLI 프레임워크
- **spf13/viper**: 설정 관리

### Development Tools

- **golangci-lint**: 코드 품질 검사
- **envtest**: 단위 테스트
- **kind**: E2E 테스트 환경

---

## 🚀 빠른 시작

### 사전 요구사항

- Kubernetes 1.35+
- kubectl CLI
- 최소 3개의 Worker 노드 (고가용성 권장)

### 1. Operator 설치

```bash
# CRD 설치
kubectl apply -f https://raw.githubusercontent.com/xcdev-0/redis-operator/main/config/crd/bases/ejlabs.in_redisclusters.yaml

# Operator 배포
kubectl apply -f https://raw.githubusercontent.com/xcdev-0/redis-operator/main/config/manager/manager.yaml
```

### 2. Redis 클러스터 생성

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: ejlabs.in/v1beta2
kind: RedisCluster
metadata:
  name: my-redis-cluster
  namespace: default
spec:
  clusterSize: 3
  clusterVersion: v7
  
  # Leader 설정
  redisLeader:
    replicaCount: 3
  
  # Follower 설정
  redisFollower:
    replicaCount: 3
  
  # Kubernetes 리소스 설정
  kubernetesConfig:
    image: redis:7.2-alpine
    imagePullPolicy: IfNotPresent
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 1000m
        memory: 512Mi
  
  # 데이터 영속화
  storage:
    data:
      enabled: true
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi
EOF
```

### 3. 클러스터 상태 확인

```bash
# RedisCluster 상태
kubectl get rediscluster my-redis-cluster

# 출력:
# NAME                CLUSTERSIZE   READYLEADERREPLICAS   READYFOLLOWERREPLICAS   STATE   AGE
# my-redis-cluster    3             3                     3                       Ready   5m

# Pod 상태
kubectl get pods -l app.kubernetes.io/name=my-redis-cluster

# Redis 클러스터 상태
kubectl exec -it my-redis-cluster-leader-0 -- redis-cli cluster info
kubectl exec -it my-redis-cluster-leader-0 -- redis-cli cluster nodes
```

### 4. 클러스터 접속

```bash
# 클러스터 내부에서
kubectl exec -it my-redis-cluster-leader-0 -- redis-cli -c

# 외부 접속 (Service 사용)
kubectl port-forward svc/my-redis-cluster-leader 6379:6379
redis-cli -c -h localhost -p 6379
```

---

## ⚙️ 설정 가이드

### Leader/Follower 독립 설정

```yaml
spec:
  # Leader (Master) 전용 설정
  redisLeader:
    replicaCount: 3
    resources:
      requests:
        cpu: 500m
        memory: 1Gi
      limits:
        cpu: 2000m
        memory: 4Gi
    redisConfig:
      maxMemoryPercentOfLimit: 80
      additionalRedisConfig: "leader-config"
  
  # Follower (Replica) 전용 설정
  redisFollower:
    replicaCount: 6  # 각 Master당 2개의 Replica
    resources:
      requests:
        cpu: 200m
        memory: 512Mi
      limits:
        cpu: 1000m
        memory: 2Gi
    redisConfig:
      maxMemoryPercentOfLimit: 70
      additionalRedisConfig: "follower-config"
```

### 보안 설정

```yaml
spec:
  # Password 인증
  redisConfig:
    secret:
      secretName: redis-password
      key: password
  
  # TLS 암호화
  TLS:
    enabled: true
    secret:
      secretName: redis-tls-certs
  
  # ACL 인증
  acl:
    enabled: true
    secret:
      secretName: redis-acl-config
```

### NodePort 모드 (외부 접근)

```yaml
spec:
  hostNetwork: false
  kubernetesConfig:
    service:
      serviceType: NodePort
```

### 추가 볼륨 마운트

```yaml
spec:
  storage:
    additionalVolumeAndMounts:
      volumes:
        - name: custom-config
          configMap:
            name: redis-custom-config
      volumeMounts:
        - name: custom-config
          mountPath: /etc/redis/external.conf.d
```

---

## 🔬 고급 기능

### 1. 외부 설정 파일 주입

**ConfigMap 생성:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: redis-custom-config
data:
  01-custom.conf: |
    maxmemory-policy allkeys-lru
    slowlog-log-slower-than 10000
  02-advanced.conf: |
    tcp-keepalive 60
    timeout 300
```

**RedisCluster 설정:**
```yaml
spec:
  redisConfig:
    additionalRedisConfig: "redis-custom-config"
```

Operator가 자동으로 `include /etc/redis/external.conf.d/*.conf` 추가

---

### 2. Sidecar 컨테이너

```yaml
spec:
  sidecars:
    - name: log-collector
      image: fluent/fluent-bit:latest
      volumeMounts:
        - name: redis-logs
          mountPath: /var/log/redis
```

---

### 3. Pod Disruption Budget

```yaml
spec:
  redisLeader:
    podDisruptionBudget:
      minAvailable: 2
  redisFollower:
    podDisruptionBudget:
      maxUnavailable: 1
```

---

### 4. Init Container 커스터마이징

```yaml
spec:
  kubernetesConfig:
    initContainers:
      - name: volume-permissions
        image: busybox:latest
        command: ['sh', '-c', 'chmod 777 /data']
        volumeMounts:
          - name: data
            mountPath: /data
```

---

## 🧪 운영 시나리오

### 스케일링

```bash
# Scale Out: 3 → 5 Masters
kubectl patch rediscluster my-redis-cluster --type='merge' -p '{"spec":{"clusterSize":5,"redisLeader":{"replicaCount":5},"redisFollower":{"replicaCount":5}}}'

# Scale In: 5 → 3 Masters
kubectl patch rediscluster my-redis-cluster --type='merge' -p '{"spec":{"clusterSize":3,"redisLeader":{"replicaCount":3},"redisFollower":{"replicaCount":3}}}'
```

### 수동 Rebalancing

```bash
kubectl exec -it my-redis-cluster-leader-0 -- \
  redis-cli --cluster rebalance localhost:6379
```

### 백업

```bash
# RDB 스냅샷 생성
kubectl exec -it my-redis-cluster-leader-0 -- redis-cli BGSAVE

# AOF 재작성
kubectl exec -it my-redis-cluster-leader-0 -- redis-cli BGREWRITEAOF
```

### 장애 시뮬레이션

```bash
# Master 노드 삭제 (자동 Failover 테스트)
kubectl delete pod my-redis-cluster-leader-1

# 클러스터 상태 확인
kubectl exec -it my-redis-cluster-leader-0 -- redis-cli cluster nodes
```

---

## 📚 추가 자료

### Redis 클러스터 문서
- [Redis Cluster Tutorial](https://redis.io/docs/management/scaling/)
- [Redis Cluster Specification](https://redis.io/docs/reference/cluster-spec/)

### Kubernetes Operator
- [Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [Kubebuilder Book](https://book.kubebuilder.io/)

---

## 📄 라이선스

Apache License 2.0

---

## 🤝 기여

이슈와 PR은 언제나 환영합니다!

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## 👨‍💻 개발자

**EJ Labs**

- Repository: [github.com/xcdev-0/redis-operator](https://github.com/xcdev-0/redis-operator)
- Issues: [github.com/xcdev-0/redis-operator/issues](https://github.com/xcdev-0/redis-operator/issues)

---

