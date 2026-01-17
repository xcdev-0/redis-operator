package cluster

import (
	"context"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterInfoSnapshot is a small parsed view of INFO cluster.
type ClusterInfoSnapshot struct {
	State         string
	KnownNodes    int
	SlotsAssigned int
}

// parseInfoCluster parses `INFO cluster` text into a snapshot.
func parseInfoCluster(info string) ClusterInfoSnapshot {
	s := ClusterInfoSnapshot{
		State:         "",
		KnownNodes:    -1,
		SlotsAssigned: -1,
	}

	// INFO output uses \r\n line endings typically.
	lines := strings.Split(info, "\r\n")
	if len(lines) == 1 {
		lines = strings.Split(info, "\n")
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}

		key := kv[0]
		val := kv[1]

		switch key {
		case "cluster_state":
			s.State = val
		case "cluster_known_nodes":
			if n, err := strconv.Atoi(val); err == nil {
				s.KnownNodes = n
			}
		case "cluster_slots_assigned":
			if n, err := strconv.Atoi(val); err == nil {
				s.SlotsAssigned = n
			}
		}
	}

	return s
}

// countClusterNodesLines counts non-empty lines of CLUSTER NODES output.
func countClusterNodesLines(nodes string) int {
	lines := strings.Split(strings.TrimSpace(nodes), "\n")
	cnt := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			cnt++
		}
	}
	return cnt
}

// CheckIfClusterAlreadyBootstrapped returns true if the node shows any sign
// that cluster creation/meet/slot assignment already happened.
// This should be used to guard `redis-cli --cluster create` from re-running.
func CheckIfClusterAlreadyBootstrapped(ctx context.Context, redisClient *redis.Client) (bool, *ClusterInfoSnapshot, int, error) {
	logger := log.FromContext(ctx)

	// 1) INFO cluster
	info, err := redisClient.Info(ctx, "cluster").Result()
	if err != nil {
		// If INFO fails, be conservative: we can't prove it's safe to create.
		logger.Error(err, "failed to run INFO cluster")
		return true, nil, 0, err
	}

	snap := parseInfoCluster(info)

	// 2) CLUSTER NODES
	nodesOut, err := redisClient.ClusterNodes(ctx).Result()
	if err != nil {
		// If CLUSTER NODES fails, also be conservative.
		logger.Error(err, "failed to run CLUSTER NODES")
		return true, &snap, 0, err
	}

	nodeLines := countClusterNodesLines(nodesOut)

	already := false
	if snap.SlotsAssigned > 0 {
		already = true // 슬롯이 할당되었으면 이미 초기화된 것으로 간주
	}
	if snap.KnownNodes > 1 {
		already = true // 노드가 다른 노드와 만났으면 이미 초기화된 것으로 간주
	}
	if nodeLines > 1 {
		already = true // 노드 라인이 1개 이상이면 이미 초기화된 것으로 간주
	}

	logger.V(1).Info("cluster bootstrap check",
		"cluster_state", snap.State,
		"known_nodes", snap.KnownNodes,
		"slots_assigned", snap.SlotsAssigned,
		"cluster_nodes_lines", nodeLines,
		"already_bootstrapped", already,
	)

	return already, &snap, nodeLines, nil
}

// CheckIfClusterAlreadyBootstrappedFromK8s는 Kubernetes client를 사용하여
// 클러스터가 이미 초기화되었는지 확인합니다.
// 모든 leader Pod를 확인하여 하나라도 클러스터가 초기화되었으면 true를 반환합니다.
func CheckIfClusterAlreadyBootstrappedFromK8s(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) (bool, *ClusterInfoSnapshot, int, error) {
	logger := log.FromContext(ctx)
	replicaCount := cr.Spec.GetLeaderReplicaCount()

	// 모든 leader Pod를 확인
	for podIndex := 0; podIndex < int(replicaCount); podIndex++ {
		podName := GetPodName(cr.Name, "leader", podIndex)
		redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, podName)

		alreadyBootstrapped, snap, nodeLines, err := CheckIfClusterAlreadyBootstrapped(ctx, redisClient)
		redisClient.Close()

		if err != nil {
			// 에러가 발생해도 다음 Pod를 확인
			logger.V(1).Info("Failed to check cluster bootstrap status on pod, trying next pod", "Pod", podName, "Error", err)
			continue
		}

		if alreadyBootstrapped {
			// 하나라도 초기화되었으면 true 반환
			return true, snap, nodeLines, nil
		}
	}

	// 모든 Pod를 확인했지만 초기화되지 않음
	return false, nil, 0, nil
}
