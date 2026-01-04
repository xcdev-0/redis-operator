package consts

const (
	RedisPort             = 6379
	RedisExporterPort     = 9121
	RedisExporterPortName = "redisutils-exporter"
	RedisBusPortName      = "redisutils-bus"
	RedisClientPortName   = "redisutils-client"
)

// Redis 기본 환경 변수 이름
const (
	// EnvRedisServerMode는 Redis 서버 모드를 설정합니다 (leader, follower, sentinel, cluster 등)
	EnvRedisServerMode = "SERVER_MODE"
	// EnvRedisSetupMode는 Init Container에서 사용하는 설정 모드입니다
	EnvRedisSetupMode = "SETUP_MODE"
	// EnvRedisMajorVersion는 Redis 클러스터 버전을 설정합니다 (예: "7", "6")
	EnvRedisMajorVersion = "REDIS_MAJOR_VERSION"
	// EnvRedisPort는 Redis 포트 번호를 설정합니다
	EnvRedisPort = "REDIS_PORT"
	// EnvRedisAddr는 Redis 연결 주소를 설정합니다 (예: redisutils://localhost:6379)
	EnvRedisAddr = "REDIS_ADDR"
	// EnvRedisPassword는 Redis 비밀번호를 설정합니다 (Secret에서 가져옴)
	EnvRedisPassword = "REDIS_PASSWORD"
	// EnvRedisMaxMemory는 Redis 최대 메모리 사용량을 설정합니다 (바이트 단위)
	EnvRedisMaxMemory = "REDIS_MAX_MEMORY"
)

// TLS 관련 환경 변수 이름
const (
	// EnvTLSMode는 TLS 모드 활성화 여부를 설정합니다
	EnvTLSMode = "TLS_MODE"
	// EnvTLSCAKey는 TLS CA 인증서 경로를 설정합니다
	EnvTLSCAKey = "REDIS_TLS_CA_KEY"
	// EnvTLSCert는 TLS 서버 인증서 경로를 설정합니다
	EnvTLSCert = "REDIS_TLS_CERT"
	// EnvTLSCertKey는 TLS 서버 개인키 경로를 설정합니다
	EnvTLSCertKey = "REDIS_TLS_CERT_KEY"
)

// ACL 관련 환경 변수 이름
const (
	// EnvACLMode는 ACL 모드 활성화 여부를 설정합니다
	EnvACLMode = "ACL_MODE"
)

// Persistence 관련 환경 변수 이름
const (
	// EnvPersistenceEnabled는 데이터 영속성 활성화 여부를 설정합니다
	EnvPersistenceEnabled = "PERSISTENCE_ENABLED"
)
