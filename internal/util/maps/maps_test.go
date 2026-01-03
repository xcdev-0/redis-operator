package maps

import (
	"reflect"
	"testing"
)

func TestCopy(t *testing.T) {
	t.Run("copy non-empty map", func(t *testing.T) {
		src := map[string]int{
			"a": 1,
			"b": 2,
			"c": 3,
		}
		copy := Copy(src)

		// 복사본이 원본과 같은 내용을 가지고 있는지 확인
		if !reflect.DeepEqual(src, copy) {
			t.Errorf("Copy() = %v, want %v", copy, src)
		}

		// 복사본과 원본이 독립적인지 확인 (원본 수정 시 복사본에 영향 없음)
		src["d"] = 4
		if _, ok := copy["d"]; ok {
			t.Error("Copy() should create an independent copy, but modification to src affected copy")
		}

		// 복사본 수정 시 원본에 영향 없음
		copy["e"] = 5
		if _, ok := src["e"]; ok {
			t.Error("Copy() should create an independent copy, but modification to copy affected src")
		}
	})

	t.Run("copy empty map", func(t *testing.T) {
		src := map[string]int{}
		copy := Copy(src)

		if copy == nil {
			t.Error("Copy() should return a non-nil empty map, got nil")
		}

		if len(copy) != 0 {
			t.Errorf("Copy() of empty map should return empty map, got length %d", len(copy))
		}
	})

	t.Run("copy nil map", func(t *testing.T) {
		var src map[string]int = nil
		copy := Copy(src)

		if copy == nil {
			t.Error("Copy() should return a non-nil empty map even for nil input, got nil")
		}

		if len(copy) != 0 {
			t.Errorf("Copy() of nil map should return empty map, got length %d", len(copy))
		}
	})

	t.Run("copy with different value types", func(t *testing.T) {
		src := map[int]string{
			1: "one",
			2: "two",
		}
		copy := Copy(src)

		if !reflect.DeepEqual(src, copy) {
			t.Errorf("Copy() = %v, want %v", copy, src)
		}
	})
}

func TestMerge(t *testing.T) {
	t.Run("merge multiple maps", func(t *testing.T) {
		m1 := map[string]int{"a": 1, "b": 2}
		m2 := map[string]int{"c": 3, "d": 4}
		m3 := map[string]int{"e": 5}

		result := Merge(m1, m2, m3)

		expected := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Merge() = %v, want %v", result, expected)
		}
	})

	t.Run("merge with overlapping keys - later values win", func(t *testing.T) {
		m1 := map[string]int{"a": 1, "b": 2}
		m2 := map[string]int{"b": 20, "c": 3}
		m3 := map[string]int{"c": 30, "d": 4}

		result := Merge(m1, m2, m3)

		expected := map[string]int{"a": 1, "b": 20, "c": 30, "d": 4}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Merge() = %v, want %v", result, expected)
		}
	})

	t.Run("merge empty maps", func(t *testing.T) {
		m1 := map[string]int{}
		m2 := map[string]int{}

		result := Merge(m1, m2)

		if result == nil {
			t.Error("Merge() should return a non-nil empty map, got nil")
		}

		if len(result) != 0 {
			t.Errorf("Merge() of empty maps should return empty map, got length %d", len(result))
		}
	})

	t.Run("merge nil maps", func(t *testing.T) {
		var m1 map[string]int = nil
		var m2 map[string]int = nil

		result := Merge(m1, m2)

		if result == nil {
			t.Error("Merge() should return a non-nil empty map even for nil inputs, got nil")
		}

		if len(result) != 0 {
			t.Errorf("Merge() of nil maps should return empty map, got length %d", len(result))
		}
	})

	t.Run("merge single map", func(t *testing.T) {
		m1 := map[string]int{"a": 1, "b": 2}

		result := Merge(m1)

		if !reflect.DeepEqual(result, m1) {
			t.Errorf("Merge() = %v, want %v", result, m1)
		}
	})

	t.Run("merge no maps", func(t *testing.T) {
		result := Merge[string, int]()

		if result == nil {
			t.Error("Merge() with no arguments should return a non-nil empty map, got nil")
		}

		if len(result) != 0 {
			t.Errorf("Merge() with no arguments should return empty map, got length %d", len(result))
		}
	})

	t.Run("merge with different key types", func(t *testing.T) {
		m1 := map[int]string{1: "one", 2: "two"}
		m2 := map[int]string{3: "three"}

		result := Merge(m1, m2)

		expected := map[int]string{1: "one", 2: "two", 3: "three"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Merge() = %v, want %v", result, expected)
		}
	})
}

func TestMergePreservingExistingKeys(t *testing.T) {
	t.Run("preserve existing keys in dst", func(t *testing.T) {
		dst := map[string]string{"a": "original", "b": "original"}
		src := map[string]string{"a": "new", "c": "new"}

		result := MergePreservingExistingKeys(dst, src)

		expected := map[string]string{"a": "original", "b": "original", "c": "new"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("MergePreservingExistingKeys() = %v, want %v", result, expected)
		}

		// dst가 수정되었는지 확인 (result와 dst가 같은 내용을 가져야 함)
		if !reflect.DeepEqual(result, dst) {
			t.Error("MergePreservingExistingKeys() should modify and return dst, but returned different map")
		}
	})

	t.Run("add new keys from src", func(t *testing.T) {
		dst := map[string]string{"a": "one"}
		src := map[string]string{"b": "two", "c": "three"}

		result := MergePreservingExistingKeys(dst, src)

		expected := map[string]string{"a": "one", "b": "two", "c": "three"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("MergePreservingExistingKeys() = %v, want %v", result, expected)
		}
	})

	t.Run("dst is nil", func(t *testing.T) {
		var dst map[string]string = nil
		src := map[string]string{"a": "one", "b": "two"}

		result := MergePreservingExistingKeys(dst, src)

		if result == nil {
			t.Error("MergePreservingExistingKeys() should return a new map when dst is nil, got nil")
		}

		expected := map[string]string{"a": "one", "b": "two"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("MergePreservingExistingKeys() = %v, want %v", result, expected)
		}
	})

	t.Run("src is nil", func(t *testing.T) {
		dst := map[string]string{"a": "one", "b": "two"}
		var src map[string]string = nil

		result := MergePreservingExistingKeys(dst, src)

		expected := map[string]string{"a": "one", "b": "two"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("MergePreservingExistingKeys() = %v, want %v", result, expected)
		}

		// dst가 변경되지 않았는지 확인
		if !reflect.DeepEqual(result, dst) {
			t.Error("MergePreservingExistingKeys() should return dst when src is nil, but returned different map")
		}
	})

	t.Run("both are nil", func(t *testing.T) {
		var dst map[string]string = nil
		var src map[string]string = nil

		result := MergePreservingExistingKeys(dst, src)

		if result != nil {
			t.Errorf("MergePreservingExistingKeys() with both nil should return nil, got %v", result)
		}
	})

	t.Run("src does not modify original", func(t *testing.T) {
		dst := map[string]string{"a": "one"}
		src := map[string]string{"b": "two"}

		// src의 원본 복사본 저장
		srcCopy := make(map[string]string)
		for k, v := range src {
			srcCopy[k] = v
		}

		MergePreservingExistingKeys(dst, src)

		// src가 변경되지 않았는지 확인
		if !reflect.DeepEqual(src, srcCopy) {
			t.Error("MergePreservingExistingKeys() should not modify src, but src was modified")
		}
	})

	t.Run("empty dst with non-empty src", func(t *testing.T) {
		dst := map[string]string{}
		src := map[string]string{"a": "one", "b": "two"}

		result := MergePreservingExistingKeys(dst, src)

		expected := map[string]string{"a": "one", "b": "two"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("MergePreservingExistingKeys() = %v, want %v", result, expected)
		}
	})

	t.Run("non-empty dst with empty src", func(t *testing.T) {
		dst := map[string]string{"a": "one", "b": "two"}
		src := map[string]string{}

		result := MergePreservingExistingKeys(dst, src)

		expected := map[string]string{"a": "one", "b": "two"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("MergePreservingExistingKeys() = %v, want %v", result, expected)
		}
	})
}
