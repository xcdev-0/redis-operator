package cluster

import (
	"context"
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
			expected: false, // 현재 구현은 IPv6 대괄호를 처리하지 않음
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
			ctx := context.Background()
			result := checkRedisNodePresence(ctx, tt.nodes, tt.podIP)
			if result != tt.expected {
				t.Errorf("checkRedisNodePresence() = %v, want %v", result, tt.expected)
			}
		})
	}
}
