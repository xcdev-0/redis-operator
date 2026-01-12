package cluster

import "strconv"

// GetStatefulSetName은 Redis Cluster StatefulSet 이름을 생성합니다.
// 예: GetStatefulSetName("clustername", "leader") -> "clustername-leader"
func GetStatefulSetName(clusterName, role string) string {
	return clusterName + "-" + role
}

// 예: GetServiceName("clustername", "leader") ->
// "clustername-leader"
func GetServiceName(clusterName, role string) string {
	return clusterName + "-" + role
}

// 예: GetNodePortServiceName("clustername", "leader", 0) ->
// "clustername-leader-0"
func GetNodePortServiceName(clusterName, role string, index int) string {
	return clusterName + "-" + role + "-" + strconv.Itoa(index)
}

// GetPodName은 Redis Cluster Pod 이름을 생성합니다.
// 예: GetPodName("clustername", "leader", 0) -> "clustername-leader-0"
func GetPodName(clusterName, role string, index int) string {
	return clusterName + "-" + role + "-" + strconv.Itoa(index)
}

// GetExecutePodName은 첫 번째 Leader Pod 이름을 반환합니다.
// 이는 클러스터 초기화나 조회 작업에서 자주 사용됩니다.
// 예: GetExecutePodName("clustername") -> "clustername-leader-0"
func GetExecutePodName(clusterName string) string {
	return clusterName + "-leader-0"
}
