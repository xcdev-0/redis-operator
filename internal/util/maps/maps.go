package maps

func Copy[K comparable, V any](src map[K]V) map[K]V {
	return Merge(src)
}

// all type: []map[K]V
func Merge[K comparable, V any](all ...map[K]V) map[K]V {
	result := make(map[K]V)
	for _, m := range all {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// dst를 기준으로 병합 src에서 dst에 없는 키만 추가
// dst는 수정 대상, src는 읽기 전용(원래 값에 영향 가면 안됨)
func MergePreservingExistingKeys(dst, src map[string]string) map[string]string {
	if dst == nil {
		if src == nil {
			return nil
		}
		dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
	return dst
}
