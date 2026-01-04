package services

import (
	"context"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	utilmaps "github.com/xcdev-0/redis-operator/internal/util/maps"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ServiceOptions contains all options for creating or updating a Kubernetes service
type ServiceOptions struct {
	Namespace            string                       // Kubernetes namespace where the service will be created
	ServiceObjectMeta    metav1.ObjectMeta            // Service metadata (name, labels, annotations)
	OwnerRef             metav1.OwnerReference        // Owner reference for garbage collection
	ExporterPortProvider k8smeta.ExporterPortProvider // Function to get Redis exporter port if enabled
	Headless             bool                         // Whether to create a headless service (ClusterIP: None)
	ServiceType          string                       // Service type: "ClusterIP", "NodePort", or "LoadBalancer"
	ClientPort           int                          // Redis client port number
	K8sClient            kubernetes.Interface         // Kubernetes client for API operations
	ExtraPorts           []corev1.ServicePort         // Additional ports to expose (e.g., Redis bus port)
}

func generateServiceDef(opts ServiceOptions) *corev1.Service {
	service := &corev1.Service{
		TypeMeta:   k8smeta.GenerateTypeMeta("Service", "v1"),
		ObjectMeta: opts.ServiceObjectMeta,
		Spec: corev1.ServiceSpec{
			Type:      generateServiceType(opts.ServiceType),
			ClusterIP: "", // Empty string means Kubernetes will assign a ClusterIP
			Selector:  utilmaps.Copy(opts.ServiceObjectMeta.GetLabels()),
			Ports: []corev1.ServicePort{
				{
					Name:       consts.RedisClientPortName,
					Port:       int32(opts.ClientPort),
					TargetPort: intstr.FromInt(opts.ClientPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	// Set ClusterIP to "None" for headless services (used by StatefulSets)
	if opts.Headless {
		service.Spec.ClusterIP = "None"
	}

	// Add Redis Exporter port if metrics are enabled
	if exporterPort, ok := opts.ExporterPortProvider(); ok {
		redisExporterService := enableMetricsPort(exporterPort)
		service.Spec.Ports = append(service.Spec.Ports, *redisExporterService)
	}

	// Add extra ports (e.g., Redis Cluster bus port)
	if len(opts.ExtraPorts) > 0 {
		service.Spec.Ports = append(service.Spec.Ports, opts.ExtraPorts...)
	}

	// Set owner reference for garbage collection
	k8smeta.AddOwnerRefToObject(service, opts.OwnerRef)
	return service
}

// enableMetricsPort creates a ServicePort for Redis Exporter metrics endpoint
func enableMetricsPort(port int) *corev1.ServicePort {
	return &corev1.ServicePort{
		Name:       consts.RedisExporterPortName,
		Port:       int32(port),
		TargetPort: intstr.FromInt(port),
		Protocol:   corev1.ProtocolTCP,
	}
}

// generateServiceType converts a string service type to Kubernetes ServiceType
// Defaults to ClusterIP if an unknown type is provided
func generateServiceType(k8sServiceType string) corev1.ServiceType {
	switch k8sServiceType {
	case "LoadBalancer":
		return corev1.ServiceTypeLoadBalancer
	case "NodePort":
		return corev1.ServiceTypeNodePort
	case "ClusterIP":
		return corev1.ServiceTypeClusterIP
	default:
		return corev1.ServiceTypeClusterIP
	}
}

// createService creates a new Kubernetes Service
func createService(ctx context.Context, kusClient kubernetes.Interface, namespace string, service *corev1.Service) error {
	_, err := kusClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis service creation is failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis service creation is successful")
	return nil
}

// updateService updates an existing Kubernetes Service
func updateService(ctx context.Context, k8sClient kubernetes.Interface, namespace string, service *corev1.Service) error {
	_, err := k8sClient.CoreV1().Services(namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis service update failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis service updated successfully")
	return nil
}

// getService retrieves an existing Kubernetes Service by name
func getService(ctx context.Context, k8sClient kubernetes.Interface, namespace string, name string) (*corev1.Service, error) {
	serviceInfo, err := k8sClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis service get action is failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis service get action is successful")
	return serviceInfo, nil
}

func CreateOrUpdateService(ctx context.Context, opts ServiceOptions) error {
	serviceDef := generateServiceDef(opts)
	storedService, err := getService(ctx, opts.K8sClient, opts.Namespace, opts.ServiceObjectMeta.GetName())
	if err != nil {
		if errors.IsNotFound(err) {
			// Set last applied annotation for future comparisons
			if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(serviceDef); err != nil {
				log.FromContext(ctx).Error(err, "Unable to patch redisutils service with compare annotations")
			}
			return createService(ctx, opts.K8sClient, opts.Namespace, serviceDef)
		}
		return err
	}
	return patchService(ctx, storedService, serviceDef, opts.Namespace, opts.K8sClient)
}

func patchService(ctx context.Context, storedService *corev1.Service, newService *corev1.Service, namespace string, cl kubernetes.Interface) error {
	// Kubernetes가 관리하는 메타데이터 필드들을 보존하여 원자적 업데이트 보장
	// ResourceVersion은 낙관적 동시성 제어를 위해 필요합니다
	newService.ResourceVersion = storedService.ResourceVersion
	newService.CreationTimestamp = storedService.CreationTimestamp
	newService.ManagedFields = storedService.ManagedFields
	newService.Finalizers = storedService.Finalizers

	// ClusterIP는 Kubernetes가 할당하므로 변경하면 안 됩니다
	// ClusterIP 타입 서비스의 경우 기존 ClusterIP를 유지해야 합니다
	if newService.Spec.Type == generateServiceType("ClusterIP") {
		newService.Spec.ClusterIP = storedService.Spec.ClusterIP
	}

	// k8s-objectmatcher를 사용하여 기존 서비스와 새 서비스 간의 변경사항 계산
	// 이 라이브러리는 실제로 변경된 부분만 감지하여 불필요한 업데이트를 방지합니다
	patchResult, err := patch.DefaultPatchMaker.Calculate(storedService, newService,
		patch.IgnoreStatusFields(),      // status 필드는 Kubernetes가 자동 관리하므로 비교에서 제외
		patch.IgnoreField("kind"),       // kind는 항상 "Service"이므로 비교에서 제외
		patch.IgnoreField("apiVersion"), // apiVersion은 항상 "v1"이므로 비교에서 제외
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Unable to patch redisutils service with comparison object")
		return err
	}

	// 변경사항이 있는 경우에만 업데이트 수행
	if !patchResult.IsEmpty() {
		log.FromContext(ctx).V(1).Info("Changes in service Detected, Updating...", "patch", string(patchResult.Patch))

		// 기존 annotations와 labels를 보존하면서 새 값과 병합
		// 사용자가 수동으로 추가한 값들이 유지되도록 합니다
		utilmaps.MergePreservingExistingKeys(newService.Annotations, storedService.Annotations)
		utilmaps.MergePreservingExistingKeys(newService.Labels, storedService.Labels)

		// 현재 설정을 annotation에 저장하여 다음 reconcile 시 변경사항 감지에 사용
		// 이는 kubectl apply의 last-applied-configuration과 유사한 동작입니다
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(newService); err != nil {
			log.FromContext(ctx).Error(err, "Unable to patch redisutils service with comparison object")
			return err
		}
		log.FromContext(ctx).V(1).Info("Syncing Redis service with defined properties")
		return updateService(ctx, cl, namespace, newService)
	}

	// 변경사항이 없으면 업데이트를 건너뜀
	log.FromContext(ctx).V(1).Info("Redis service is already in-sync")
	return nil
}
