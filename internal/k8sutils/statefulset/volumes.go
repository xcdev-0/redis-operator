package statefulset

import (
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generateEmptyVolume(volumeName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{}, // Pod가 삭제되면 함께 삭제되는 임시 볼륨
		},
	}
}

func generateConfigVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.ConfigVolumeName,
		MountPath: "/etc/redis",
	}
}

func generateExternalConfigVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.ExternalConfigVolumeName,
		MountPath: "/etc/redis/external.conf.d",
	}
}

// generateNodeConfVolumeMount는 클러스터 모드에서 노드 설정 볼륨 마운트를 생성합니다.
func generateNodeConfVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.NodeConfVolumeName,
		MountPath: "/node-conf",
	}
}

// generateDataVolumeMount는 데이터 영속성을 위한 PVC 볼륨 마운트를 생성합니다.
func generateDataVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.DataVolumeName,
		MountPath: "/data",
	}
}

// generateTLSVolumeMount는 TLS 인증서 볼륨 마운트를 생성합니다.
func generateTLSVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.TLSCertsVolumeName,
		ReadOnly:  true,   // 읽기 전용 (보안)
		MountPath: "/tls", // TLS 인증서 경로
	}
}

// generateACLVolumeMount는 ACL 설정 파일 볼륨 마운트를 생성합니다.
// ACL이 설정되지 않았거나 볼륨 이름이 없으면 nil을 반환합니다.
func generateACLVolumeMount(acl *ACLConfig) *corev1.VolumeMount {
	if acl == nil {
		return nil
	}
	volumeName := acl.GetVolumeName()
	if volumeName == "" {
		return nil
	}
	return &corev1.VolumeMount{
		Name:      volumeName,
		MountPath: "/etc/redis/user.acl",
		SubPath:   "user.acl",
	}
}

func convertFromConfigmapToVolume(volumeName string, configMapName string) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				},
			},
		},
	}
}

type volumeMountParams struct {
	ContainerName          string
	AdditionalVolumeMounts []corev1.VolumeMount
	DataPersistence        bool
	NodePersistence        bool
	ClusterModeEnabled     bool
	ExternalConfig         *string
	TLS                    *TLSConfig
	ACL                    *ACLConfig
}

// // getVolumeMountForUserInitContainer는 User Init Container용 볼륨 마운트를 생성합니다.
// // Init Container는 기본 설정 볼륨과 선택적으로 데이터 볼륨만 필요합니다.
// func getVolumeMountForUserInitContainer(name string, persistence bool, additional []corev1.VolumeMount) []corev1.VolumeMount {
// 	var mounts []corev1.VolumeMount

// 	// 데이터 영속성 볼륨 (선택적)
// 	if persistence {
// 		mounts = append(mounts, generatePersistenceVolumeMount(name))
// 	}

// 	// 기본 설정 볼륨 (항상 포함)
// 	mounts = append(mounts, generateConfigVolumeMount())

// 	// 추가 볼륨 마운트
// 	mounts = append(mounts, additional...)

// 	return mounts
// }

// getVolumeMountForMainContainer는 Redis 메인 컨테이너용 볼륨 마운트를 생성합니다.
// 메인 컨테이너는 모든 볼륨 타입이 필요할 수 있습니다.
func getVolumeMountForMainContainer(p volumeMountParams) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	// 클러스터 모드 + 노드 설정 볼륨
	// 노드 영속성은 데이터 영속성과 함께 사용되어야 합니다
	if p.NodePersistence {
		mounts = append(mounts, generateNodeConfVolumeMount())
	}

	// 데이터 영속성 볼륨
	if p.DataPersistence {
		mounts = append(mounts, generateDataVolumeMount())
	}

	// TLS 인증서 볼륨
	if p.TLS != nil {
		mounts = append(mounts, generateTLSVolumeMount())
	}

	// ACL 설정 볼륨
	if aclMount := generateACLVolumeMount(p.ACL); aclMount != nil {
		mounts = append(mounts, *aclMount)
	}

	// 외부 설정 볼륨 (유저가 제공한 컨피그맵)
	// volumename: external-config, mountPath: /etc/redis/external.conf.d
	if p.ExternalConfig != nil {
		mounts = append(mounts, generateExternalConfigVolumeMount())
	}

	// 기본 설정 볼륨 (항상 포함)
	// volumename: config, mountPath: /etc/redis
	mounts = append(mounts, generateConfigVolumeMount())

	// 추가 볼륨 마운트
	mounts = append(mounts, p.AdditionalVolumeMounts...)

	return mounts
}

// getVolumeMountForExporter는 Redis Exporter 사이드카 컨테이너용 볼륨 마운트를 생성합니다.
// Exporter는 TLS 인증서와 ACL만 필요하며, 데이터 볼륨은 필요 없습니다.
func getVolumeMountForExporter(tls *TLSConfig, acl *ACLConfig, additional []corev1.VolumeMount) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	// TLS 인증서 볼륨
	if tls != nil {
		mounts = append(mounts, generateTLSVolumeMount())
	}

	// ACL 설정 볼륨
	if aclMount := generateACLVolumeMount(acl); aclMount != nil {
		mounts = append(mounts, *aclMount)
	}

	// 기본 설정 볼륨 (항상 포함)
	mounts = append(mounts, generateConfigVolumeMount())

	// 추가 볼륨 마운트
	mounts = append(mounts, additional...)

	return mounts
}

func createPVCTemplate(volumeName string, objectMeta metav1.ObjectMeta, pvc corev1.PersistentVolumeClaim) corev1.PersistentVolumeClaim {
	pvcTemplate := pvc // 복사후 필요한 필드만 설정

	// 템플릿이므로 생성 시간 초기화
	pvcTemplate.CreationTimestamp = metav1.Time{}
	// 볼륨 이름 설정
	pvcTemplate.Name = volumeName
	// StatefulSet과 동일한 라벨
	pvcTemplate.Labels = objectMeta.GetLabels()
	// StatefulSet과 동일한 어노테이션
	pvcTemplate.Annotations = k8smeta.GenerateStatefulSetsAnots(objectMeta, nil)
	// AccessMode가 지정되지 않으면 기본값으로 ReadWriteOnce 사용
	if pvc.Spec.AccessModes == nil {
		pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	} else {
		pvcTemplate.Spec.AccessModes = pvc.Spec.AccessModes
	}
	// VolumeMode 설정 (Filesystem 또는 Block)
	pvcVolumeMode := corev1.PersistentVolumeFilesystem
	if pvc.Spec.VolumeMode != nil {
		pvcVolumeMode = *pvc.Spec.VolumeMode
	}
	pvcTemplate.Spec.VolumeMode = &pvcVolumeMode
	// 스토리지 크기
	pvcTemplate.Spec.Resources = pvc.Spec.Resources
	// 특정 PV 선택용
	pvcTemplate.Spec.Selector = pvc.Spec.Selector
	return pvcTemplate
}
