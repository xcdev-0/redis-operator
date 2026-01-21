package consts

const (
	EnvOperatorSTSPVCTemplateName = "OPERATOR_STS_PVC_TEMPLATE_NAME"
)

const (
	AnnotationKeyRecreateStatefulset         = "redis.ejlabs.in/recreate-statefulset"
	AnnotationKeyRecreateStatefulsetStrategy = "redis.ejlabs.in/recreate-statefulset-strategy"
	AnnotationKeyStorageCapacity             = "storageCapacity"
)

const (
	LabelKeyCurrentRole = "redis-current-role"
	LabelValueMaster    = "master"
	LabelValueSlave     = "slave"
)

// StatefulSet이 Pod를 식별하는 데 사용하는 안정적인 라벨 키
const (
	LabelKeyApp     = "sts-name"
	LabelKeyRole    = "role"
	LabelKeyCluster = "cluster"
)

const (
	MainContainerName = "redis"
)

const (
	VolumeNameConfig         = "config"
	VolumeNameExternalConfig = "external-config"
	VolumeNameNodeConf       = "node-conf"
	VolumeNameData           = "data-persistence" // 데이터 영속성 볼륨 이름
	VolumeNameTLSCerts       = "tls-certs"
	VolumeNameACLSecret      = "acl-secret"
	VolumeNameACLPVC         = "acl-pvc"
)
