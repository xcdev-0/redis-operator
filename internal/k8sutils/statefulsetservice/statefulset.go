package statefulsetservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/xc/redis-operator/api/v1beta2"
	helper "github.com/xc/redis-operator/internal/k8sutils/helper"
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
	cl kubernetes.Interface,
	namespace string,
	stsMeta metav1.ObjectMeta,
	params statefulSetParameters,
	ownerDef metav1.OwnerReference,
	initcontainerParams initContainerParameters,
	containerParams containerParameters,
	sidecars *[]v1beta2.Sidecar) error {

	storedStateful, err := GetStatefulSet(ctx, cl, namespace, stsMeta.Name)
	statefulSetDef := generateStatefulSet(stsMeta, params, ownerDef, initcontainerParams, containerParams, getSidecars(sidecars))
	if err != nil {
		if apierrors.IsNotFound(err) {
			// StatefulSet이 존재하지 않는 경우에만 어노테이션 설정
			if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(statefulSetDef); err != nil {
				log.FromContext(ctx).Error(err, "Unable to patch redis statefulset with comparison object")
				return err
			}
			return createStatefulSet(ctx, cl, namespace, statefulSetDef)
		}
		// 다른 에러는 바로 반환
		return err
	}
	return patchStatefulSet(ctx, storedStateful, statefulSetDef, namespace, params.RecreateStatefulSet, params.RecreateStatefulsetStrategy, cl)
}

func generateStatefulSet(
	stsMeta metav1.ObjectMeta,
	params statefulSetParameters,
	ownerDef metav1.OwnerReference,
	initcontainerParams initContainerParameters,
	containerParams containerParameters,
	sidecars []v1beta2.Sidecar) *appsv1.StatefulSet {

	selectorLabels := helper.ExtractStatefulSetSelectorLabels(stsMeta.GetLabels())

	statefulset := &appsv1.StatefulSet{
		TypeMeta:   helper.GenerateMetaInformation("StatefulSet", "apps/v1"),
		ObjectMeta: stsMeta,
		Spec: appsv1.StatefulSetSpec{
			Selector:                             helper.LabelSelectors(selectorLabels),
			ServiceName:                          fmt.Sprintf("%s-headless", stsMeta.Name),
			Replicas:                             params.Replicas,
			UpdateStrategy:                       params.UpdateStrategy,
			PersistentVolumeClaimRetentionPolicy: params.PersistentVolumeClaimRetentionPolicy,
			MinReadySeconds:                      params.MinReadySeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      stsMeta.GetLabels(),
					Annotations: helper.GenerateStatefulSetsAnots(stsMeta, params.IgnoreAnnotations),
				},
			},
		},
	}
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
