package consts

const (
	RedisPort = 6379

	RedisExporterPort      = 9121
	RedisExporterPortName  = "redis-exporter"
	RedisExporterContainer = "redis-exporter"

	RedisBusPortName    = "redis-bus"
	RedisClientPortName = "redis-client"
)

// Redis 기본 환경 변수 이름
const (
	// REDIS_SERVER_MODE는 Redis 서버 모드를 설정합니다 (leader, follower, sentinel, cluster 등)
	REDIS_SERVER_MODE = "SERVER_MODE"
	// REDIS_SETUP_MODE는 Init Container에서 사용하는 설정 모드입니다
	REDIS_SETUP_MODE = "SETUP_MODE"
	// REDIS_MAJOR_VERSION는 Redis 클러스터 버전을 설정합니다 (예: "7", "6")
	REDIS_MAJOR_VERSION = "REDIS_MAJOR_VERSION"
	// REDIS_PORT는 Redis 포트 번호를 설정합니다
	REDIS_PORT = "REDIS_PORT"
	// REDIS_ADDR는 Redis 연결 주소를 설정합니다 (예: redisutils://localhost:6379)
	REDIS_ADDR = "REDIS_ADDR"
	// REDIS_PASSWORD는 Redis 비밀번호를 설정합니다 (Secret에서 가져옴)
	REDIS_PASSWORD = "REDIS_PASSWORD"
	// REDIS_MAX_MEMORY는 Redis 최대 메모리 사용량을 설정합니다 (바이트 단위)
	REDIS_MAX_MEMORY = "REDIS_MAX_MEMORY"
)

// TLS 관련 환경 변수 이름
const (
	// TLS_MODE는 TLS 모드 활성화 여부를 설정합니다
	TLS_MODE = "TLS_MODE"
	// REDIS_TLS_CA_CERT는 TLS CA 인증서 경로를 설정합니다
	REDIS_TLS_CA_CERT = "REDIS_TLS_CA_CERT"
	// REDIS_TLS_CERT는 TLS 서버 인증서 경로를 설정합니다
	REDIS_TLS_CERT = "REDIS_TLS_CERT"
	// REDIS_TLS_KEY는 TLS 서버 개인키 경로를 설정합니다
	REDIS_TLS_KEY = "REDIS_TLS_KEY"
)

// Persistence 관련 환경 변수 이름
const (
	// DATA_PERSISTENCE_ENABLED는 데이터 영속성 활성화 여부를 설정합니다
	DATA_PERSISTENCE_ENABLED = "PERSISTENCE_ENABLED"
)
