package cluster

import (
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"strings"

	"github.com/redis/go-redis/v9"
)

// ClusterNode는 CLUSTER NODES 명령의 응답을 파싱한 구조체입니다.
// 형식: <nodeid> <ip:port@cport> <flags> <master-id> <ping> <pong> <epoch> <state> <slots>
type ClusterNode struct {
	// node id
	NodeID string
	// 10.0.0.1:6379@16379,redis-cluster-leader-0.<headless-svc>.svc.cluster.local"
	AddressAndHostName string
	// master,myself,fail
	Flags string
	// master node id (slave인 경우, 아니면 "-")
	MasterID string
	// ping time
	Ping string
	// pong time
	Pong string
	// epoch
	Epoch string
	// connected, disconnected
	State string
	// slots
	Slots []string
}

// Redis 명령어: CLUSTER NODES
func GetClusterNodes(ctx context.Context, redisClient *redis.Client) ([]ClusterNode, error) {
	output, err := redisClient.ClusterNodes(ctx).Result()
	// <nodeid> <ip:port@cport> <flags> <master-id> <ping> <pong> <epoch> <state> <slots>
	if err != nil {
		return nil, err
	}

	csvOutput := csv.NewReader(strings.NewReader(output))
	// 구분자 설정
	csvOutput.Comma = ' '
	// 레코드별로 필드 수 달라도 되도록
	csvOutput.FieldsPerRecord = -1
	csvOutputRecords, err := csvOutput.ReadAll()
	if err != nil {
		return nil, err
	}

	response := make([]ClusterNode, 0, len(csvOutputRecords))
	for _, record := range csvOutputRecords {
		if len(record) < 8 {
			// 최소 필수 필드가 없으면 스킵
			continue
		}

		node := ClusterNode{
			NodeID:             record[0],
			AddressAndHostName: record[1],
			Flags:              record[2],
			MasterID:           record[3],
			Ping:               record[4],
			Pong:               record[5],
			Epoch:              record[6],
			State:              record[7],
		}

		// 슬롯은 8번째 인덱스부터 시작 (여러 개일 수 있음)
		if len(record) > 8 {
			node.Slots = record[8:]
		} else {
			node.Slots = []string{}
		}

		response = append(response, node)
	}
	return response, nil
}

func (node *ClusterNode) IsLeader() bool {
	return node.HasFlagType("master")
}
func (node *ClusterNode) IsFollower() bool {
	return node.HasFlagType("slave")
}
func (node *ClusterNode) IsFailed() bool {
	// "fail" 또는 "fail?" (PFAIL) 모두 감지
	return node.HasFlagType("fail") || strings.Contains(node.Flags, "fail?")
}
func (node *ClusterNode) IsFailedOrDisconnected() bool {
	return node.IsFailed() || node.HasFlagType("disconnected")
}

// GetIP는 ClusterNode의 AddressAndHostName에서 IP를 추출합니다.
//   - 호스트네임이 있는 경우 (Redis v6+ with cluster-announce-hostname):
//     "10.0.0.1:6379@16379,redis-cluster-leader-0.svc.cluster.local" → "10.0.0.1"
//     "[2001:db8::1]:6379@16379,redis-cluster-leader-0.svc.cluster.local" → "2001:db8::1"
//   - 호스트네임이 없는 경우 (Redis v6 이하 또는 호스트네임 미설정):
//     "10.0.0.1:6379@16379" → "10.0.0.1"
//     "[2001:db8::1]:6379@16379" → "2001:db8::1"
func (node *ClusterNode) GetIP() (string, error) {
	parts := strings.Split(node.AddressAndHostName, ",")

	// 첫 번째 부분은 항상 "ip:port@cport" 또는 "[ipv6]:port@cport" 형식
	if len(parts) == 0 {
		return "", fmt.Errorf("empty address field")
	}

	// "10.0.0.1:6379@16379" 또는 "[2001:db8::1]:6379@16379"
	ipPortCPort := parts[0]

	// @ 기준으로 split해서 "ip:port" 부분만 추출
	ipPortParts := strings.Split(ipPortCPort, "@")
	if len(ipPortParts) == 0 {
		return "", fmt.Errorf("invalid address format: %s", node.AddressAndHostName)
	}
	ipPort := ipPortParts[0] // "10.0.0.1:6379" 또는 "[2001:db8::1]:6379"

	// net.SplitHostPort를 사용하면 IPv4/IPv6 모두 안전하게 처리 가능
	host, _, err := net.SplitHostPort(ipPort)
	if err != nil {
		return "", fmt.Errorf("failed to parse host:port from %s: %w", ipPort, err)
	}

	if host == "" {
		return "", fmt.Errorf("failed to extract IP from address: %s", node.AddressAndHostName)
	}

	return host, nil
}

// GetHostname은 ClusterNode의 AddressAndHostName에서 호스트네임을 추출합니다.
//   - 호스트네임이 있는 경우 (Redis v7+ with cluster-announce-hostname):
//     "10.0.0.1:6379@16379,redis-cluster-leader-0.svc.cluster.local" → "redis-cluster-leader-0.svc.cluster.local"
//   - 호스트네임이 없는 경우:
//     "10.0.0.1:6379@16379" → ""
func (node *ClusterNode) GetHostname() string {
	parts := strings.Split(node.AddressAndHostName, ",")
	if len(parts) < 2 {
		return "" // 호스트네임 없음
	}
	hostname := strings.TrimSpace(parts[1])
	return hostname
}

// ex: master,myself,fail
func (node *ClusterNode) HasFlagType(flagType string) bool {
	for _, value := range strings.Split(node.Flags, ",") {
		if strings.TrimSpace(value) == flagType {
			return true
		}
	}
	return false
}
