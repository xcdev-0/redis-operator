package clusterresource

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

func CreateRedisLeaderService(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	return createRedisClusterServices(ctx, cr, cl, "leader")
}

func CreateRedisFollowerService(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	return createRedisClusterServices(ctx, cr, cl, "follower")
}

func createRedisClusterServices(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface, role string) error {
	stsName := k8smeta.GetStatefulSetName(cr.Name, role)
	exporterPortProvider := getExporterPortProvider(cr)

	labels := k8smeta.GetRedisClusterLabels(&k8smeta.RedisLabels{
		STSName:          stsName,
		Role:             role,
		AdditionalLabels: cr.Labels,
		ClusterName:      cr.Name,
	})

	selectorLabels := k8smeta.GetRedisClusterStableLabelsFromLabels(labels)
	ownerRef := k8smeta.RedisClusterAsOwner(cr)

	// 1 cluster ip, 2 headless, 3 master 서비스는 항상 생성

	// 1. cluster ip
	// svc name: stsName
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

	clusterIPDef := buildServiceDef(clusterIPObjectMeta, selectorLabels, ownerRef, k8smeta.DisableMetrics, false, cr.GetClientPort(), clusterIPExtraPorts)
	if err := createOrUpdateServiceDef(ctx, cl, cr.Namespace, clusterIPDef); err != nil {
		log.FromContext(ctx).Error(err, "Cannot create service for Redis", "Setup.Type", role)
		return err
	}

	// 2. headless service
	// svc name: stsName + "-headless"
	// 헤드리스 서비스는 클러스터 내부 통신을 위해 사용됩니다.
	headlessLabels := utilmaps.Copy(labels)
	if cr.Spec.RedisExporter.IsEnabled() {
		headlessLabels[consts.LabelKeyMetricsScrape] = "true"
	}
	headlessObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName + "-headless",
		Namespace:   cr.Namespace,
		Labels:      headlessLabels,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, exporterPortProvider),
	})
	headlessDef := buildServiceDef(headlessObjectMeta, selectorLabels, ownerRef, exporterPortProvider, true, cr.GetClientPort(), nil)
	if err := createOrUpdateServiceDef(ctx, cl, cr.Namespace, headlessDef); err != nil {
		log.FromContext(ctx).Error(err, "Cannot create headless service for Redis", "Setup.Type", role)
		return err
	}

	// 3. master service
	// svc name: cr.Name + "-master"
	// 일반 Service는 StatefulSet 역할(leader/follower)을 기반으로 선택하지만,
	// Master Service는 실제 Redis 역할(master/slave)을 기반으로 선택합니다.
	// 따라서 redis-current-role: master 라벨을 사용하여 실제 master 역할을 하는 Pod를 선택합니다.
	masterSelectorLabels := k8smeta.GetCurrentMasterSelectorLabels(cr.Name, consts.LabelValueMaster)
	masterObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        cr.Name + "-master",
		Namespace:   cr.Namespace,
		Labels:      masterSelectorLabels,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, k8smeta.DisableMetrics),
	})

	masterDef := buildServiceDef(masterObjectMeta, masterSelectorLabels, ownerRef, k8smeta.DisableMetrics, false, cr.GetClientPort(), nil)
	if err := createOrUpdateServiceDef(ctx, cl, cr.Namespace, masterDef); err != nil {
		log.FromContext(ctx).Error(err, "Cannot create master service for Redis", "Setup.Type", role)
		return err
	}

	return nil
}

func getExporterPortProvider(cr *rcvb2.RedisCluster) k8smeta.ExporterPortProvider {
	if !cr.Spec.RedisExporter.IsEnabled() {
		return k8smeta.DisableMetrics
	}

	return func() (port int, enable bool) {
		defaultP := ptr.To(consts.RedisExporterPort)
		exporterPort := *util.Coalesce(cr.Spec.RedisExporter.Port, defaultP)
		return exporterPort, cr.Spec.RedisExporter.IsEnabled()
	}
}

func createOrUpdateServiceDef(ctx context.Context, client kubernetes.Interface, namespace string, serviceDef *corev1.Service) error {
	storedService, err := getService(ctx, client, namespace, serviceDef.GetName())
	if err != nil {
		if errors.IsNotFound(err) {
			// Set last applied annotation for future comparisons
			if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(serviceDef); err != nil {
				log.FromContext(ctx).Error(err, "Unable to patch redis service with compare annotations")
			}
			return createService(ctx, client, namespace, serviceDef)
		}
		return err
	}
	return patchService(ctx, storedService, serviceDef, namespace, client)
}

func buildServiceDef(
	serviceObjectMeta metav1.ObjectMeta,
	selectorLabels map[string]string,
	ownerRef metav1.OwnerReference,
	exporterPortProvider k8smeta.ExporterPortProvider,
	headless bool,
	clientPort int,
	extraPorts []corev1.ServicePort,
) *corev1.Service {
	service := &corev1.Service{
		TypeMeta:   k8smeta.GenerateTypeMeta("Service", "v1"),
		ObjectMeta: serviceObjectMeta,
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "", // Empty string means Kubernetes will assign a ClusterIP
			Selector:  utilmaps.Copy(selectorLabels),
			Ports: []corev1.ServicePort{
				{
					Name:       consts.RedisClientPortName, // redis-client
					Port:       int32(clientPort),
					TargetPort: intstr.FromInt(clientPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	// Set ClusterIP to "None" for headless services (used by StatefulSets)
	if headless {
		service.Spec.ClusterIP = "None"
	}

	// Add Redis Exporter port if metrics are enabled
	if exporterPort, ok := exporterPortProvider(); ok {
		redisExporterService := &corev1.ServicePort{
			Name:       consts.RedisExporterPortName,
			Port:       int32(exporterPort),
			TargetPort: intstr.FromInt(exporterPort),
			Protocol:   corev1.ProtocolTCP,
		}
		service.Spec.Ports = append(service.Spec.Ports, *redisExporterService)
	}

	// Add extra ports (e.g., Redis Cluster bus port)
	if len(extraPorts) > 0 {
		service.Spec.Ports = append(service.Spec.Ports, extraPorts...)
	}

	// Set owner reference for garbage collection
	k8smeta.AddOwnerRefToObject(service, ownerRef)
	return service
}
