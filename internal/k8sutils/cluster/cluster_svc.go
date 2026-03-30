package cluster

import (
	"context"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/util"
	utilmaps "github.com/xcdev-0/redis-operator/internal/util/maps"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RedisClusterService는 Redis Cluster Service 생성을 위한 파라미터를 담는 구조체입니다.
type RedisClusterService struct {
	role string // Service 역할 ("leader" 또는 "follower")
}

func (rcs RedisClusterService) CreateRedisClusterService(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	stsName := k8smeta.GetStatefulSetName(cr.Name, rcs.role)
	var epp k8smeta.ExporterPortProvider
	if cr.Spec.RedisExporter.IsEnabled() {
		epp = func() (port int, enable bool) {
			defaultP := ptr.To(consts.RedisExporterPort)
			exporterPort := *util.Coalesce(cr.Spec.RedisExporter.Port, defaultP)
			return exporterPort, cr.Spec.RedisExporter.IsEnabled()
		}
	} else {
		epp = k8smeta.DisableMetrics
	}

	labels := k8smeta.GetRedisClusterLabels(&k8smeta.RedisLabels{
		STSName:          stsName,
		Role:             rcs.role,
		AdditionalLabels: cr.Labels,
		ClusterName:      cr.Name,
	})

	selectorLabels := k8smeta.GetRedisClusterStableLabelsFromLabels(labels)

	// 1 cluster ip, 2 headless, 3 master 서비스는 항상 생성

	// 1. cluster ip
	// svv name: stsName
	// 클러스터 IP 서비스는 쿠버네티스 내부 클라이언트가 사용하는 서비스입니다.
	clusterIPObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:      stsName,
		Namespace: cr.Namespace,
		Labels:    labels,
		Annotations: k8smeta.GenerateServiceAnots(
			cr.ObjectMeta,
			cr.Spec.KubernetesConfig.GetServiceAnnotations(),
			k8smeta.DisableMetrics),
	})

	busPortNum := int32(cr.GetClientPort() + 10000)
	clusterIPBusPort := corev1.ServicePort{
		Name:     consts.RedisBusPortName, // redis-bus
		Port:     busPortNum,              // Service 포트
		Protocol: corev1.ProtocolTCP,
		TargetPort: intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: busPortNum, // Pod의 컨테이너 포트
		},
	}

	clusterIPExtraPorts := []corev1.ServicePort{}
	if cr.Spec.KubernetesConfig.ShouldIncludeBusPort() {
		clusterIPExtraPorts = append(clusterIPExtraPorts, clusterIPBusPort)
	}

	var err error
	opts := ServiceOptions{
		Namespace:            cr.Namespace,
		ServiceObjectMeta:    clusterIPObjectMeta,
		SelectorLabels:       selectorLabels,
		OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
		ExporterPortProvider: k8smeta.DisableMetrics,
		Headless:             false,
		ClientPort:           cr.GetClientPort(),
		K8sClient:            cl,
		ExtraPorts:           clusterIPExtraPorts,
	}
	def := opts.generateServiceDef()
	err = opts.createOrUpdateService(ctx, def)
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot create service for Redis", "Setup.Type", rcs.role)
		return err
	}

	// 2. headless service
	// svv name: stsName + "-headless"
	// 헤드리스 서비스는 클러스터 내부 통신을 위해 사용됩니다.
	headlessLabels := utilmaps.Copy(labels)
	if cr.Spec.RedisExporter.IsEnabled() {
		headlessLabels[consts.LabelKeyMetricsScrape] = "true"
	}
	headlessObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName + "-headless",
		Namespace:   cr.Namespace,
		Labels:      headlessLabels,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, epp),
	})
	// headlessExtraPorts := []corev1.ServicePort{clusterIPBusPort}
	headlessOpts := ServiceOptions{
		Namespace:            cr.Namespace,
		ServiceObjectMeta:    headlessObjectMeta,
		SelectorLabels:       selectorLabels,
		OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
		ExporterPortProvider: epp,
		Headless:             true,
		ClientPort:           cr.GetClientPort(),
		K8sClient:            cl,
		ExtraPorts:           []corev1.ServicePort{},
	}
	headlessDef := headlessOpts.generateServiceDef()
	err = headlessOpts.createOrUpdateService(ctx, headlessDef)
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot create headless service for Redis", "Setup.Type", rcs.role)
		return err
	}

	// 3. master service
	// svv name: cr.Name + "-master"
	// 일반 Service는 StatefulSet 역할(leader/follower)을 기반으로 선택하지만,
	// Master Service는 실제 Redis 역할(master/slave)을 기반으로 선택합니다.
	// 따라서 redis-current-role: master 라벨을 사용하여 실제 master 역할을 하는 Pod를 선택합니다.
	masterSelectorLables := k8smeta.GetCurrentMasterSelectorLabels(cr.Name, consts.LabelValueMaster)
	masterObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        cr.Name + "-master",
		Namespace:   cr.Namespace,
		Labels:      masterSelectorLables,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, k8smeta.DisableMetrics),
	})

	masterOpts := ServiceOptions{
		Namespace:            cr.Namespace,
		ServiceObjectMeta:    masterObjectMeta,
		SelectorLabels:       masterSelectorLables,
		OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
		ExporterPortProvider: k8smeta.DisableMetrics,
		Headless:             false,
		ClientPort:           cr.GetClientPort(),
		K8sClient:            cl,
		ExtraPorts:           []corev1.ServicePort{},
	}
	masterDef := masterOpts.generateServiceDef()
	err = masterOpts.createOrUpdateService(ctx, masterDef)
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot create master service for Redis", "Setup.Type", rcs.role)
		return err
	}

	return nil
}

type ServiceOptions struct {
	Namespace            string                       // Kubernetes namespace where the service will be created
	ServiceObjectMeta    metav1.ObjectMeta            // Service metadata (name, labels, annotations)
	SelectorLabels       map[string]string            // Selector labels for the service
	OwnerRef             metav1.OwnerReference        // Owner reference for garbage collection
	ExporterPortProvider k8smeta.ExporterPortProvider // Function to get Redis exporter port if enabled
	Headless             bool                         // Whether to create a headless service (ClusterIP: None)
	ClientPort           int                          // Redis client port number
	K8sClient            kubernetes.Interface         // Kubernetes client for API operations
	ExtraPorts           []corev1.ServicePort         // Additional ports to expose (e.g., Redis bus port)
}

func (opts ServiceOptions) createOrUpdateService(ctx context.Context, serviceDef *corev1.Service) error {
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

func (opts ServiceOptions) generateServiceDef() *corev1.Service {
	service := &corev1.Service{
		TypeMeta:   k8smeta.GenerateTypeMeta("Service", "v1"),
		ObjectMeta: opts.ServiceObjectMeta,
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "", // Empty string means Kubernetes will assign a ClusterIP
			Selector:  utilmaps.Copy(opts.SelectorLabels),
			Ports: []corev1.ServicePort{
				{
					Name:       consts.RedisClientPortName, // redis-client
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
		redisExporterService := &corev1.ServicePort{
			Name:       consts.RedisExporterPortName,
			Port:       int32(exporterPort),
			TargetPort: intstr.FromInt(exporterPort),
			Protocol:   corev1.ProtocolTCP,
		}
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
