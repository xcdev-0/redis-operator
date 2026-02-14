package cluster

import (
	"testing"
)

func TestCheckRedisNodePresence(t *testing.T) {
	tests := []struct {
		name     string
		nodes    []ClusterNode
		podIP    string
		expected bool
	}{
		{
			name: "노드가 존재하는 경우 - 기본 형식",
			nodes: []ClusterNode{
				{AddressAndHostName: "10.0.0.1:6379@16379"},
				{AddressAndHostName: "10.0.0.2:6379@16379"},
				{AddressAndHostName: "10.0.0.3:6379@16379"},
			},
			podIP:    "10.0.0.2",
			expected: true,
		},
		{
			name: "노드가 존재하지 않는 경우",
			nodes: []ClusterNode{
				{AddressAndHostName: "10.0.0.1:6379@16379"},
				{AddressAndHostName: "10.0.0.2:6379@16379"},
			},
			podIP:    "10.0.0.99",
			expected: false,
		},
		{
			name:     "빈 노드 목록",
			nodes:    []ClusterNode{},
			podIP:    "10.0.0.1",
			expected: false,
		},
		{
			name: "첫 번째 노드와 일치",
			nodes: []ClusterNode{
				{AddressAndHostName: "192.168.1.100:6379@16379"},
				{AddressAndHostName: "192.168.1.101:6379@16379"},
			},
			podIP:    "192.168.1.100",
			expected: true,
		},
		{
			name: "마지막 노드와 일치",
			nodes: []ClusterNode{
				{AddressAndHostName: "192.168.1.100:6379@16379"},
				{AddressAndHostName: "192.168.1.101:6379@16379"},
				{AddressAndHostName: "192.168.1.102:6379@16379"},
			},
			podIP:    "192.168.1.102",
			expected: true,
		},
		{
			name: "다른 포트 번호",
			nodes: []ClusterNode{
				{AddressAndHostName: "10.0.0.1:6380@16380"},
				{AddressAndHostName: "10.0.0.2:6379@16379"},
			},
			podIP:    "10.0.0.1",
			expected: true,
		},
		{
			name: "IPv6 주소 형식 (대괄호 포함)",
			nodes: []ClusterNode{
				{AddressAndHostName: "[2001:db8::1]:6379@16379"},
				{AddressAndHostName: "[2001:db8::2]:6379@16379"},
			},
			podIP:    "2001:db8::1",
			expected: true,
		},
		{
			name: "여러 노드 중 중간 노드와 일치",
			nodes: []ClusterNode{
				{AddressAndHostName: "10.0.0.1:6379@16379"},
				{AddressAndHostName: "10.0.0.2:6379@16379"},
				{AddressAndHostName: "10.0.0.3:6379@16379"},
				{AddressAndHostName: "10.0.0.4:6379@16379"},
			},
			podIP:    "10.0.0.3",
			expected: true,
		},
		{
			name: "부분 일치가 아닌 정확한 IP 일치만",
			nodes: []ClusterNode{
				{AddressAndHostName: "10.0.0.10:6379@16379"},
			},
			podIP:    "10.0.0.1",
			expected: false,
		},
		{
			name: "빈 Address 필드",
			nodes: []ClusterNode{
				{AddressAndHostName: ""},
				{AddressAndHostName: "10.0.0.1:6379@16379"},
			},
			podIP:    "10.0.0.1",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkRedisNodePresence(tt.nodes, tt.podIP)
			if result != tt.expected {
				t.Errorf("checkRedisNodePresence() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCountClusterMemberNodes(t *testing.T) {
	tests := []struct {
		name     string
		nodes    []ClusterNode
		expected int32
	}{
		{
			name: "master/slave only counts as members",
			nodes: []ClusterNode{
				{NodeID: "m1", Flags: "master"},
				{NodeID: "s1", Flags: "slave"},
				{NodeID: "m2", Flags: "master,fail?"},
				{NodeID: "h1", Flags: "handshake"},
				{NodeID: "n1", Flags: "noaddr"},
			},
			expected: 3,
		},
		{
			name: "deduplicates by node id",
			nodes: []ClusterNode{
				{NodeID: "m1", Flags: "master"},
				{NodeID: "m1", Flags: "master,fail?"},
				{NodeID: "s1", Flags: "slave"},
			},
			expected: 2,
		},
		{
			name: "falls back to address key when node id is empty",
			nodes: []ClusterNode{
				{NodeID: "", AddressAndHostName: "10.0.0.1:6379@16379", Flags: "master"},
				{NodeID: "", AddressAndHostName: "10.0.0.1:6379@16379", Flags: "master,fail?"},
				{NodeID: "", AddressAndHostName: "10.0.0.2:6379@16379", Flags: "slave"},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countClusterMemberNodes(tt.nodes)
			if got != tt.expected {
				t.Fatalf("countClusterMemberNodes() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestCountAliveLeaderNodes(t *testing.T) {
	tests := []struct {
		name     string
		nodes    []ClusterNode
		expected int32
	}{
		{
			name: "counts only connected masters",
			nodes: []ClusterNode{
				{NodeID: "m1", Flags: "master", State: "connected"},
				{NodeID: "m2", Flags: "master,fail?", State: "connected"},
				{NodeID: "m3", Flags: "master", State: "disconnected"},
				{NodeID: "s1", Flags: "slave", State: "connected"},
			},
			expected: 1,
		},
		{
			name: "deduplicates by node id",
			nodes: []ClusterNode{
				{NodeID: "m1", Flags: "master", State: "connected"},
				{NodeID: "m1", Flags: "master", State: "connected"},
				{NodeID: "m2", Flags: "master", State: "connected"},
			},
			expected: 2,
		},
		{
			name: "falls back to address key when node id is empty",
			nodes: []ClusterNode{
				{NodeID: "", AddressAndHostName: "10.0.0.1:6379@16379", Flags: "master", State: "connected"},
				{NodeID: "", AddressAndHostName: "10.0.0.1:6379@16379", Flags: "master", State: "connected"},
				{NodeID: "", AddressAndHostName: "10.0.0.2:6379@16379", Flags: "master", State: "connected"},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countAliveLeaderNodes(tt.nodes)
			if got != tt.expected {
				t.Fatalf("countAliveLeaderNodes() = %d, want %d", got, tt.expected)
			}
		})
	}
}
