package v1beta2

import (
	"k8s.io/utils/ptr"
)

func (r *RedisCluster) SetDefault() {
	if r.Spec.ClientPort == nil {
		r.Spec.ClientPort = ptr.To(6379)
	}
	if r.Spec.RedisExporter != nil && r.Spec.RedisExporter.Port == nil {
		r.Spec.RedisExporter.Port = ptr.To(9121)
	}
}
