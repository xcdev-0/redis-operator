package cluster

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
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
			return cr.Spec.RedisExporter.GetPort(), cr.Spec.RedisExporter.IsEnabled()
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

	// 1. cluster ip
	// svv name: stsName
	clusterIPObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName,
		Namespace:   cr.Namespace,
		Labels:      labels,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, epp),
	})

	clusterIPBusPort := corev1.ServicePort{
		Name:     consts.RedisBusPortName,            // redis-bus
		Port:     int32(*cr.Spec.ClientPort + 10000), // Service 포트
		Protocol: corev1.ProtocolTCP,
		TargetPort: intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: int32(*cr.Spec.ClientPort + 10000), // Pod의 컨테이너 포트
		},
	}

	clusterIPExtraPorts := []corev1.ServicePort{}
	if cr.Spec.KubernetesConfig.ShouldIncludeBusPort() {
		clusterIPExtraPorts = append(clusterIPExtraPorts, clusterIPBusPort)
	}

	var err error
	err = createOrUpdateService(ctx, ServiceOptions{
		Namespace:            cr.Namespace,
		ServiceObjectMeta:    clusterIPObjectMeta,
		SelectorLabels:       selectorLabels,
		OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
		ExporterPortProvider: epp,
		Headless:             false,
		ServiceType:          "ClusterIP",
		ClientPort:           *cr.Spec.ClientPort,
		K8sClient:            cl,
		ExtraPorts:           clusterIPExtraPorts,
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot create service for Redis", "Setup.Type", rcs.role)
		return err
	}

	// 2. headless service
	// svv name: stsName + "-headless"
	headlessObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName + "-headless",
		Namespace:   cr.Namespace,
		Labels:      labels,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, epp),
	})
	headlessExtraPorts := []corev1.ServicePort{}
	if cr.Spec.KubernetesConfig.ShouldIncludeBusPortForHeadless() {
		headlessExtraPorts = append(headlessExtraPorts, clusterIPBusPort)
	}
	err = createOrUpdateService(ctx, ServiceOptions{
		Namespace:            cr.Namespace,
		ServiceObjectMeta:    headlessObjectMeta,
		OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
		ExporterPortProvider: k8smeta.DisableMetrics,
		Headless:             true,
		ServiceType:          "ClusterIP",
		ClientPort:           *cr.Spec.ClientPort,
		K8sClient:            cl,
		ExtraPorts:           headlessExtraPorts,
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot create headless service for Redis", "Setup.Type", rcs.role)
		return err
	}

	// 3. additional service
	// svv name: stsName + "-additional"
	additionalObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName + "-additional",
		Namespace:   cr.Namespace,
		Labels:      labels,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, cr.Spec.KubernetesConfig.GetServiceAnnotations(), epp),
	})
	additionalExtraPorts := []corev1.ServicePort{}
	if cr.Spec.KubernetesConfig.ShouldIncludeBusPortForAdditional() {
		additionalExtraPorts = append(additionalExtraPorts, clusterIPBusPort)
	}
	if cr.Spec.KubernetesConfig.ShouldCreateAdditionalService() {
		err = createOrUpdateService(ctx, ServiceOptions{
			Namespace:            cr.Namespace,
			ServiceObjectMeta:    additionalObjectMeta,
			OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
			ExporterPortProvider: k8smeta.DisableMetrics,
			Headless:             false,
			ServiceType:          cr.Spec.KubernetesConfig.GetServiceType(),
			ClientPort:           *cr.Spec.ClientPort,
			K8sClient:            cl,
			ExtraPorts:           additionalExtraPorts,
		})
		if err != nil {
			log.FromContext(ctx).Error(err, "Cannot create additional service for Redis", "Setup.Type", rcs.role)
			return err
		}
	}
	// 4. master service
	// svv name: cr.Name + "-master"
	// 일반 Service는 StatefulSet 역할(leader/follower)을 기반으로 선택하지만,
	// Master Service는 실제 Redis 역할(master/slave)을 기반으로 선택합니다.
	// 따라서 redis-current-role: master 라벨을 사용하여 실제 master 역할을 하는 Pod를 선택합니다.
	masterSelectorLables := k8smeta.GetCurrentMasterSelectorLabels(cr.Name, consts.LabelValueMaster)
	masterObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        cr.Name + "-master",
		Namespace:   cr.Namespace,
		Labels:      masterSelectorLables,
		Annotations: k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, epp),
	})

	err = createOrUpdateService(ctx, ServiceOptions{
		Namespace:            cr.Namespace,
		ServiceObjectMeta:    masterObjectMeta,
		SelectorLabels:       masterSelectorLables,
		OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
		ExporterPortProvider: k8smeta.DisableMetrics,
		Headless:             false,
		ServiceType:          "ClusterIP",
		ClientPort:           *cr.Spec.ClientPort,
		K8sClient:            cl,
		ExtraPorts:           []corev1.ServicePort{},
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot create master service for Redis", "Setup.Type", rcs.role)
		return err
	}

	// 5. nodeport type 일경우 추가로 생성
	if cr.Spec.KubernetesConfig.GetServiceType() == "NodePort" {
		err = createOrUpdateClusterNodePortService(ctx, cr, cl, rcs.role)
		if err != nil {
			log.FromContext(ctx).Error(err, "Cannot create nodeport service for Redis", "Setup.Type", rcs.role)
			return err
		}
	}
	return nil
}

func createOrUpdateClusterNodePortService(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface, role string) error {
	var replicaCount int32
	if role == "leader" {
		replicaCount = cr.Spec.GetLeaderReplicaCount()
	} else {
		replicaCount = cr.Spec.GetFollowerReplicaCount()
	}

	// 각 Pod마다 개별 NodePort Service 생성
	for i := 0; i < int(replicaCount); i++ {
		podName := k8smeta.GetPodName(cr.Name, role, i)
		serviceLabels := k8smeta.GetRedisClusterLabels(
			&k8smeta.RedisLabels{
				STSName: k8smeta.GetStatefulSetName(cr.Name, role),
				Role:    role,
				AdditionalLabels: map[string]string{
					// sts가 팟에게 자동으로 붙이는 레이블, 특정 팟만 선택하기 위해 사용
					"statefulset.kubernetes.io/pod-name": podName,
				},
				ClusterName: cr.Name,
			})
		serviceAnnotations := k8smeta.GenerateServiceAnots(cr.ObjectMeta, nil, k8smeta.DisableMetrics)
		serviceObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
			Name:        k8smeta.GetNodePortServiceName(cr.Name, role, i),
			Namespace:   cr.Namespace,
			Labels:      serviceLabels,
			Annotations: serviceAnnotations,
		})

		// Redis Cluster Bus 포트 정의
		busPort := corev1.ServicePort{
			Name:     consts.RedisBusPortName,            // redis-bus
			Port:     int32(*cr.Spec.ClientPort + 10000), // Service 포트
			Protocol: corev1.ProtocolTCP,
			TargetPort: intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: int32(*cr.Spec.ClientPort + 10000), // Pod의 컨테이너 포트
			},
		}
		// NodePort Service 생성 (각 Pod마다 고유한 NodePort 할당)
		err := createOrUpdateService(ctx, ServiceOptions{
			Namespace:            cr.Namespace,
			ServiceObjectMeta:    serviceObjectMeta,
			OwnerRef:             k8smeta.RedisClusterAsOwner(cr),
			ExporterPortProvider: k8smeta.DisableMetrics,
			Headless:             false,
			ServiceType:          "NodePort",
			ClientPort:           *cr.Spec.ClientPort,
			ExtraPorts:           []corev1.ServicePort{busPort},
		})
		if err != nil {
			log.FromContext(ctx).Error(err, "Cannot create nodeport service for Redis", "Setup.Type", role)
			return err
		}
	}
	return nil
}
