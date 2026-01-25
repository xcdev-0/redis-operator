package statefulset

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generateStatefulSetDef(
	objectMeta metav1.ObjectMeta,
	stsParams StatefulSetParameters,
	ownerDef metav1.OwnerReference,
	containerParams ContainerParameters,
) *appsv1.StatefulSet {

	selectorLabels := &metav1.LabelSelector{
		MatchLabels: k8smeta.GetRedisClusterStableLabelsFromLabels(objectMeta.GetLabels())}

	statefulset := &appsv1.StatefulSet{
		TypeMeta:   k8smeta.GenerateTypeMeta("StatefulSet", "apps/v1"),
		ObjectMeta: objectMeta,
		Spec: appsv1.StatefulSetSpec{
			Selector:                             selectorLabels,
			ServiceName:                          fmt.Sprintf("%s-headless", objectMeta.Name),
			Replicas:                             stsParams.Replicas,
			UpdateStrategy:                       stsParams.UpdateStrategy,
			PersistentVolumeClaimRetentionPolicy: stsParams.PersistentVolumeClaimRetentionPolicy,
			MinReadySeconds:                      stsParams.MinReadySeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      objectMeta.GetLabels(), // sts label == pod label
					Annotations: k8smeta.GenerateStatefulSetsAnots(objectMeta, stsParams.IgnoreAnnotations),
				},
				Spec: corev1.PodSpec{
					// 메인 컨테이너 설정
					Containers: generateMainContainerDef(ContainerConfigParams{
						ContainerName:   consts.MainContainerName,
						EnableMetrics:   stsParams.EnableMetrics,
						ContainerParams: containerParams,
						ExternalConfig:  stsParams.ExternalConfig,
						ClusterVersion:  stsParams.ClusterVersion,
					}),

					// Init Container에서 생성하는 설정 파일을 저장할 볼륨
					Volumes: []corev1.Volume{
						NewConfigVolumeConfig().Volume},

					// Init Container 설정
					InitContainers: generateInitContainerDef(InitContainerConfig{
						RedisSetupType:      containerParams.RedisSetupType,
						ContainerName:       "init-config",
						ExternalConfig:      stsParams.ExternalConfig,
						ContainerParameters: containerParams,
						ClusterVersion:      stsParams.ClusterVersion,
					}),
				},
			},
		},
	}

	// 외부 ConfigMap이 있는 경우, 볼륨에 추가 -> init container에서 사용
	if stsParams.ExternalConfig != nil {
		volumeConfig := NewExternalConfigVolumeConfig(*stsParams.ExternalConfig)
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			volumeConfig.Volume)
	}

	// 추가 볼륨 추가
	if len(stsParams.AdditionalVolumes) > 0 {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, stsParams.AdditionalVolumes...)
	}

	// TLS 인증서 볼륨 추가
	if tlsConfig := NewTLSVolumeConfig(containerParams.TLSConfig); tlsConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			tlsConfig.Volume)
	}

	// ACL 설정 볼륨 추가 (Secret 또는 PVC)
	if aclConfig := NewACLVolumeConfig(containerParams.ACLConfig); aclConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			aclConfig.Volume)
	}

	// 노드 설정 저장용 PVC 템플릿 설정
	// 노드 영속성은 데이터 영속성과 함께 사용되어야 합니다
	if containerParams.NodePersistenceEnabled {
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVCTemplate(
				consts.VolumeNameNodeConf,
				objectMeta,
				stsParams.NodeConfPVC))
	}

	// 데이터 저장용 PVC 템플릿 설정
	if containerParams.DataPersistenceEnabled {
		// TODO: 이름 설정
		// pvcTplName := util.CoalesceEnv1(consts.EnvOperatorSTSPVCTemplateName, consts.DataVolumeName)
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVCTemplate(
				consts.VolumeNameData,
				objectMeta,
				stsParams.DataPVC))
	}

	// tolerations 설정
	if stsParams.Tolerations != nil {
		statefulset.Spec.Template.Spec.Tolerations = *stsParams.Tolerations
	}
	// 이미지 풀 시크릿 설정 (프라이빗 레지스트리 인증용)
	if stsParams.ImagePullSecrets != nil {
		statefulset.Spec.Template.Spec.ImagePullSecrets = *stsParams.ImagePullSecrets
	}
	// ServiceAccount 설정
	if stsParams.ServiceAccountName != nil {
		statefulset.Spec.Template.Spec.ServiceAccountName = *stsParams.ServiceAccountName
	}
	// OwnerReference 추가 (CRD가 삭제되면 StatefulSet도 함께 삭제되도록)
	k8smeta.AddOwnerRefToObject(statefulset, ownerDef)

	return statefulset
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
