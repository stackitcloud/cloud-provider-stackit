package main

import (
	"flag"
	"os"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/alb/ingress"
	albclient "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	albsdk "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certsdk "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo,funlen // TODO: Refactor into smaller functions.
func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var leaderElectionNamespace string
	var leaderElectionID string
	var probeAddr string
	var cloudConfig string
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "default", "The namespace in which the leader "+
		"election resource will be created.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "d0fbe9c4.stackit.cloud", "The name of the resource that "+
		"leader election will use for holding the leader lock.")
	flag.StringVar(&cloudConfig, "cloud-config", "cloud.yaml", "The path to the cloud config file.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	config, err := stackitconfig.ReadALBConfigFromFile(cloudConfig)
	if err != nil {
		setupLog.Error(err, "Failed to read cloud config")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        leaderElectionID,
		LeaderElectionNamespace: leaderElectionNamespace,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	albOpts := []sdkconfig.ConfigurationOption{}
	if config.Global.APIEndpoints.ApplicationLoadBalancerAPI != "" {
		albOpts = append(albOpts, sdkconfig.WithEndpoint(config.Global.APIEndpoints.ApplicationLoadBalancerAPI))
	}

	certOpts := []sdkconfig.ConfigurationOption{}
	if config.Global.APIEndpoints.ApplicationLoadBalancerCertificateAPI != "" {
		certOpts = append(certOpts, sdkconfig.WithEndpoint(config.Global.APIEndpoints.ApplicationLoadBalancerCertificateAPI))
	}

	// Setup ALB API client
	sdkClient, err := albsdk.NewAPIClient(albOpts...)
	if err != nil {
		setupLog.Error(err, "unable to create ALB SDK client", "controller", "IngressClass")
		os.Exit(1)
	}
	albClient, err := albclient.NewApplicationLoadBalancerClient(sdkClient)
	if err != nil {
		setupLog.Error(err, "unable to create ALB client", "controller", "IngressClass")
		os.Exit(1)
	}

	// Setup Certificates API client
	certificateAPI, err := certsdk.NewAPIClient(certOpts...)
	if err != nil {
		setupLog.Error(err, "unable to create certificate SDK client", "controller", "IngressClass")
		os.Exit(1)
	}
	certificateClient, err := albclient.NewCertClient(certificateAPI)
	if err != nil {
		setupLog.Error(err, "unable to create Certificates client", "controller", "IngressClass")
		os.Exit(1)
	}

	if err = (&ingress.IngressClassReconciler{
		Client:            mgr.GetClient(),
		ALBClient:         albClient,
		CertificateClient: certificateClient,
		Scheme:            mgr.GetScheme(),
		ALBConfig:         config,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IngressClass")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
