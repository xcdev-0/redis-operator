package k8smeta

import (
	"maps"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SetupType string

const (
	Cluster SetupType = "cluster"
)

// 이 라벨들은 변경되지 않아야 하며, StatefulSet이 Pod를 식별하는 데 사용됩니다.
var StableLabelKeys = []string{"app", "redis_setup_type", "role"}

func RedisClusterAsOwner(cr *rcvb2.RedisCluster) metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: cr.APIVersion,
		Kind:       cr.Kind,
		Name:       cr.Name,
		UID:        cr.UID,
		Controller: &trueVar,
	}
}

type RedisLabels struct {
	Name      string
	SetupType SetupType
	Role      string
	Labels    map[string]string
}

func GetRedisLabels(rl *RedisLabels) map[string]string {
	newRedisLabels := map[string]string{
		"app":              rl.Name,
		"redis_setup_type": string(rl.SetupType),
		"role":             rl.Role,
	}
	maps.Copy(newRedisLabels, rl.Labels)
	return newRedisLabels
}

func ExtractStatefulSetSelectorLabels(givenLabels map[string]string) map[string]string {
	stableLabels := make(map[string]string)

	for _, key := range StableLabelKeys {
		if value, exists := givenLabels[key]; exists {
			stableLabels[key] = value
		}
	}

	return stableLabels
}
func LabelSelectors(labels map[string]string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: labels}
}
