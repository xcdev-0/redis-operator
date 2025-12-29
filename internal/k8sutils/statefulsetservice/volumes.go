package statefulsetservice

import (
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	types "github.com/xcdev-0/redis-operator/internal/k8sutils/statefulsetservice/types"
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

func generateInitConfigVolumeMount(volumeName string) corev1.VolumeMount {
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

func getVolumeMount(p types.VolumeMountParams) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	if p.Runtime.ClusterModeEnabled && p.Runtime.NodeConfVolumeEnabled {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "node-conf",
			MountPath: "/node-conf",
		})
	}

	if p.Persistence.Enabled != nil && *p.Persistence.Enabled {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      util.CoalesceEnv1(consts.EnvOperatorSTSPVCTemplateName, p.Name),
			MountPath: "/data",
		})
	}

	if p.TLS != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "tls-certs",
			ReadOnly:  true,   // 읽기 전용 (보안)
			MountPath: "/tls", // TLS 인증서 경로
		})
	}
	if p.ACL != nil {
		volumeName := "acl-secret"
		if p.ACL.PersistentVolumeClaim != nil {
			volumeName = "acl-pvc"
		}
		mounts = append(mounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: "/etc/redis/user.acl",
			SubPath:   "user.acl",
		})
	}

	// 유저가 제공한 컨피그맵을 마운트
	// 컨피그맵 -> external-config 볼륨 이 볼륨을 마운트
	if p.ExternalConfig != nil {
		mounts = append(mounts,
			generateExternalConfigVolumeMount(consts.ExternalInitConfigVolumeName))
	}

	// Init Container에서 생성한 설정 파일 볼륨 마운트
	mounts = append(mounts, generateInitConfigVolumeMount(consts.InitConfigVolumeName))

	// 추가 볼륨 마운트
	mounts = append(mounts, p.AdditionalVolumeMounts...)

	return mounts
}

func createPVCTemplate(volumeName string, stsMeta metav1.ObjectMeta, storageSpec corev1.PersistentVolumeClaim) corev1.PersistentVolumeClaim {
	pvcTemplate := storageSpec // 복사후 필요한 필드만 설정

	// 템플릿이므로 생성 시간 초기화
	pvcTemplate.CreationTimestamp = metav1.Time{}
	// 볼륨 이름 설정
	pvcTemplate.Name = volumeName
	// StatefulSet과 동일한 라벨
	pvcTemplate.Labels = stsMeta.GetLabels()
	// StatefulSet과 동일한 어노테이션
	pvcTemplate.Annotations = k8smeta.GenerateStatefulSetsAnots(stsMeta, nil)
	// AccessMode가 지정되지 않으면 기본값으로 ReadWriteOnce 사용
	if storageSpec.Spec.AccessModes == nil {
		pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	} else {
		pvcTemplate.Spec.AccessModes = storageSpec.Spec.AccessModes
	}
	// VolumeMode 설정 (Filesystem 또는 Block)
	pvcVolumeMode := corev1.PersistentVolumeFilesystem
	if storageSpec.Spec.VolumeMode != nil {
		pvcVolumeMode = *storageSpec.Spec.VolumeMode
	}
	pvcTemplate.Spec.VolumeMode = &pvcVolumeMode
	// 스토리지 크기
	pvcTemplate.Spec.Resources = storageSpec.Spec.Resources
	// 특정 PV 선택용
	pvcTemplate.Spec.Selector = storageSpec.Spec.Selector
	return pvcTemplate
}
