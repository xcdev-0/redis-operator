package controller

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func finalizeRedisClusterPVC(ctx context.Context, ctrlclient client.Client, cr *rcvb2.RedisCluster) error {
	return nil
}
func HandleRedisClusterFinalizer(ctx context.Context, ctrlclient client.Client, cr *rcvb2.RedisCluster, finalizer string) error {
	if cr.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(cr, finalizer) {
			if cr.Spec.Storage != nil && !cr.Spec.Storage.KeepAfterDelete {
				if err := finalizeRedisClusterPVC(ctx, ctrlclient, cr); err != nil {
					return err
				}
			}
			controllerutil.RemoveFinalizer(cr, finalizer)
			if err := ctrlclient.Update(ctx, cr); err != nil {
				log.FromContext(ctx).Error(err, "Could not remove finalizer "+finalizer)
				return err
			}
		}
	}
	return nil
}

func addFinalizer(ctx context.Context, cr client.Object, finalizer string, cl client.Client) error {
	if !controllerutil.ContainsFinalizer(cr, finalizer) {
		controllerutil.AddFinalizer(cr, finalizer)
		return cl.Update(ctx, cr)
	}
	return nil
}
