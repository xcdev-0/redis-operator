package redisservice

import "testing"

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
