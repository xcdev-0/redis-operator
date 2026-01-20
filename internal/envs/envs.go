package envs

import (
	"os"
	"strconv"
	"sync"

	"github.com/xcdev-0/redis-operator/internal/util"
)

const (
	WatchNamespaceEnv = "WATCH_NAMESPACE"

	MaxConcurrentReconcilesEnv = "MAX_CONCURRENT_RECONCILES"

	EnableWebhooksEnv = "ENABLE_WEBHOOKS"

	ServiceDNSDomain = "SERVICE_DNS_DOMAIN"

	InitContainerImageEnv = "INIT_CONTAINER_IMAGE"
)

var (
	initContainerImage     string
	initContainerImageOnce sync.Once
)

func GetInitContainerImage() string {
	initContainerImageOnce.Do(func() {
		val := os.Getenv(InitContainerImageEnv)
		initContainerImage = util.Coalesce(val, "docker.io/onssing/rediscluster-operator:latest")
	})
	return initContainerImage
}

func IsWebhookEnabled() bool {
	if v, err := strconv.ParseBool(os.Getenv(EnableWebhooksEnv)); err == nil {
		return v
	}
	return true
}

func GetMaxConcurrentReconciles(defaultVal int) int {
	if str := os.Getenv(MaxConcurrentReconcilesEnv); str != "" {
		if intVal, err := strconv.Atoi(str); err == nil {
			return intVal
		}
	}
	return defaultVal
}
func GetServiceDNSDomain() string {
	return util.Coalesce(os.Getenv(ServiceDNSDomain), "cluster.local")
}
