package k8smeta

import (
	"maps"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SetupType string

const (
	SetupTypeCluster SetupType = "cluster"
)

// StableLabelKeys는 StatefulSet이 Pod를 식별하는 데 사용하는 안정적인 라벨 키 목록입니다.
// 이 라벨들은 변경되지 않아야 합니다.
var StableLabelKeys = []string{
	consts.LabelKeyApp,
	consts.LabelKeyRole,
	consts.LabelKeyCluster,
}

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
	STSName          string
	Role             string
	AdditionalLabels map[string]string // 추가적인 라벨들
	ClusterName      string
}

// GetCurrentMasterSelectorLabels는 failover 등으로 클러스터 내 역할이 바뀔 수 있으므로,
// 실제 클러스터 내 역할(master/slave)을 나타내는 라벨로 Pod를 찾기 위한 셀렉터를 생성합니다.
// 주의: app과 role은 StatefulSet 이름과 역할이므로 실제 클러스터 내 역할을 나타내지 않습니다.
func GetCurrentMasterSelectorLabels(clusterName string, currentRole string) map[string]string {
	return map[string]string{
		consts.LabelKeyCurrentRole: currentRole, // redis-current-role: master
		consts.LabelKeyCluster:     clusterName, // cluster: clusterName
	}
}

// GetRedisClusterLabels는 Redis 클러스터 리소스에 사용할 라벨 맵을 생성합니다.
func GetRedisClusterLabels(rl *RedisLabels) map[string]string {
	newLabels := map[string]string{
		consts.LabelKeyApp:     rl.STSName,
		consts.LabelKeyRole:    rl.Role,
		consts.LabelKeyCluster: rl.ClusterName,
	}
	maps.Copy(newLabels, rl.AdditionalLabels)
	return newLabels
}

// GetRedisClusterStableLabels는 주어진 라벨 맵에서 안정적인 라벨만 추출합니다.
// StatefulSet selector와 일관성을 유지하기 위해 사용됩니다.
func GetRedisClusterStableLabels(stsName string, role string, clusterName string) map[string]string {
	return map[string]string{
		consts.LabelKeyApp:     stsName,
		consts.LabelKeyRole:    role,
		consts.LabelKeyCluster: clusterName,
	}
}
func GetRedisClusterStableLabelsFromLabels(labels map[string]string) map[string]string {
	return map[string]string{
		consts.LabelKeyApp:     labels[consts.LabelKeyApp],
		consts.LabelKeyRole:    labels[consts.LabelKeyRole],
		consts.LabelKeyCluster: labels[consts.LabelKeyCluster],
	}
}

func GetPVCSelectorLabels(stsName string) map[string]string {
	return map[string]string{
		consts.LabelKeyApp: stsName,
	}
}
