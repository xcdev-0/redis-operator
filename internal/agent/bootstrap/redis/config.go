package bootstrap

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Showmax/go-fqdn"
	agentutil "github.com/xcdev-0/redis-operator/internal/agent/util"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	util "github.com/xcdev-0/redis-operator/internal/util"
)

const defaultRedisConfig = `
bind 0.0.0.0 ::
tcp-backlog 511
timeout 0
tcp-keepalive 300
daemonize no
supervised no
pidfile /var/run/redisutils.pid
`

func applyAuth(cfg *agentutil.Config) {
	if val, ok := util.CoalesceEnv(consts.REDIS_PASSWORD, ""); ok && val != "" {
		cfg.Append("masterauth", val)       // Master-Replica 복제 인증
		cfg.Append("requirepass", val)      // 클라이언트 인증
		cfg.Append("protected-mode", "yes") // 보호 모드 활성화
	} else {
		fmt.Println("Redis is running without password which is not recommended")
		cfg.Append("protected-mode", "no") // 보호 모드 비활성화 (비밀번호 없이 접근 가능)
	}
}
func applyCluster(cfg *agentutil.Config,
	nodePortEnabled string,
	redisMajorVersion string,
	nodeConfDir string) {

	// 클러스터 노드 간 통신을 위한 IP 주소 설정
	var err error
	var clusterAnnounceIP string
	if nodePortEnabled == "true" {
		// NodePort 모드: 노드의 실제 IP 주소 사용
		clusterAnnounceIP = os.Getenv("HOST_IP")
	} else {
		// 일반 모드: Pod의 로컬 IP 주소 사용
		clusterAnnounceIP, err = util.GetLocalIP()
		if err != nil {
			log.Printf("Warning: Failed to get local IP: %v", err)
		}
	}
	if clusterAnnounceIP != "" {
		// 클러스터 노드들이 서로 통신할 때 사용할 IP 주소
		cfg.Append("cluster-announce-ip", clusterAnnounceIP)
	}

	// cluster-announce-hostname: Redis 클러스터 노드가 호스트명을 사용하도록 설정
	// - Redis v7+: 공식 지원
	// - Redis v6+: 지원되지만 일부 버전에서는 동작하지 않을 수 있음
	// - Redis v5 이하: 지원하지 않음 (설정해도 무시됨)
	fqdnName, err := fqdn.FqdnHostname() // FQDN (Fully Qualified Domain Name) 가져오기
	if err != nil {
		log.Printf("Warning: Failed to get FQDN: %v", err)
	} else {
		// 클러스터 노드들이 서로 통신할 때 사용할 호스트명
		cfg.Append("cluster-announce-hostname", fqdnName)
	}

	nodeConfPath := filepath.Join(nodeConfDir, "nodes.conf")

	// 클러스터 기본 설정
	cfg.Append("cluster-enabled", "yes")              // 클러스터 모드 활성화
	cfg.Append("cluster-node-timeout", "5000")        // 노드 타임아웃 (5초): 이 시간 동안 응답 없으면 노드를 실패로 간주
	cfg.Append("cluster-require-full-coverage", "no") // 전체 슬롯 커버리지 불필요: 일부 슬롯이 없어도 클러스터 동작
	cfg.Append("cluster-migration-barrier", "1")      // 마이그레이션 배리어: 리더가 1개만 남아도 마이그레이션 허용
	cfg.Append("cluster-config-file", nodeConfPath)   // 클러스터 설정 파일 경로

	// nodes.conf 파일의 IP 주소 업데이트
	if ip, err := util.GetLocalIP(); err != nil {
		log.Printf("Warning: Failed to get local IP: %v", err)
	} else {
		// nodes.conf 파일에서 "myself" 라인의 IP 주소를 현재 Pod IP로 업데이트
		_, err = updateMyselfIP(nodeConfPath, strings.TrimSpace(ip))
		if err != nil {
			log.Printf("Warning: Failed to update nodes.conf: %v", err)
		}
	}
}

// applyTLS는 TLS 암호화 설정을 적용합니다.
// TLS를 활성화하면 클라이언트와 Redis 서버 간 통신이 암호화됩니다.
// 프로덕션 환경에서는 보안을 위해 TLS를 사용하는 것이 강력히 권장됩니다!
func applyTLS(cfg *agentutil.Config, tlsMode, redisMajorVersion, nodeport string) {
	if tlsMode != "true" {
		fmt.Println("Running without TLS mode")
		return
	}

	// TLS 인증서 파일 경로 설정
	cfg.Append("tls-cert-file", util.CoalesceEnv1(consts.REDIS_TLS_CERT, ""))       // 서버 인증서
	cfg.Append("tls-key-file", util.CoalesceEnv1(consts.REDIS_TLS_KEY, ""))         // 서버 개인키
	cfg.Append("tls-ca-cert-file", util.CoalesceEnv1(consts.REDIS_TLS_CA_CERT, "")) // CA 인증서 (클라이언트 인증용)
	cfg.Append("tls-auth-clients", "optional")                                      // 클라이언트 인증: optional (선택적)
	cfg.Append("tls-replication", "yes")                                            // Master-Replica 복제 시 TLS 사용
	cfg.Append("tls-cluster", "yes")                                                // 클러스터 노드 간 통신 시 TLS 사용

	// Redis v7 + 일반 모드: 호스트명을 우선 사용 (IP 변경에 더 안정적)
	if redisMajorVersion == "v7" && nodeport == "false" {
		cfg.Append("cluster-preferred-endpoint-type", "hostname")
	}
}

// applyACL는 ACL (Access Control List) 설정을 적용합니다.
// ACL을 사용하면 사용자별로 접근 권한을 세밀하게 제어할 수 있습니다.
// 예: 특정 키에만 읽기 권한, 특정 명령어만 실행 가능 등
func applyACL(cfg *agentutil.Config) {
	if aclMode := util.CoalesceEnv1(consts.ACL_MODE, ""); aclMode == "true" {
		cfg.Append("aclfile", "/etc/redis/user.acl") // ACL 규칙이 저장된 파일 경로
	} else {
		fmt.Println("ACL_MODE is not true, skipping ACL file modification")
	}
}

// applyPersistence는 데이터 영속화 (Persistence) 설정을 적용합니다.
func applyPersistence(cfg *agentutil.Config, persistenceEnabled, dataDir string) {
	if persistenceEnabled != "true" {
		fmt.Println("Running without persistence mode")
		return
	}

	// RDB 스냅샷 저장 규칙 (주기적으로 메모리 내용을 디스크에 저장)
	// 형식: save <초> <변경된 키 개수>
	cfg.Append("save", "900 1")    // 900초(15분) 동안 1개 이상 키가 변경되면 저장
	cfg.Append("save", "300 10")   // 300초(5분) 동안 10개 이상 키가 변경되면 저장
	cfg.Append("save", "60 10000") // 60초(1분) 동안 10000개 이상 키가 변경되면 저장

	// AOF (Append Only File) 활성화
	// 모든 쓰기 명령을 로그 파일에 기록하여 더 안전한 데이터 복구 가능
	cfg.Append("Appendonly", "yes")                    // AOF 활성화
	cfg.Append("Appendfilename", "\"Appendonly.aof\"") // AOF 파일명
	cfg.Append("dir", dataDir)                         // 데이터 저장 디렉토리 (RDB, AOF 모두 여기에 저장)
}

// applyPort는 포트 설정을 적용합니다.
// TLS 모드: 일반 포트는 비활성화하고 TLS 포트만 사용
// 일반 모드: 기본 포트 사용
func applyPort(cfg *agentutil.Config, tlsMode, redisPort string) {
	if tlsMode == "true" {
		cfg.Append("port", "0")           // 일반 포트 비활성화
		cfg.Append("tls-port", redisPort) // TLS 포트 사용
	} else {
		cfg.Append("port", redisPort) // 일반 포트 사용
	}
	busPort, err := strconv.Atoi(redisPort)
	if err != nil {
		log.Printf("Warning: Failed to convert redis port to int: %v", err)
	}
	cfg.Append("bus-port", strconv.Itoa(busPort+10000))
}

// applyNodePort는 NodePort 모드 설정을 적용합니다.
// NodePort 모드를 사용하면 각 Pod마다 다른 포트가 할당됩니다.
// Pod의 호스트명을 기반으로 환경 변수 이름을 만들어서 해당 Pod의 포트를 가져옵니다.
// 예: Pod 이름이 "redis-cluster-leader-0"이면
//
//	환경 변수: "announce_port_redis_cluster_leader_0"
func applyNodePort(cfg *agentutil.Config, nodeport, tlsMode string) {
	if nodeport != "true" {
		return
	}

	podHostname, _ := os.Hostname() // Pod 호스트명 가져오기 (예: "redis-cluster-leader-0")

	announcePortVar := "announce_port_" + strings.ReplaceAll(podHostname, "-", "_")
	announceBusPortVar := "announce_bus_port_" + strings.ReplaceAll(podHostname, "-", "_")

	// 환경 변수에서 해당 Pod의 포트 정보 가져오기
	clusterAnnouncePort := os.Getenv(announcePortVar)       // 클라이언트 통신 포트
	clusterAnnounceBusPort := os.Getenv(announceBusPortVar) // 클러스터 버스 포트 (노드 간 통신용)

	if clusterAnnouncePort != "" {
		cfg.Append("cluster-announce-port", clusterAnnouncePort) // 클라이언트 통신 포트
		if tlsMode == "true" {
			cfg.Append("cluster-announce-tls-port", clusterAnnouncePort) // TLS 포트
		}
	}
	if clusterAnnounceBusPort != "" {
		cfg.Append("cluster-announce-bus-port", clusterAnnounceBusPort) // 클러스터 버스 포트
	}
}

// applyMemory는 메모리 제한 설정을 적용합니다.
// maxmemory: Redis가 사용할 수 있는 최대 메모리 크기
// 메모리가 가득 차면 eviction 정책에 따라 오래된 데이터를 제거합니다.
func applyMemory(cfg *agentutil.Config) {
	if maxMemory := util.CoalesceEnv1(consts.REDIS_MAX_MEMORY, ""); maxMemory != "" {
		cfg.Append("maxmemory", maxMemory)
	}
}

// applyExternalConfig는 외부 설정 파일 포함 설정을 적용합니다.
// 사용자가 추가로 정의한 설정 파일들을 포함합니다.
// 이 파일들은 ConfigMap이나 Secret으로 마운트할 수 있어요!
// 디렉토리의 모든 .conf 파일을 자동으로 include하며, 파일명 순서대로 처리됩니다.
// 주의: 이 설정은 마지막에 추가되므로, 기본 설정을 덮어쓸 수 있습니다.
func applyExternalConfig(cfg *agentutil.Config, externalConfigDir string) {
	// 디렉토리 존재 여부 확인
	if _, err := os.Stat(externalConfigDir); os.IsNotExist(err) {
		fmt.Printf("External config directory not found: %s\n", externalConfigDir)
		return
	}

	// 디렉토리의 모든 .conf 파일 찾기
	pattern := filepath.Join(externalConfigDir, "*.conf")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("Warning: Failed to list external config files in %s: %v", externalConfigDir, err)
		return
	}

	// 파일이 없으면 메시지 출력
	if len(files) == 0 {
		fmt.Printf("No .conf files found in %s\n", externalConfigDir)
		return
	}

	// 파일명 순서대로 정렬 (01-, 02- 같은 접두사로 순서 제어 가능)
	sort.Strings(files)

	// 모든 .conf 파일 include
	fmt.Printf("Loading external config files from %s:\n", externalConfigDir)
	for _, file := range files {
		fmt.Printf("  - %s\n", filepath.Base(file))
		cfg.Append("include", file)
	}
}

// GenerateConfig는 환경 변수를 읽어서 Redis 설정 파일(redisutils.conf)을 동적으로 생성합니다.
// 이 함수는 Init Container에서 실행되며, Pod가 시작되기 전에 Redis 설정을 준비합니다.
func GenerateConfig() error {
	cfg := agentutil.NewConfig("/etc/redis/redis.conf", defaultRedisConfig)
	var (
		persistenceEnabled = util.CoalesceEnv1(consts.DATA_PERSISTENCE_ENABLED, "false") // 데이터 영속화 활성화 여부
		dataDir            = util.CoalesceEnv1("DATA_DIR", "/data")                      // Redis 데이터 저장 디렉토리
		nodeConfDir        = util.CoalesceEnv1("NODE_CONF_DIR", "/node-conf")            // 클러스터 nodes.conf 파일 위치
		redisMajorVersion  = util.CoalesceEnv1(consts.REDIS_MAJOR_VERSION, "v7")         // Redis 메이저 버전 (v6 또는 v7)
		redisPort          = util.CoalesceEnv1(consts.REDIS_PORT, "6379")                // Redis 포트 번호
		nodeport           = util.CoalesceEnv1("NODEPORT", "false")                      // NodePort 모드 사용 여부 (Kubernetes Service 타입)
		tlsMode            = util.CoalesceEnv1(consts.TLS_MODE, "false")                 // TLS 암호화 활성화 여부
	)

	applyAuth(cfg)
	applyCluster(cfg, nodeport, redisMajorVersion, nodeConfDir)
	applyTLS(cfg, tlsMode, redisMajorVersion, nodeport)
	applyACL(cfg)
	applyPersistence(cfg, persistenceEnabled, dataDir)
	applyPort(cfg, tlsMode, redisPort)
	applyNodePort(cfg, nodeport, tlsMode)
	applyMemory(cfg)
	applyExternalConfig(cfg, "/etc/redis/external.conf.d")

	return cfg.Commit()
}

func updateMyselfIP(nodesConfPath, newIP string) (updated []byte, err error) {
	// nodes.conf 파일 읽기
	raw, err := os.ReadFile(nodesConfPath)
	if err != nil {
		return nil, err
	}

	// IP 주소를 찾기 위한 정규식: IPv4 주소 패턴
	// 예: "192.168.1.1", "10.0.0.1" 등
	ipRe := regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)

	var out bytes.Buffer                              // 출력 버퍼: 업데이트된 내용을 저장
	scanner := bufio.NewScanner(bytes.NewReader(raw)) // 파일 내용을 한 줄씩 읽기
	changed := false                                  // IP 주소가 변경되었는지 여부

	// 파일을 한 줄씩 읽으면서 처리
	for scanner.Scan() {
		line := scanner.Text() // 현재 라인

		// "myself" 라벨이 있는 라인인지 확인
		// nodes.conf 파일 형식 예시:
		//   "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx 192.168.1.1:6379@16379 myself,master ..."
		if bytes.Contains([]byte(line), []byte("myself")) {
			// 이 라인의 모든 IP 주소를 새로운 IP로 교체
			replaced := ipRe.ReplaceAllString(line, newIP)
			if replaced != line {
				changed = true // 변경사항이 있음
				line = replaced
			}
		}

		// 처리된 라인을 출력 버퍼에 추가
		out.WriteString(line)
		out.WriteByte('\n')
	}

	// 스캔 중 에러 확인
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// 변경사항이 있으면 파일에 저장
	if changed {
		updatedBytes := out.Bytes()
		return updatedBytes, os.WriteFile(nodesConfPath, updatedBytes, 0o644)
	}

	// 변경사항이 없으면 nil 반환
	return nil, nil
}
