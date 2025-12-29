package redis

import (
	"context"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func configureRedisClient(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, podName string) *redis.Client {
	redisInfo := RedisDetails{
		PodName:   podName,
		Namespace: cr.Namespace,
	}
	var err error
	var pass string
	if cr.Spec.KubernetesConfig.ExistingPasswordSecret != nil {
		pass, err = getRedisPassword(
			ctx,
			client,
			cr.Namespace,
			*cr.Spec.KubernetesConfig.ExistingPasswordSecret.Name,
			*cr.Spec.KubernetesConfig.ExistingPasswordSecret.Key)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error in getting redis password")
		}
	}
	opts := &redis.Options{
		Addr:     getRedisServerAddress(ctx, client, redisInfo, *cr.Spec.Port),
		Password: pass,
		DB:       0,
	}
	if cr.Spec.TLS != nil {
		opts.TLSConfig = getRedisTLSConfig(ctx, client, cr.Namespace, cr.Spec.TLS.Secret.SecretName, redisInfo.PodName)
	}
	return redis.NewClient(opts)
}
