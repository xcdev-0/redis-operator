package helper

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SetupType string

const (
	SetupTypeReplication SetupType = "replication"
)

// 이 라벨들은 변경되지 않아야 하며, StatefulSet이 Pod를 식별하는 데 사용됩니다.
var StableLabelKeys = []string{"app", "redis_setup_type", "role"}

func GetRedisLabels(name string, st SetupType, role string, labels map[string]string) map[string]string {
	newRedisLabels := map[string]string{
		"app":              name,
		"redis_setup_type": string(st),
		"role":             role,
	}
	maps.Copy(newRedisLabels, labels)
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
