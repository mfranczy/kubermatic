package openstack

import (
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	goopenstack "github.com/gophercloud/gophercloud/openstack"
	osflavors "github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	osprojects "github.com/gophercloud/gophercloud/openstack/identity/v3/projects"
	ossecuritygroups "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	ossubnets "github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	kubermaticv1 "github.com/kubermatic/kubermatic/api/pkg/crd/kubermatic/v1"
	"github.com/kubermatic/kubermatic/api/pkg/kubernetes"
	"github.com/kubermatic/kubermatic/api/pkg/provider"
)

const (
	securityGroupCleanupFinalizer = "kubermatic.io/cleanup-openstack-security-group"
	// Deprecated: Got splitted into dedicated finalizers
	oldNetworkCleanupFinalizer = "kubermatic.io/cleanup-openstack-network"

	networkCleanupFinalizer          = "kubermatic.io/cleanup-openstack-network-v2"
	subnetCleanupFinalizer           = "kubermatic.io/cleanup-openstack-subnet-v2"
	routerCleanupFinalizer           = "kubermatic.io/cleanup-openstack-router-v2"
	routerSubnetLinkCleanupFinalizer = "kubermatic.io/cleanup-openstack-router-subnet-link-v2"
)

// Provider is a struct that implements CloudProvider interface
type Provider struct {
	dcs map[string]provider.DatacenterMeta
}

// NewCloudProvider creates a new openstack provider.
func NewCloudProvider(dcs map[string]provider.DatacenterMeta) provider.CloudProvider {
	return &Provider{
		dcs: dcs,
	}
}

// DefaultCloudSpec adds defaults to the cloud spec
func (os *Provider) DefaultCloudSpec(spec kubermaticv1.CloudSpec) error {
	return nil
}

// ValidateCloudSpec validates the given CloudSpec
func (os *Provider) ValidateCloudSpec(spec kubermaticv1.CloudSpec) error {
	netClient, err := os.getNetClient(spec)
	if err != nil {
		return fmt.Errorf("failed to create a authenticated openstack client: %v", err)
	}

	if spec.Openstack.SecurityGroups != "" {
		if err := validateSecurityGroupsExist(netClient, strings.Split(spec.Openstack.SecurityGroups, ",")); err != nil {
			return err
		}
	}

	if spec.Openstack.Network != "" {
		_, err := getNetworkByName(netClient, spec.Openstack.Network, false)
		if err != nil {
			return fmt.Errorf("failed to get network %q: %v", spec.Openstack.Network, err)
		}
	}

	if spec.Openstack.FloatingIPPool != "" {
		_, err := getNetworkByName(netClient, spec.Openstack.FloatingIPPool, true)
		if err != nil {
			return fmt.Errorf("failed to get floating ip pool %q: %v", spec.Openstack.FloatingIPPool, err)
		}
	}

	return nil
}

// InitializeCloudProvider initializes a cluster, in particular
// creates security group and network configuration
func (os *Provider) InitializeCloudProvider(cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	dc, found := os.dcs[cluster.Spec.Cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, fmt.Errorf("invalid datacenter %q", cluster.Spec.Cloud.DatacenterName)
	}

	netClient, err := os.getNetClient(cluster.Spec.Cloud)
	if err != nil {
		return nil, fmt.Errorf("failed to create a authenticated openstack client: %v", err)
	}

	if cluster.Spec.Cloud.Openstack.FloatingIPPool == "" {
		extNetwork, err := getExternalNetwork(netClient)
		if err != nil {
			return nil, err
		}
		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Spec.Cloud.Openstack.FloatingIPPool = extNetwork.Name
			// We're just searching for the floating ip pool here & don't create anything. Thus no need to create a finalizer
		})
		if err != nil {
			return nil, err
		}
	}

	if cluster.Spec.Cloud.Openstack.SecurityGroups == "" {
		g, err := createKubermaticSecurityGroup(netClient, cluster.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create the kubermatic security group: %v", err)
		}
		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Spec.Cloud.Openstack.SecurityGroups = g.Name
			cluster.Finalizers = kubernetes.AddFinalizer(cluster.Finalizers, securityGroupCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if cluster.Spec.Cloud.Openstack.Network == "" {
		network, err := createKubermaticNetwork(netClient, cluster.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create the kubermatic network: %v", err)
		}
		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Spec.Cloud.Openstack.Network = network.Name
			cluster.Finalizers = kubernetes.AddFinalizer(cluster.Finalizers, networkCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	network, err := getNetworkByName(netClient, cluster.Spec.Cloud.Openstack.Network, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get network '%s': %v", cluster.Spec.Cloud.Openstack.Network, err)
	}

	if cluster.Spec.Cloud.Openstack.SubnetID == "" {
		subnet, err := createKubermaticSubnet(netClient, cluster.Name, network.ID, dc.Spec.Openstack.DNSServers)
		if err != nil {
			return nil, fmt.Errorf("failed to create the kubermatic subnet: %v", err)
		}

		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Spec.Cloud.Openstack.SubnetID = subnet.ID
			cluster.Finalizers = kubernetes.AddFinalizer(cluster.Finalizers, subnetCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if cluster.Spec.Cloud.Openstack.RouterID == "" {
		// Check if the subnet has already a router
		routerID, err := getRouterIDForSubnet(netClient, cluster.Spec.Cloud.Openstack.SubnetID, network.ID)
		if err != nil {
			if err == errNotFound {
				// No Router exists -> Create a router
				router, err := createKubermaticRouter(netClient, cluster.Name, cluster.Spec.Cloud.Openstack.FloatingIPPool)
				if err != nil {
					return nil, fmt.Errorf("failed to create the kubermatic router: %v", err)
				}
				cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
					cluster.Spec.Cloud.Openstack.RouterID = router.ID
					cluster.Finalizers = kubernetes.AddFinalizer(cluster.Finalizers, routerCleanupFinalizer)
				})
				if err != nil {
					return nil, err
				}
			} else {
				return nil, fmt.Errorf("failed to verify that the subnet '%s' has a router attached: %v", cluster.Spec.Cloud.Openstack.SubnetID, err)
			}
		} else {
			// A router already exists -> Reuse it but don't clean it up
			cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
				cluster.Spec.Cloud.Openstack.RouterID = routerID
			})
			if err != nil {
				return nil, err
			}
		}
	}

	// If we created the subnet, but have not created the router-subnet-link finalizer, we need to attach the subnet to the router
	// Otherwise the vm's won't have connectivity
	if kubernetes.HasFinalizer(cluster, subnetCleanupFinalizer) && !kubernetes.HasFinalizer(cluster, routerSubnetLinkCleanupFinalizer) {
		if _, err = attachSubnetToRouter(netClient, cluster.Spec.Cloud.Openstack.SubnetID, cluster.Spec.Cloud.Openstack.RouterID); err != nil {
			return nil, fmt.Errorf("failed to attach subnet to router: %v", err)
		}

		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.AddFinalizer(cluster.Finalizers, routerSubnetLinkCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	return cluster, nil
}

// CleanUpCloudProvider does the clean-up in particular:
// removes security group and network configuration
func (os *Provider) CleanUpCloudProvider(cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	netClient, err := os.getNetClient(cluster.Spec.Cloud)
	if err != nil {
		return nil, fmt.Errorf("failed to create a authenticated openstack client: %v", err)
	}

	if kubernetes.HasFinalizer(cluster, securityGroupCleanupFinalizer) {
		for _, g := range strings.Split(cluster.Spec.Cloud.Openstack.SecurityGroups, ",") {
			if err := deleteSecurityGroup(netClient, strings.TrimSpace(g)); err != nil {
				return nil, fmt.Errorf("failed to delete security group %q: %v", g, err)
			}
		}

		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.RemoveFinalizer(cluster.Finalizers, securityGroupCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if kubernetes.HasFinalizer(cluster, routerSubnetLinkCleanupFinalizer) || kubernetes.HasFinalizer(cluster, oldNetworkCleanupFinalizer) {
		if _, err = detachSubnetFromRouter(netClient, cluster.Spec.Cloud.Openstack.SubnetID, cluster.Spec.Cloud.Openstack.RouterID); err != nil {
			if !isNotFoundErr(err) {
				return nil, fmt.Errorf("failed to detach subnet from router: %v", err)
			}
		}
		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.RemoveFinalizer(cluster.Finalizers, routerSubnetLinkCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if kubernetes.HasFinalizer(cluster, subnetCleanupFinalizer) || kubernetes.HasFinalizer(cluster, oldNetworkCleanupFinalizer) {
		if err := deleteSubnet(netClient, cluster.Spec.Cloud.Openstack.SubnetID); err != nil {
			return nil, fmt.Errorf("failed to delete subnet '%s': %v", cluster.Spec.Cloud.Openstack.SubnetID, err)
		}
		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.RemoveFinalizer(cluster.Finalizers, subnetCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if kubernetes.HasFinalizer(cluster, networkCleanupFinalizer) || kubernetes.HasFinalizer(cluster, oldNetworkCleanupFinalizer) {
		if err = deleteNetworkByName(netClient, cluster.Spec.Cloud.Openstack.Network); err != nil {
			if !isNotFoundErr(err) {
				return nil, fmt.Errorf("failed to delete network '%s': %v", cluster.Spec.Cloud.Openstack.Network, err)
			}
		}

		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.RemoveFinalizer(cluster.Finalizers, networkCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if kubernetes.HasFinalizer(cluster, routerCleanupFinalizer) || kubernetes.HasFinalizer(cluster, oldNetworkCleanupFinalizer) {
		if err = deleteRouter(netClient, cluster.Spec.Cloud.Openstack.RouterID); err != nil {
			if !isNotFoundErr(err) {
				return nil, fmt.Errorf("failed to delete router '%s': %v", cluster.Spec.Cloud.Openstack.RouterID, err)
			}
		}

		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.RemoveFinalizer(cluster.Finalizers, routerCleanupFinalizer)
		})
		if err != nil {
			return nil, err
		}
	}

	if kubernetes.HasFinalizer(cluster, oldNetworkCleanupFinalizer) {
		cluster, err = update(cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Finalizers = kubernetes.RemoveFinalizer(cluster.Finalizers, oldNetworkCleanupFinalizer)
		})
	}

	return cluster, nil
}

// GetFlavors lists available flavors for the given CloudSpec.DatacenterName and OpenstackSpec.Region
func (os *Provider) GetFlavors(cloud kubermaticv1.CloudSpec) ([]osflavors.Flavor, provider.DatacenterMeta, error) {
	authClient, err := os.getAuthClient(cloud)
	if err != nil {
		return nil, provider.DatacenterMeta{}, err
	}
	dc, found := os.dcs[cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, provider.DatacenterMeta{}, fmt.Errorf("invalid datacenter %q", cloud.DatacenterName)
	}
	flavors, err := getFlavors(authClient, dc.Spec.Openstack.Region)
	if err != nil {
		return nil, provider.DatacenterMeta{}, err
	}

	return flavors, dc, nil
}

// GetTenants lists all available tenents for the given CloudSpec.DatacenterName
func (os *Provider) GetTenants(cloud kubermaticv1.CloudSpec) ([]osprojects.Project, error) {
	authClient, err := os.getAuthClient(cloud)
	if err != nil {
		return nil, fmt.Errorf("couldn't get auth client: %v", err)
	}

	dc, found := os.dcs[cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, fmt.Errorf("invalid datacenter %q", cloud.DatacenterName)
	}

	region := dc.Spec.Openstack.Region
	tenants, err := getTenants(authClient, region)
	if err != nil {
		return nil, fmt.Errorf("couldn't get tenants for region %s: %v", region, err)
	}

	return tenants, nil
}

// GetNetworks lists all available networks for the given CloudSpec.DatacenterName
func (os *Provider) GetNetworks(cloud kubermaticv1.CloudSpec) ([]NetworkWithExternalExt, error) {
	authClient, err := os.getNetClient(cloud)
	if err != nil {
		return nil, fmt.Errorf("couldn't get auth client: %v", err)
	}

	dc, found := os.dcs[cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, fmt.Errorf("invalid datacenter %q", cloud.DatacenterName)
	}

	networks, err := getAllNetworks(authClient)
	if err != nil {
		return nil, fmt.Errorf("couldn't get networks: %v", err)
	}

	return networks, nil
}

// GetSecurityGroups lists all available security groups for the given CloudSpec.DatacenterName
func (os *Provider) GetSecurityGroups(cloud kubermaticv1.CloudSpec) ([]ossecuritygroups.SecGroup, error) {
	authClient, err := os.getNetClient(cloud)
	if err != nil {
		return nil, fmt.Errorf("couldn't get auth client: %v", err)
	}

	dc, found := os.dcs[cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, fmt.Errorf("invalid datacenter %q", cloud.DatacenterName)
	}

	securityGroups, err := getAllSecurityGroups(authClient)
	if err != nil {
		return nil, fmt.Errorf("couldn't get securityGroups: %v", err)
	}

	return securityGroups, nil
}

func (os *Provider) getAuthClient(cloud kubermaticv1.CloudSpec) (*gophercloud.ProviderClient, error) {
	dc, found := os.dcs[cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, fmt.Errorf("invalid datacenter %q", cloud.DatacenterName)
	}

	opts := gophercloud.AuthOptions{
		IdentityEndpoint: dc.Spec.Openstack.AuthURL,
		Username:         cloud.Openstack.Username,
		Password:         cloud.Openstack.Password,
		DomainName:       cloud.Openstack.Domain,
		TenantName:       cloud.Openstack.Tenant,
	}

	client, err := goopenstack.AuthenticatedClient(opts)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (os *Provider) getNetClient(cloud kubermaticv1.CloudSpec) (*gophercloud.ServiceClient, error) {
	authClient, err := os.getAuthClient(cloud)
	if err != nil {
		return nil, err
	}

	dc, found := os.dcs[cloud.DatacenterName]
	if !found || dc.Spec.Openstack == nil {
		return nil, fmt.Errorf("invalid datacenter %q", cloud.DatacenterName)
	}

	return goopenstack.NewNetworkV2(authClient, gophercloud.EndpointOpts{Region: dc.Spec.Openstack.Region})
}

// GetSubnets list all available subnet ids fot a given CloudSpec
func (os *Provider) GetSubnets(cloud kubermaticv1.CloudSpec, networkID string) ([]ossubnets.Subnet, error) {
	serviceClient, err := os.getNetClient(cloud)
	if err != nil {
		return nil, fmt.Errorf("couldn't get auth client: %v", err)
	}

	subnets, err := getSubnetForNetwork(serviceClient, networkID)
	if err != nil {
		return nil, err
	}

	return subnets, nil
}
