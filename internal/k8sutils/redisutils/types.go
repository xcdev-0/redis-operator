package redis

// Redis Cluster Service 생성 및 관리
//
// 이 패키지는 Redis Cluster를 위한 여러 종류의 Kubernetes Service를 생성합니다:
//
// 1. ClusterIP Service (일반)
//    - 클러스터 내부에서 Redis에 접근하기 위한 기본 Service
//    - 버스 포트: 조건부 포함 (ShouldIncludeBusPort() 설정에 따라)
//
// 2. Headless Service
//    - StatefulSet의 각 Pod에 대한 고유한 DNS 이름 제공 (ClusterIP: None)
//    - Pod 간 직접 통신을 위한 Service
//    - 버스 포트: 조건부 포함 (ShouldIncludeBusPortForHeadless() 설정에 따라)
//
// 3. Additional Service
//    - 사용자가 추가로 정의한 선택적 Service
//    - ServiceType은 KubernetesConfig에서 설정 가능 (ClusterIP, NodePort, LoadBalancer)
//    - 버스 포트: 조건부 포함 (ShouldIncludeBusPortForAdditional() 설정에 따라)
//
// 4. Master Service
//    - Master 역할의 Pod를 선택하는 Service
//    - 클러스터의 Master 노드에 접근할 때 사용
//    - 버스 포트: 포함하지 않음 (Master 선택 전용)
//
// 5. NodePort Service (각 Pod마다 개별 생성)
//    - ServiceType이 "NodePort"일 때만 생성
//    - 각 Pod마다 고유한 NodePort를 할당하여 외부 접근 지원
//    - 버스 포트: 항상 포함
//    - 이유: NodePort 모드에서는 cluster-announce-ip가 노드 IP를 사용
//            노드 IP로 접근하려면 NodePort가 필요하며, ClusterIP는 클러스터 내부에서만
//            접근 가능하므로 노드 IP로는 접근할 수 없음.
// 			  따라서 버스 포트도 NodePort로 노출하여 노드 간 통신이 가능하게함
//
// Redis Cluster Bus Port 정의:
// - 버스 포트는 Redis Cluster 노드 간 통신을 위한 포트
// - 포트 번호: ClientPort + 10000 (예: ClientPort가 6379이면 버스 포트는 16379)
// - Redis Cluster는 클라이언트 포트와 버스 포트를 모두 사용하여 클러스터를 구성함

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/envs"
)

// RedisDetails will hold the information for Redis Pod
type RedisDetails struct {
	PodName   string
	Namespace string
}

func (rd *RedisDetails) FQDN() string {
	return fmt.Sprintf("%s.%s.%s.svc.%s",
		rd.PodName,
		getHeadlessServiceNameFromPodName(rd.PodName),
		rd.Namespace,
		envs.GetServiceDNSDomain(),
	)
}
