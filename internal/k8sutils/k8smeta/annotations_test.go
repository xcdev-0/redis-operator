package k8smeta

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateServiceAnots_DisableMetrics(t *testing.T) {
	got := GenerateServiceAnots(
		metav1.ObjectMeta{Name: "sample"},
		nil,
		DisableMetrics,
	)

	if _, ok := got["prometheus.io/scrape"]; ok {
		t.Fatalf("prometheus scrape annotation must not exist when metrics are disabled")
	}
	if _, ok := got["prometheus.io/port"]; ok {
		t.Fatalf("prometheus port annotation must not exist when metrics are disabled")
	}
}

func TestGenerateServiceAnots_EnableMetrics(t *testing.T) {
	provider := func() (int, bool) {
		return 19121, true
	}
	got := GenerateServiceAnots(
		metav1.ObjectMeta{Name: "sample"},
		nil,
		provider,
	)

	if got["prometheus.io/scrape"] != "true" {
		t.Fatalf("expected prometheus.io/scrape=true, got %q", got["prometheus.io/scrape"])
	}
	if got["prometheus.io/port"] != "19121" {
		t.Fatalf("expected prometheus.io/port=19121, got %q", got["prometheus.io/port"])
	}
}

func TestGenerateServiceAnots_NilProvider(t *testing.T) {
	got := GenerateServiceAnots(
		metav1.ObjectMeta{Name: "sample"},
		nil,
		nil,
	)

	if _, ok := got["prometheus.io/scrape"]; ok {
		t.Fatalf("prometheus scrape annotation must not exist when provider is nil")
	}
	if _, ok := got["prometheus.io/port"]; ok {
		t.Fatalf("prometheus port annotation must not exist when provider is nil")
	}
}
