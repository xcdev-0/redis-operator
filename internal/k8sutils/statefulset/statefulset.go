package statefulset

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
)

type StatefulSet interface {
	IsStatefulSetReady(ctx context.Context, namespace, name string) bool
	GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32
}

type StatefulSetService struct {
	kubeClient kubernetes.Interface
}

// interface implementation
// IsStatefulSet, GetStatefulSetReplicas
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

func NewStatefulSetService(kubeClient kubernetes.Interface) *StatefulSetService {
	return &StatefulSetService{
		kubeClient: kubeClient,
	}
}

// StatefulSetRequest는 CreateOrUpdateStateFul 함수에 전달되는 모든 매개변수를 그룹화합니다.
type StatefulSetRequest struct {
	Namespace           string
	StsObjectMeta       metav1.ObjectMeta
	OwnerReference      metav1.OwnerReference
	StsParams           StatefulSetParameters
	InitContainerParams InitContainerParameters
	ContainerParams     ContainerParameters
}

func CreateOrUpdateStateFul(ctx context.Context,
	kubeClient kubernetes.Interface,
	req *StatefulSetRequest) error {
	storedStateful, err := getStatefulSet(ctx, kubeClient, req.Namespace, req.StsObjectMeta.Name)
	statefulSetDef := generateStatefulSetDef(
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
		return err
	}
	return patchStatefulSet(ctx, storedStateful, statefulSetDef, req.Namespace, req.StsParams.RecreateStatefulSet, req.StsParams.RecreateStatefulsetStrategy, kubeClient)
}

func getStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, name string) (*appsv1.StatefulSet, error) {
	statefulInfo, err := cl.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis statefulset get action failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset get action was successful")
	return statefulInfo, nil
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
