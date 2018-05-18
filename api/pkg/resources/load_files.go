package resources

import (
	"fmt"
	"path"

	"github.com/golang/glog"
	apiv1 "github.com/kubermatic/kubermatic/api/pkg/api/v1"
	etcdoperatorv1beta2 "github.com/kubermatic/kubermatic/api/pkg/crd/etcdoperator/v1beta2"
	kubermaticv1 "github.com/kubermatic/kubermatic/api/pkg/crd/kubermatic/v1"
	"github.com/kubermatic/kubermatic/api/pkg/provider"
	k8stemplate "github.com/kubermatic/kubermatic/api/pkg/template/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
)

const (
	// EtcdClusterName is the name of the etcd cluster
	EtcdClusterName = "etcd-cluster"

	//AddonManagerDeploymentName is the name for the addon-manager deployment
	AddonManagerDeploymentName = "addon-manager"
	//ApiserverDeploymenName is the name for the apiserver deployment
	ApiserverDeploymenName = "apiserver"
	//ControllerManagerDeploymentName is the name for the controller manager deployment
	ControllerManagerDeploymentName = "controller-manager"
	//EtcdOperatorDeploymentName is the name for the etcd-operator deployment
	EtcdOperatorDeploymentName = "etcd-operator"
	//SchedulerDeploymentName is the name for the scheduler deployment
	SchedulerDeploymentName = "scheduler"
	//MachineControllerDeploymentName is the name for the machine-controller deployment
	MachineControllerDeploymentName = "machine-controller"
	//OpenVPNServerDeploymentName is the name for the openvpn server deployment
	OpenVPNServerDeploymentName = "openvpn-server"

	//PrometheusStatefulSetName is the name for the prometheus StatefulSet
	PrometheusStatefulSetName = "prometheus"

	//ApiserverExternalServiceName is the name for the external apiserver service
	ApiserverExternalServiceName = "apiserver-external"
	//ApiserverInternalServiceName is the name for the internal apiserver service
	ApiserverInternalServiceName = "apiserver"
	//PrometheusServiceName is the name for the prometheus service
	PrometheusServiceName = "prometheus"
	//OpenVPNServerServiceName is the name for the openvpn server service
	OpenVPNServerServiceName = "openvpn-server"

	//AdminKubeconfigSecretName is the name for the secret containing the private ca key
	AdminKubeconfigSecretName = "admin-kubeconfig"
	//CAKeySecretName is the name for the secret containing the private ca key
	CAKeySecretName = "ca-key"
	//CACertSecretName is the name for the secret containing the ca.crt
	CACertSecretName = "ca-cert"
	//ApiserverTLSSecretName is the name for the secrets required for the apiserver tls
	ApiserverTLSSecretName = "apiserver-tls"
	//KubeletClientCertificatesSecretName is the name for the secret containing the kubelet client certificates
	KubeletClientCertificatesSecretName = "kubelet-client-certificates"
	//ServiceAccountKeySecretName is the name for the secret containing the service account key
	ServiceAccountKeySecretName = "service-account-key"
	//TokensSecretName is the name for the secret containing the user tokens
	TokensSecretName = "tokens"
	//OpenVPNServerCertificatesSecretName is the name for the secret containing the openvpn server certificates
	OpenVPNServerCertificatesSecretName = "openvpn-server-certificates"
	//OpenVPNClientCertificatesSecretName is the name for the secret containing the openvpn client certificates
	OpenVPNClientCertificatesSecretName = "openvpn-client-certificates"

	//CloudConfigConfigMapName is the name for the configmap containing the cloud-config
	CloudConfigConfigMapName = "cloud-config"
	//OpenVPNClientConfigConfigMapName is the name for the configmap containing the openvpn client config used within the user cluster
	OpenVPNClientConfigConfigMapName = "openvpn-client-configs"
	//PrometheusConfigConfigMapName is the name for the configmap containing the prometheus config
	PrometheusConfigConfigMapName = "prometheus"

	//EtcdOperatorServiceAccountName is the name for the etcd-operator serviceaccount
	EtcdOperatorServiceAccountName = "etcd-operator"
	//PrometheusServiceAccountName is the name for the Prometheus serviceaccount
	PrometheusServiceAccountName = "prometheus"

	//PrometheusRoleName is the name for the Prometheus role
	PrometheusRoleName = "prometheus"

	//PrometheusRoleBindingName is the name for the Prometheus rolebinding
	PrometheusRoleBindingName = "prometheus"

	//EtcdOperatorClusterRoleBindingName is the name for the etcd-operator clusterrolebinding
	EtcdOperatorClusterRoleBindingName = "etcd-operator"

	// DefaultOwnerReadOnlyMode represents file mode 0400 in decimal
	DefaultOwnerReadOnlyMode = 256

	// AppLabelKey defines the label key app which should be used within resources
	AppLabelKey = "app"
)

const (
	// CAKeySecretKey ca.key
	CAKeySecretKey = "ca.key"
	// CACertSecretKey ca.crt
	CACertSecretKey = "ca.crt"
	// ApiserverTLSKeySecretKey apiserver-tls.key
	ApiserverTLSKeySecretKey = "apiserver-tls.key"
	// ApiserverTLSCertSecretKey apiserver-tls.crt
	ApiserverTLSCertSecretKey = "apiserver-tls.crt"
	// KubeletClientKeySecretKey kubelet-client.key
	KubeletClientKeySecretKey = "kubelet-client.key"
	// KubeletClientCertSecretKey kubelet-client.crt
	KubeletClientCertSecretKey = "kubelet-client.crt"
	// ServiceAccountKeySecretKey sa.key
	ServiceAccountKeySecretKey = "sa.key"
	// AdminKubeconfigSecretKey admin-kubeconfig
	AdminKubeconfigSecretKey = "admin-kubeconfig"
	// TokensSecretKey tokens.csv
	TokensSecretKey = "tokens.csv"
	// OpenVPNServerKeySecretKey server.key
	OpenVPNServerKeySecretKey = "server.key"
	// OpenVPNServerCertSecretKey server.crt
	OpenVPNServerCertSecretKey = "server.crt"
	// OpenVPNInternalClientKeySecretKey client.key
	OpenVPNInternalClientKeySecretKey = "client.key"
	// OpenVPNInternalClientCertSecretKey client.crt
	OpenVPNInternalClientCertSecretKey = "client.crt"
)

// TemplateData is a group of data required for template generation
type TemplateData struct {
	Cluster           *kubermaticv1.Cluster
	Version           *apiv1.MasterVersion
	DC                *provider.DatacenterMeta
	SecretLister      corev1lister.SecretLister
	ConfigMapLister   corev1lister.ConfigMapLister
	ServiceLister     corev1lister.ServiceLister
	OverwriteRegistry string
	NodePortRange     string
}

// GetClusterRef returns a instance of a OwnerReference for the Cluster in the TemplateData
func (d *TemplateData) GetClusterRef() metav1.OwnerReference {
	gv := kubermaticv1.SchemeGroupVersion
	return *metav1.NewControllerRef(d.Cluster, gv.WithKind("Cluster"))
}

// Int32 returns a pointer to of the int32 value passed in.
func Int32(v int32) *int32 {
	return &v
}

// Int64 returns a pointer to of the int64 value passed in.
func Int64(v int64) *int64 {
	return &v
}

// Bool returns a pointer to of the bool value passed in.
func Bool(v bool) *bool {
	return &v
}

// NewTemplateData returns an instance of TemplateData
func NewTemplateData(
	cluster *kubermaticv1.Cluster,
	version *apiv1.MasterVersion,
	dc *provider.DatacenterMeta,
	secretLister corev1lister.SecretLister,
	configMapLister corev1lister.ConfigMapLister,
	serviceLister corev1lister.ServiceLister,
	overwriteRegistry string,
	nodePortRange string) *TemplateData {
	return &TemplateData{
		Cluster:           cluster,
		DC:                dc,
		Version:           version,
		ConfigMapLister:   configMapLister,
		SecretLister:      secretLister,
		ServiceLister:     serviceLister,
		OverwriteRegistry: overwriteRegistry,
		NodePortRange:     nodePortRange,
	}
}

// SecretRevision returns the resource version of the secret specified by name. A empty string will be returned in case of an error
func (d *TemplateData) SecretRevision(name string) (string, error) {
	secret, err := d.SecretLister.Secrets(d.Cluster.Status.NamespaceName).Get(name)
	if err != nil {
		return "", fmt.Errorf("could not get secret %s from lister for cluster %s: %v", name, d.Cluster.Name, err)
	}
	return secret.ResourceVersion, nil
}

// ConfigMapRevision returns the resource version of the configmap specified by name. A empty string will be returned in case of an error
func (d *TemplateData) ConfigMapRevision(name string) (string, error) {
	cm, err := d.ConfigMapLister.ConfigMaps(d.Cluster.Status.NamespaceName).Get(name)
	if err != nil {
		return "", fmt.Errorf("could not get configmap %s from lister for cluster %s: %v", name, d.Cluster.Name, err)
	}
	return cm.ResourceVersion, nil
}

// ProviderName returns the name of the clusters providerName
func (d *TemplateData) ProviderName() string {
	p, err := provider.ClusterCloudProviderName(d.Cluster.Spec.Cloud)
	if err != nil {
		glog.V(0).Infof("could not identify cloud provider: %v", err)
	}
	return p
}

// GetApiserverExternalNodePort returns the nodeport of the external apiserver service
func (d *TemplateData) GetApiserverExternalNodePort() (int32, error) {
	s, err := d.ServiceLister.Services(d.Cluster.Status.NamespaceName).Get(ApiserverExternalServiceName)
	if err != nil {

		return 0, fmt.Errorf("failed to get NodePort for external apiserver service: %v", err)

	}
	return s.Spec.Ports[0].NodePort, nil
}

// ImageRegistry returns the image registry to use or the passed in default if no override is specified
func (d *TemplateData) ImageRegistry(defaultRegistry string) string {
	if d.OverwriteRegistry != "" {
		return d.OverwriteRegistry
	}
	return defaultRegistry
}

// LoadEtcdClusterFile loads a etcd-operator crd from disk and returns a Cluster crd struct
func LoadEtcdClusterFile(data *TemplateData, masterResourcesPath, yamlFile string) (*etcdoperatorv1beta2.EtcdCluster, string, error) {
	t, err := k8stemplate.ParseFile(path.Join(masterResourcesPath, yamlFile))
	if err != nil {
		return nil, "", err
	}

	var c etcdoperatorv1beta2.EtcdCluster
	json, err := t.Execute(data, &c)
	return &c, json, err
}

// GetLabels returns default labels every resource should have
func GetLabels(app string) map[string]string {
	return map[string]string{AppLabelKey: app}
}
