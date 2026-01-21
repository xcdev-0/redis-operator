package statefulset

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatefulSetParameters는 StatefulSet 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 StatefulSet의 스펙을 정의하는 데 사용됩니다.
type StatefulSetParameters struct {
	Replicas                  *int32                            // StatefulSet의 레플리카 수
	ClusterModeEnabled        bool                              // Redis Cluster 모드 활성화 여부
	ClusterVersion            *string                           // Redis 클러스터 버전
	NodeSelector              map[string]string                 // Pod가 스케줄링될 노드를 선택하는 라벨
	TopologySpreadConstraints []corev1.TopologySpreadConstraint // Pod 분산 제약 조건 (노드/존 간 균등 분산)
	PodSecurityContext        *corev1.PodSecurityContext        // Pod 레벨 보안 컨텍스트
	PriorityClassName         string                            // Pod 우선순위 클래스 이름
	Affinity                  *corev1.Affinity                  // Pod 어피니티 규칙 (노드/Pod 간 선호도)
	Tolerations               *[]corev1.Toleration              // Pod 톨러레이션 (테인트 허용)
	EnableMetrics             bool                              // Redis Exporter 메트릭 활성화 여부

	DataPVC           corev1.PersistentVolumeClaim // 데이터 저장용 PVC 템플릿
	NodeConfPVC       corev1.PersistentVolumeClaim // 노드 설정 저장용 PVC 템플릿 (클러스터 모드)
	AdditionalVolumes []corev1.Volume              // 추가 볼륨
	ExternalConfig    *string                      // 외부 ConfigMap 이름 (추가 Redis 설정)

	ImagePullSecrets                     *[]corev1.LocalObjectReference                          // 이미지 풀 시크릿 (프라이빗 레지스트리용)
	ServiceAccountName                   *string                                                 // Pod에 사용할 ServiceAccount 이름
	UpdateStrategy                       appsv1.StatefulSetUpdateStrategy                        // StatefulSet 업데이트 전략
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy // PVC 보존 정책
	RecreateStatefulSet                  bool                                                    // 변경 불가능한 필드 변경 시 StatefulSet 재생성 여부
	RecreateStatefulsetStrategy          *metav1.DeletionPropagation                             // StatefulSet 재생성 시 삭제 전파 전략
	TerminationGracePeriodSeconds        *int64                                                  // Pod 종료 유예 기간 (초)
	IgnoreAnnotations                    []string                                                // 패치 비교 시 무시할 어노테이션 목록
	HostNetwork                          bool                                                    // 호스트 네트워크 사용 여부
	MinReadySeconds                      int32                                                   // Pod가 준비된 것으로 간주되기 전 최소 대기 시간 (초)
}
