package clustermembership

import (
	"testing"
)

func TestClusterNode_HasFlagType(t *testing.T) {
	tests := []struct {
		name     string
		flags    string
		flagType string
		want     bool
	}{
		{
			name:     "정확히 일치하는 flag",
			flags:    "master,myself,fail",
			flagType: "fail",
			want:     true,
		},
		{
			name:     "첫 번째 flag",
			flags:    "master,myself",
			flagType: "master",
			want:     true,
		},
		{
			name:     "마지막 flag",
			flags:    "master,myself",
			flagType: "myself",
			want:     true,
		},
		{
			name:     "존재하지 않는 flag",
			flags:    "master,myself",
			flagType: "fail",
			want:     false,
		},
		{
			name:     "fail?는 fail과 다름",
			flags:    "master,fail?",
			flagType: "fail",
			want:     false, // 현재 구현으로는 false
		},
		{
			name:     "공백이 있는 경우",
			flags:    "master, fail, myself",
			flagType: "fail",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ClusterNode{
				Flags: tt.flags,
			}
			if got := node.HasFlagType(tt.flagType); got != tt.want {
				t.Errorf("HasFlagType() = %v, want %v (Flags: %s, flagType: %s)",
					got, tt.want, tt.flags, tt.flagType)
			}
		})
	}
}

func TestClusterNode_IsFailed(t *testing.T) {
	tests := []struct {
		name  string
		flags string
		want  bool
	}{
		{
			name:  "fail flag",
			flags: "master,fail",
			want:  true,
		},
		{
			name:  "fail? flag (PFAIL)",
			flags: "master,fail?",
			want:  true, // strings.Contains 때문에 true여야 함
		},
		{
			name:  "정상 master",
			flags: "master,myself",
			want:  false,
		},
		{
			name:  "정상 slave",
			flags: "slave",
			want:  false,
		},
		{
			name:  "fail 포함하지만 다른 문자열",
			flags: "master,failover",
			want:  false, // strings.Contains는 "fail?"를 찾으므로 failover는 안 잡힘
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ClusterNode{
				Flags: tt.flags,
			}
			if got := node.IsFailed(); got != tt.want {
				t.Errorf("IsFailed() = %v, want %v (Flags: %s)", got, tt.want, tt.flags)
			}
		})
	}
}

func TestClusterNode_IsFailedOrDisconnected(t *testing.T) {
	tests := []struct {
		name  string
		flags string
		state string
		want  bool
	}{
		{
			name:  "fail flag",
			flags: "master,fail",
			state: "connected",
			want:  true,
		},
		{
			name:  "fail? flag (PFAIL)",
			flags: "master,fail?",
			state: "connected",
			want:  true,
		},
		{
			name:  "disconnected state",
			flags: "master",
			state: "disconnected",
			want:  true,
		},
		{
			name:  "disconnected flag fallback",
			flags: "master,disconnected",
			state: "connected",
			want:  true,
		},
		{
			name:  "정상 노드",
			flags: "master,myself",
			state: "connected",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ClusterNode{
				Flags: tt.flags,
				State: tt.state,
			}
			if got := node.IsFailedOrDisconnected(); got != tt.want {
				t.Errorf("IsFailedOrDisconnected() = %v, want %v (Flags: %s, State: %s)",
					got, tt.want, tt.flags, tt.state)
			}
		})
	}
}

func TestClusterNode_IsLeader(t *testing.T) {
	tests := []struct {
		name  string
		flags string
		want  bool
	}{
		{
			name:  "master 노드",
			flags: "master,myself",
			want:  true,
		},
		{
			name:  "slave 노드",
			flags: "slave",
			want:  false,
		},
		{
			name:  "fail된 master",
			flags: "master,fail?",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ClusterNode{
				Flags: tt.flags,
			}
			if got := node.IsLeader(); got != tt.want {
				t.Errorf("IsLeader() = %v, want %v (Flags: %s)", got, tt.want, tt.flags)
			}
		})
	}
}
