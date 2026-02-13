package manager

import (
	"flag"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	redisclustercontroller "github.com/xcdev-0/redis-operator/internal/controller"
	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	"github.com/xcdev-0/redis-operator/internal/scheme"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var setupLog = ctrl.Log.WithName("setup")

type managerOptions struct {
	metricsAddr             string
	probeAddr               string
	pprofAddr               string
	enableLeaderElection    bool
	enableWebhooks          bool
	maxConcurrentReconciles int
	zapOptions              zap.Options
}

const (
	KubeClientTimeoutMGRFlag = "kube-client-timeout"
	KubeClientQPSMGRFlag     = "kube-client-qps"
)

func CMD() *cobra.Command {
	opts := &managerOptions{
		zapOptions: zap.Options{
			Development: true,
		},
	}

	// Cobra 명령어 객체를 생성합니다.
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Start the Redis operator manager",

		PreRunE: func(cmd *cobra.Command, args []string) error {
			return viper.BindPFlags(cmd.Flags())
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			return runManager(opts)
		},
	}

	addFlags(cmd, opts)

	return cmd
}

func addFlags(cmd *cobra.Command, opts *managerOptions) {
	// 메트릭 서버 바인딩 주소
	cmd.Flags().StringVar(&opts.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")

	// Health Check 바인딩 주소
	cmd.Flags().StringVar(&opts.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	// pprof 프로파일링 바인딩 주소
	cmd.Flags().StringVar(&opts.pprofAddr, "pprof-bind-address", "", "The address the pprof endpoint binds to. If empty, pprof is disabled. Example: ':6060'")

	// Leader Election 활성화
	// 여러 Operator Pod가 실행될 때, 하나만 활성화되도록 보장합니다.
	// 활성화하면 Kubernetes의 Lease 리소스를 사용하여 Leader를 선출합니다.
	// 기본값: false (단일 Pod 환경에서는 불필요)
	cmd.Flags().BoolVar(&opts.enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	// Webhook 활성화
	cmd.Flags().BoolVar(&opts.enableWebhooks, "enable-webhooks", envs.IsWebhookEnabled(), "Enable webhooks")

	// 사용자가 입력한 플래그 값을 우리가 미리 만든 변수 주소(&opts...)에 직접 배달해 줌
	cmd.Flags().IntVar(&opts.maxConcurrentReconciles, "max-concurrent-reconciles", 1, "Max concurrent reconciles")

	// 변수 주소를 받지 않음. 대신 코브라가 내부적으로 메모리 어딘가에 값을 저장해 둡니다.
	cmd.Flags().Duration(
		KubeClientTimeoutMGRFlag,
		60*time.Second,
		"Timeout for requests made by the Kubernetes API client.",
	)

	cmd.Flags().Float32(
		KubeClientQPSMGRFlag,
		0,
		"Maximum number of queries per second to the Kubernetes API.",
	)

	zapFlagSet := flag.NewFlagSet("zap", flag.ExitOnError)
	opts.zapOptions.BindFlags(zapFlagSet)
	zapFlagSet.VisitAll(func(f *flag.Flag) {
		cmd.Flags().AddGoFlag(f)
	})
}

func runManager(opts *managerOptions) error {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts.zapOptions)))

	// monitoring.RegisterRedisReplicationMetrics()
	// monitoring.RegisterRedisClusterMetrics()

	setupLog.Info("setting up v1beta2 scheme")
	scheme.SetupV1beta2Scheme()

	// cfg, ctrl options 생성
	cfg := ctrl.GetConfigOrDie()
	if qps := float32(viper.GetFloat64(KubeClientQPSMGRFlag)); qps > 0 {
		cfg.QPS = qps
		cfg.Burst = int(qps * 2)
	}
	cfg.Timeout = viper.GetDuration(KubeClientTimeoutMGRFlag)
	ctrlOptions := createControllerOptions(opts)

	mgr, err := ctrl.NewManager(cfg, ctrlOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	k8sClient, err := createK8sClient()
	if err != nil {
		return err
	}

	if err := setupRedisClusterController(mgr, k8sClient, opts.maxConcurrentReconciles); err != nil {
		return err
	}

	// if opts.enableWebhooks {
	// 	if err := setupWebhooks(mgr); err != nil {
	// 		return err
	// 	}
	// }

	if err := setupHealthChecks(mgr); err != nil {
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	return nil
}

// 유저가 입력한 날것의 데이터(opts)를 라이브러리가 알아들을 수 있는
// 표준 규격(ctrl.Options)으로 번역해서 채워주는 과정
func createControllerOptions(opts *managerOptions) ctrl.Options {
	options := ctrl.Options{
		Metrics: metricsserver.Options{
			BindAddress: opts.metricsAddr,
		},

		WebhookServer: &webhook.DefaultServer{
			Options: webhook.Options{
				Port: 9443,
			},
		},

		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.enableLeaderElection,
		LeaderElectionID:       "770c900a.ejlabs.in",
	}

	if opts.pprofAddr != "" {
		options.PprofBindAddress = opts.pprofAddr
	}

	return options
}

func createK8sClient() (kubernetes.Interface, error) {
	// Kubernetes 설정을 생성합니다.
	// ~/.kube/config 또는 ServiceAccount 토큰에서 설정을 로드합니다.
	k8sConfig := k8smeta.GenerateK8sConfig()

	// Kubernetes 클라이언트를 생성합니다.
	k8sClient, err := k8smeta.GenerateK8sClient(k8sConfig)
	if err != nil {
		setupLog.Error(err, "unable to create k8s client")
		return nil, err
	}

	return k8sClient, nil
}

// 컨트롤러 (Controller): WithManager를 통해 만들어진 실행 단위
// 리컨사일러 (Reconciler): 컨트롤러가 실제 업무를 수행할 때 참고하는 "업무 매뉴얼(함수)"
func setupRedisClusterController(mgr ctrl.Manager, k8sClient kubernetes.Interface, maxConcurrentReconciles int) error {
	// 환경 변수에서 최대 동시 Reconcile 수를 가져옵니다.
	// 환경 변수가 설정되어 있으면 그 값을 사용하고, 없으면 플래그 값을 사용합니다.
	maxConcurrentReconciles = envs.GetMaxConcurrentReconciles(maxConcurrentReconciles)

	if err := (&redisclustercontroller.RedisClusterReconciler{
		Client:             mgr.GetClient(),                               // CRD 읽기/쓰기용
		StatefulSetService: statefulset.NewStatefulSetService(k8sClient),  // StatefulSet 유틸리티
		K8sClient:          k8sClient,                                     // Pod 실행, 명령 실행용
		Recorder:           mgr.GetEventRecorderFor("cluster-controller"), // Kubernetes Event 기록용
	}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RedisCluster")
		return err
	}

	return nil
}

// setupWebhooks는 모든 Webhook을 Manager에 등록합니다.
// Webhook은 Kubernetes Admission Controller의 일부로, 리소스 생성/수정 시 검증 및 변형을 수행합니다.
// func setupWebhooks(mgr ctrl.Manager) error {
// 	if err := (&rcvb2.RedisCluster{}).SetupWebhookWithManager(mgr); err != nil {
// 		setupLog.Error(err, "unable to create webhook", "webhook", "RedisCluster")
// 		return err
// 	}

// 	wblog := ctrl.Log.WithName("webhook").WithName("PodAffiniytMutate")
// 	mgr.GetWebhookServer().Register("/mutate-core-v1-pod", &webhook.Admission{
// 		Handler: coreWebhook.NewPodAffiniytMutate(mgr.GetClient(), admission.NewDecoder(scheme.Scheme), wblog),
// 	})

// 	return nil
// }

func setupHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	return nil
}
