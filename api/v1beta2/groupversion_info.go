/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package v1beta2 contains API Schema definitions for the cluster v1beta2 API group.
// +kubebuilder:object:generate=true
// +groupName=ejlabs.in

// NOTE: +groupName 주석은 controller-gen이 'make manifests' 실행 시 CRD YAML 파일을 생성할 때 사용됩니다.
// 이 값이 CRD의 spec.group 필드가 되며, apiVersion의 group 부분을 결정합니다.
// 예: apiVersion: ejlabs.in/v1beta2

package v1beta2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion은 Operator 런타임에 사용되는 API Group과 Version을 정의합니다.
	// Operator가 Kubernetes API 서버와 통신하여 RedisCluster 리소스를 조회/생성/수정할 때 이 값을 사용합니다.
	// 이 값은 +groupName 주석과 반드시 일치해야 합니다.
	GroupVersion = schema.GroupVersion{Group: "ejlabs.in", Version: "v1beta2"}

	// SchemeBuilder는 이 API Group의 타입들을 Kubernetes Scheme에 등록하는 데 사용됩니다.
	// Scheme은 GVK(GroupVersionKind)와 Go 타입 간의 매핑 정보를 담고 있으며,
	// Operator가 리소스를 직렬화/역직렬화할 때 필요합니다.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme은 이 API Group의 모든 타입을 주어진 Scheme에 등록하는 헬퍼 함수입니다.
	// main() 함수에서 manager 생성 시 호출되며, Operator가 RedisCluster 타입을 인식할 수 있게 합니다.
	// 사용 예: AddToScheme(mgr.GetScheme())
	AddToScheme = SchemeBuilder.AddToScheme
)
