package redisservice

import (
	"context"
	"testing"
)

func TestEndpointInfo_String(t *testing.T) {
	tests := []struct {
		name     string
		endpoint *EndpointInfo
		expected string
	}{
		{
			name: "정상적인 엔드포인트",
			endpoint: &EndpointInfo{
				IP:   "10.0.0.1",
				Port: "6379",
			},
			expected: "10.0.0.1:6379",
		},
		{
			name: "IPv6 주소",
			endpoint: &EndpointInfo{
				IP:   "[2001:db8::1]",
				Port: "6379",
			},
			expected: "[2001:db8::1]:6379",
		},
		{
			name: "FQDN 주소",
			endpoint: &EndpointInfo{
				IP:   "redis-pod-0.redis-headless.default.svc.cluster.local",
				Port: "6379",
			},
			expected: "redis-pod-0.redis-headless.default.svc.cluster.local:6379",
		},
		{
			name:     "nil 엔드포인트",
			endpoint: nil,
			expected: "",
		},
		{
			name: "빈 문자열",
			endpoint: &EndpointInfo{
				IP:   "",
				Port: "",
			},
			expected: ":",
		},
		{
			name: "NodePort 형식",
			endpoint: &EndpointInfo{
				IP:   "192.168.1.100",
				Port: "30001",
			},
			expected: "192.168.1.100:30001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.endpoint.HostAndPort()
			if result != tt.expected {
				t.Errorf("EndpointInfo.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetEndpoint_ErrorHandling(t *testing.T) {
	// 이 테스트는 실제 Kubernetes 클러스터가 필요하므로
	// 통합 테스트로 작성하거나 mock을 사용해야 합니다.
	// 여기서는 기본적인 구조만 제공합니다.

	_ = context.Background() // ctx 변수 사용하지 않으므로 무시

	tests := []struct {
		name        string
		description string
		// 실제 구현 시 Kubernetes client mock이 필요합니다
	}{
		{
			name:        "nil Kubernetes client",
			description: "Kubernetes client가 nil일 때 에러 처리",
		},
		{
			name:        "존재하지 않는 Pod",
			description: "Pod가 존재하지 않을 때 에러 처리",
		},
		{
			name:        "Service 타입별 동작",
			description: "ClusterIP와 NodePort 타입에 따른 다른 동작",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test case: %s - %s", tt.name, tt.description)
			// TODO: Kubernetes client mock을 사용한 실제 테스트 구현
		})
	}
}

func TestGetEndPointIP_ErrorHandling(t *testing.T) {
	// 이 테스트는 실제 Kubernetes 클러스터가 필요하므로
	// 통합 테스트로 작성하거나 mock을 사용해야 합니다.
	// 여기서는 기본적인 구조만 제공합니다.

	_ = context.Background() // ctx 변수 사용하지 않으므로 무시

	tests := []struct {
		name        string
		description string
		// 실제 구현 시 Kubernetes client mock이 필요합니다
	}{
		{
			name:        "IP만 반환",
			description: "GetEndPointIP는 포트 없이 IP만 반환해야 함",
		},
		{
			name:        "NodePort 타입에서 Node IP 반환",
			description: "NodePort 타입일 때는 Pod IP가 아닌 Node IP를 반환",
		},
		{
			name:        "ClusterIP 타입에서 Pod IP 반환",
			description: "ClusterIP 타입일 때는 Pod IP를 반환",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Test case: %s - %s", tt.name, tt.description)
			// TODO: Kubernetes client mock을 사용한 실제 테스트 구현
		})
	}
}

func TestEndpointInfo_Compare(t *testing.T) {
	tests := []struct {
		name    string
		left    *EndpointInfo
		right   *EndpointInfo
		want    bool
		wantErr bool
	}{
		{
			name: "ipv6 bracket normalization",
			left: &EndpointInfo{
				IP:   "[2001:db8::1]",
				Port: "6379",
			},
			right: &EndpointInfo{
				IP:   "2001:db8::1",
				Port: "6379",
			},
			want: true,
		},
		{
			name: "fqdn lowercase trim normalization",
			left: &EndpointInfo{
				IP:   "10.0.0.1",
				Port: "6379",
				FQDN: strPtr(" Redis-0.Default.SVC.Cluster.Local "),
			},
			right: &EndpointInfo{
				IP:   "10.0.0.99",
				Port: "6379",
				FQDN: strPtr("redis-0.default.svc.cluster.local"),
			},
			want: true,
		},
		{
			name: "nil target returns error",
			left: &EndpointInfo{
				IP:   "10.0.0.1",
				Port: "6379",
			},
			right:   nil,
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.left.Compare(tt.right)
			if tt.wantErr && err == nil {
				t.Fatalf("Compare() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Compare() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(value string) *string {
	return &value
}
