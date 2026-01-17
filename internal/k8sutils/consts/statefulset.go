package consts

const (
	EnvOperatorSTSPVCTemplateName = "OPERATOR_STS_PVC_TEMPLATE_NAME"
)

const (
	AnnotationKeyRecreateStatefulset         = "redis.ejlabs.in/recreate-statefulset"
	AnnotationKeyRecreateStatefulsetStrategy = "redis.ejlabs.in/recreate-statefulset-strategy"
)

const (
	LabelKeyCurrentRole = "redis-current-role"
	LabelValueMaster    = "master"
	LabelValueSlave     = "slave"
)

// StatefulSet이 Pod를 식별하는 데 사용하는 안정적인 라벨 키
const (
	LabelKeyApp     = "app"
	LabelKeyRole    = "role"
	LabelKeyCluster = "cluster"
)

const (
	ConfigVolumeName         = "config"
	ExternalConfigVolumeName = "external-config"
	NodeConfVolumeName       = "node-conf"
	TLSCertsVolumeName       = "tls-certs"
	ACLSecretVolumeName      = "acl-secret"
	ACLPVCVolumeName         = "acl-pvc"
)
