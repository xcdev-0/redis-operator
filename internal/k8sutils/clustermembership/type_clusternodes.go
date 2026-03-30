package clustermembership

import (
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
)

// ClusterNode는 CLUSTER NODES 명령의 응답을 파싱한 구조체입니다.
// 형식: <nodeid> <ip:port@cport> <flags> <master-id> <ping> <pong> <epoch> <state> <slots>
type ClusterNode struct {
	// node id
	NodeID string
	// 10.0.0.1:6379@16379,redis-cluster-leader-0.<headless-svc>.svc.cluster.local"
	address string
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
			NodeID:   record[0],
			address:  record[1],
			Flags:    record[2],
			MasterID: record[3],
			Ping:     record[4],
			Pong:     record[5],
			Epoch:    record[6],
			State:    record[7],
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
	// Redis CLUSTER NODES의 link state는 별도 state 필드(connected/disconnected)에 기록됩니다.
	// 일부 환경의 비표준 출력 호환을 위해 flags의 disconnected도 보조적으로 확인합니다.
	return node.IsFailed() || strings.EqualFold(strings.TrimSpace(node.State), "disconnected") || node.HasFlagType("disconnected")
}

func (node *ClusterNode) GetEndpoint() (*redisservice.EndpointInfo, error) {
	parts := strings.Split(node.address, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty address field")
	}

	// ip:port@cport,hostname
	ipPortAndBusPort := parts[0]
	ipPortParts := strings.Split(ipPortAndBusPort, "@")
	if len(ipPortParts) == 0 {
		return nil, fmt.Errorf("invalid address format: %s", ipPortAndBusPort)
	}
	ip, port, err := net.SplitHostPort(ipPortParts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to split host and port: %w", err)
	}

	endpoint := &redisservice.EndpointInfo{
		IP:   ip,
		Port: port,
	}

	if len(parts) >= 2 {
		fqdn := parts[1]
		endpoint.FQDN = &fqdn
	}

	return endpoint, nil
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
