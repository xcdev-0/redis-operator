package statefulsetservice

// PersistenceCfg는 데이터 영속성 설정을 담는 구조체입니다.
// nil: 미지정(기본값 사용) / true/false: 명시
type PersistenceCfg struct {
	Enabled *bool
}

// RuntimeCfg는 런타임 설정을 담는 구조체입니다.
type RuntimeCfg struct {
	ClusterModeEnabled    bool
	NodeConfVolumeEnabled bool
}
