package statefulset

import (
	"context"
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
)

type StatefulSet interface {
	IsStatefulSetReady(ctx context.Context, namespace, name string) bool
	GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32
}

type StatefulSetService struct {
	kubeClient kubernetes.Interface
}

func NewStatefulSetService(kubeClient kubernetes.Interface) *StatefulSetService {
	return &StatefulSetService{
		kubeClient: kubeClient,
	}
}

func (s *StatefulSetService) IsStatefulSetReady(ctx context.Context, namespace, name string) bool {
	var (
		partition = 0
		replicas  = 1
	)
	sts, err := s.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get statefulset")
		return false
	}

	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = int(*sts.Spec.UpdateStrategy.RollingUpdate.Partition)
	}

	if sts.Spec.Replicas != nil {
		replicas = int(*sts.Spec.Replicas)
	}

	// expectedUpdateReplicas: 새 revision으로 바뀐 Pod 수만 말해줌
	// 그 Pod들이 Ready인지, 혹은 새 Pod가 실제로 Service endpoints로 들어갔는지는 별개
	if expectedUpdateReplicas := replicas - partition; sts.Status.UpdatedReplicas < int32(expectedUpdateReplicas) {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.UpdatedReplicas", sts.Status.UpdatedReplicas, "ExpectedUpdateReplicas", expectedUpdateReplicas)
		return false
	}

	if partition == 0 && sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.CurrentRevision", sts.Status.CurrentRevision, "Status.UpdateRevision", sts.Status.UpdateRevision)
		return false
	}

	if sts.Status.ObservedGeneration != sts.Generation {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.ObservedGeneration", sts.Status.ObservedGeneration, "Generation", sts.Generation)
		return false
	}
	if int(sts.Status.ReadyReplicas) != replicas {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.ReadyReplicas", sts.Status.ReadyReplicas, "Replicas", replicas)
		return false
	}
	return true
}

func (s *StatefulSetService) GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32 {
	sts, err := s.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0
	}
	if sts.Spec.Replicas == nil {
		return 0
	}
	return *sts.Spec.Replicas
}

func GetStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, name string) (*appsv1.StatefulSet, error) {
	statefulInfo, err := cl.AppsV1().StatefulSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis statefulset get action failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset get action was successful")
	return statefulInfo, nil
}

func CreateOrUpdateStateFul(ctx context.Context,
	kubeClient kubernetes.Interface,
	req *StatefulSetRequest) error {
	storedStateful, err := GetStatefulSet(ctx, kubeClient, req.Namespace, req.StsObjectMeta.Name)
	statefulSetDef := generateStatefulSet(
		req.StsObjectMeta,
		req.StsParams,
		req.OwnerReference,
		req.InitContainerParams,
		req.ContainerParams,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// StatefulSet이 존재하지 않는 경우에만 어노테이션 설정
			if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(statefulSetDef); err != nil {
				log.FromContext(ctx).Error(err, "Unable to patch redisutils statefulset with comparison object")
				return err
			}
			return createStatefulSet(ctx, kubeClient, req.Namespace, statefulSetDef)
		}
		// 다른 에러는 바로 반환
		return err
	}
	return patchStatefulSet(ctx, storedStateful, statefulSetDef, req.Namespace, req.StsParams.RecreateStatefulSet, req.StsParams.RecreateStatefulsetStrategy, kubeClient)
}

func generateStatefulSet(
	stsMeta metav1.ObjectMeta,
	stsParams StatefulSetParameters,
	ownerDef metav1.OwnerReference,
	initcontainerParams InitContainerParameters,
	containerParams ContainerParameters,
) *appsv1.StatefulSet {

	statefulset := &appsv1.StatefulSet{
		TypeMeta:   k8smeta.GenerateTypeMeta("StatefulSet", "apps/v1"),
		ObjectMeta: stsMeta,
		Spec: appsv1.StatefulSetSpec{
			Selector: k8smeta.LabelSelectors(
				k8smeta.ExtractStatefulSetSelectorLabels(stsMeta.GetLabels())),
			ServiceName:                          fmt.Sprintf("%s-headless", stsMeta.Name),
			Replicas:                             stsParams.Replicas,
			UpdateStrategy:                       stsParams.UpdateStrategy,
			PersistentVolumeClaimRetentionPolicy: stsParams.PersistentVolumeClaimRetentionPolicy,
			MinReadySeconds:                      stsParams.MinReadySeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      stsMeta.GetLabels(),
					Annotations: k8smeta.GenerateStatefulSetsAnots(stsMeta, stsParams.IgnoreAnnotations),
				},
				Spec: corev1.PodSpec{
					// 메인 컨테이너 설정
					Containers: generateMainContainerDef(ContainerConfig{
						Name:                   stsMeta.GetName(),
						ContainerParams:        containerParams,
						ClusterModeEnabled:     stsParams.ClusterModeEnabled,
						NodeConfVolumeEnabled:  stsParams.NodeConfVolumeEnabled,
						ExternalConfig:         stsParams.ExternalConfig,
						ClusterVersion:         stsParams.ClusterVersion,
						AdditionalVolumeMounts: containerParams.AdditionalMountPath,
					}),

					// Init Container에서 생성하는 설정 파일을 저장할 볼륨
					Volumes: []corev1.Volume{
						generateEmptyVolume(consts.InitConfigVolumeName)},

					// Init Container 설정
					InitContainers: generateInitContainerDef(InitContainerConfig{
						Role:                    containerParams.Role,
						Name:                    stsMeta.GetName(),
						InitContainerParameters: initcontainerParams,
						ExternalConfig:          stsParams.ExternalConfig,
						AdditionalVolumeMounts:  initcontainerParams.AdditionalMountPath,
						ContainerParameters:     containerParams,
						ClusterVersion:          stsParams.ClusterVersion,
					}),
				},
			},
		},
	}

	// 외부 ConfigMap이 있는 경우, 볼륨에 추가 -> init container에서 사용
	if stsParams.ExternalConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			convertFromConfigmapToVolume(consts.ExternalInitConfigVolumeName, *stsParams.ExternalConfig)...)
	}

	// 추가 볼륨 추가
	if containerParams.AdditionalVolume != nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, containerParams.AdditionalVolume...)
	}

	// TLS 인증서 볼륨 추가
	if containerParams.TLSConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: consts.TLSCertsVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: containerParams.TLSConfig.Secret,
				},
			})
	}

	// ACL 설정 볼륨 추가 (Secret 또는 PVC)
	if containerParams.ACLConfig != nil {
		if containerParams.ACLConfig.Secret != nil {
			// ACL이 Secret에 저장된 경우
			statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name: consts.ACLSecretVolumeName,
					VolumeSource: corev1.VolumeSource{
						Secret: containerParams.ACLConfig.Secret,
					},
				})
		} else if containerParams.ACLConfig.PersistentVolumeClaimName != nil {
			// ACL이 PVC에 저장된 경우
			statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name: consts.ACLPVCVolumeName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: *containerParams.ACLConfig.PersistentVolumeClaimName,
						},
					},
				})
		}
	}

	// 노드 설정 저장용 PVC 템플릿 설정
	if containerParams.PersistenceEnabled != nil &&
		*containerParams.PersistenceEnabled &&
		stsParams.ClusterModeEnabled &&
		stsParams.NodeConfVolumeEnabled {
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVCTemplate("node-conf", stsMeta, stsParams.NodeConfPersistentVolumeClaim))
	}

	// 데이터 저장용 PVC 템플릿 설정
	if containerParams.PersistenceEnabled != nil && *containerParams.PersistenceEnabled {
		pvcTplName := util.CoalesceEnv1(consts.EnvOperatorSTSPVCTemplateName, stsMeta.GetName())
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVCTemplate(pvcTplName, stsMeta, stsParams.PersistentVolumeClaim))
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

func createStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, stateful *appsv1.StatefulSet) error {
	_, err := cl.AppsV1().StatefulSets(namespace).Create(ctx, stateful, metav1.CreateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create statefulset")
		return err
	}
	return nil
}

func patchStatefulSet(ctx context.Context, storedStateful, newStateful *appsv1.StatefulSet, namespace string, recreateStatefulSet bool, deletePropagation *metav1.DeletionPropagation, cl kubernetes.Interface) error {
	return nil
}
