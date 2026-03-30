package clusterresource

import (
	"context"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	utilmaps "github.com/xcdev-0/redis-operator/internal/util/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func createService(ctx context.Context, kusClient kubernetes.Interface, namespace string, service *corev1.Service) error {
	_, err := kusClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis service creation is failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis service creation is successful")
	return nil
}

func updateService(ctx context.Context, k8sClient kubernetes.Interface, namespace string, service *corev1.Service) error {
	_, err := k8sClient.CoreV1().Services(namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis service update failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis service updated successfully")
	return nil
}

func getService(ctx context.Context, k8sClient kubernetes.Interface, namespace string, name string) (*corev1.Service, error) {
	serviceInfo, err := k8sClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis service get action is failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis service get action is successful")
	return serviceInfo, nil
}

func patchService(ctx context.Context, storedService *corev1.Service, newService *corev1.Service, namespace string, cl kubernetes.Interface) error {
	newService.ResourceVersion = storedService.ResourceVersion
	newService.CreationTimestamp = storedService.CreationTimestamp
	newService.ManagedFields = storedService.ManagedFields
	newService.Finalizers = storedService.Finalizers

	if newService.Spec.Type == corev1.ServiceTypeClusterIP {
		newService.Spec.ClusterIP = storedService.Spec.ClusterIP
	}

	patchResult, err := patch.DefaultPatchMaker.Calculate(storedService, newService,
		patch.IgnoreStatusFields(),
		patch.IgnoreField("kind"),
		patch.IgnoreField("apiVersion"),
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Unable to patch redis service with comparison object")
		return err
	}

	if !patchResult.IsEmpty() {
		log.FromContext(ctx).V(1).Info("Changes in service Detected, Updating...", "patch", string(patchResult.Patch))

		utilmaps.MergePreservingExistingKeys(newService.Annotations, storedService.Annotations)
		utilmaps.MergePreservingExistingKeys(newService.Labels, storedService.Labels)

		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(newService); err != nil {
			log.FromContext(ctx).Error(err, "Unable to patch redis service with comparison object")
			return err
		}
		log.FromContext(ctx).V(1).Info("Syncing Redis service with defined properties")
		return updateService(ctx, cl, namespace, newService)
	}

	log.FromContext(ctx).V(1).Info("Redis service is already in-sync")
	return nil
}
