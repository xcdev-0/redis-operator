package v1beta2

type RedisClusterState string

const (
	RedisClusterInitializing RedisClusterState = "Initializing"
	RedisClusterBootstrap    RedisClusterState = "Bootstrap"
	RedisClusterReady        RedisClusterState = "Ready"
	RedisClusterFailed       RedisClusterState = "Failed"
)
const (
	InitializingClusterLeaderReason   string = "RedisCluster is initializing leaders"
	InitializingClusterFollowerReason string = "RedisCluster is initializing followers"
	BootstrapClusterReason            string = "RedisCluster is bootstrapping"
	ReadyClusterReason                string = "RedisCluster is ready"
)
const (
	EventReasonRedisClusterDownscale = "RedisClusterDownscale"
)

// +kubebuilder:subresource:status
type RedisClusterStatus struct {
	State  RedisClusterState `json:"state,omitempty"`
	Reason string            `json:"reason,omitempty"`
	// +kubebuilder:default=0
	ReadyLeaderReplicas int32 `json:"readyLeaderReplicas,omitempty"`
	// +kubebuilder:default=0
	ReadyFollowerReplicas int32 `json:"readyFollowerReplicas,omitempty"`
}
