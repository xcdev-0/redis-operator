package statefulset

import (
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/util"
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

func generateConfigVolumeMount(volumeName string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      volumeName,
		MountPath: "/etc/redis",
	}
}

func generateExternalConfigVolumeMount(volumeName string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      volumeName,
		MountPath: "/etc/redis/external.conf.d",
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
	Name                   string
	AdditionalVolumeMounts []corev1.VolumeMount
	Persistence            bool
	ClusterModeEnabled     bool
	NodeConfVolumeEnabled  bool
	ExternalConfig         *string
	TLS                    *tlsConfig
	ACL                    *aclConfig
}

func getVolumeMount(p volumeMountParams) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	if p.ClusterModeEnabled && p.NodeConfVolumeEnabled && p.Persistence {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      consts.NodeConfVolumeName,
			MountPath: "/node-conf",
		})
	}

	if p.Persistence {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      util.CoalesceEnv1(consts.EnvOperatorSTSPVCTemplateName, p.Name),
			MountPath: "/data",
		})
	}

	if p.TLS != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      consts.TLSCertsVolumeName,
			ReadOnly:  true,   // 읽기 전용 (보안)
			MountPath: "/tls", // TLS 인증서 경로
		})
	}
	if p.ACL != nil {
		volumeName := p.ACL.GetVolumeName()
		if volumeName != "" {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/etc/redis/user.acl",
				SubPath:   "user.acl",
			})
		}
	}

	// 유저가 제공한 컨피그맵을 마운트
	// 컨피그맵 -> external-config 볼륨 이 볼륨을 마운트
	if p.ExternalConfig != nil {
		mounts = append(mounts,
			generateExternalConfigVolumeMount(consts.ExternalConfigVolumeName))
	}

	// Init Container에서 생성한 설정 파일 볼륨 마운트
	mounts = append(mounts, generateConfigVolumeMount(consts.ConfigVolumeName))

	// 추가 볼륨 마운트
	mounts = append(mounts, p.AdditionalVolumeMounts...)

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
