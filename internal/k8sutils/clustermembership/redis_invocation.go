package clustermembership

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
)

type ClusterStatus struct {
	SlotsAssigned int // Total number of assigned slots
}

type RedisInvocation struct {
	Command      []string // e.g. {"redis-cli", "--cluster", "create"}
	Flags        []string // e.g. {"-h", "localhost", "-p", "6379"}
	RedisCommand []string // e.g. {"CLUSTER", "ADDSLOTS", "1", "2", "3"}
}

func (r *RedisInvocation) AddCommand(command []string) {
	r.Command = append(r.Command, command...)
}
func (r *RedisInvocation) AddFlags(flags []string) {
	r.Flags = append(r.Flags, flags...)
}

func (r *RedisInvocation) Args() []string {
	args := append([]string{}, r.Command...)
	args = append(args, r.Flags...)
	args = append(args, r.RedisCommand...)
	return args
}

func (ri *RedisInvocation) AddAuthAndTLS(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) *RedisInvocation {
	ri.AddFlags(redisservice.GetRedisPasswordArgs(ctx, client, cr.Namespace, cr.Spec.KubernetesConfig.ExistingPasswordSecret))
	ri.AddFlags(redisservice.GetRedisTLSArgs(cr.Spec.TLS))
	return ri
}
