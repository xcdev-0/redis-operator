package statefulset

import (
	"testing"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFindTargetVolumeClaimTemplate_ReturnsErrorWhenOnlyNodeConfExists(t *testing.T) {
	templates := []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{Name: consts.VolumeNameNodeConf},
			Spec:       corev1.PersistentVolumeClaimSpec{},
		},
	}

	index, err := findTargetVolumeClaimTemplate(templates)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if index != -1 {
		t.Fatalf("expected index -1, got %d", index)
	}
}
