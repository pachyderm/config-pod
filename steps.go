package main

import (
	"strings"

	"github.com/pachyderm/pachyderm/v2/src/auth"
	"github.com/pachyderm/pachyderm/v2/src/client"
	"github.com/pachyderm/pachyderm/v2/src/enterprise"
	"github.com/pachyderm/pachyderm/v2/src/identity"
	"github.com/pachyderm/pachyderm/v2/src/license"
	"github.com/pachyderm/pachyderm/v2/src/pfs"
	"github.com/pachyderm/pachyderm/v2/src/pps"

	"github.com/gogo/protobuf/proto"
)

const (
	localhostEnterpriseClusterId = "localhost"
)

func localhostEnterpriseCluster(secret string) license.AddClusterRequest {
	return license.AddClusterRequest{
		Id:               localhostEnterpriseClusterId,
		Address:          "grpc://localhost:1653",
		UserAddress:      "grpc://localhost:1653",
		Secret:           secret,
		EnterpriseServer: true,
	}
}

func localhostEnterpriseConfig(secret string) enterprise.ActivateRequest {
	return enterprise.ActivateRequest{
		Id:            localhostEnterpriseClusterId,
		LicenseServer: "grpc://localhost:1653",
		Secret:        secret,
	}
}

func licenseStep(_ *client.APIClient, ec *client.APIClient) error {
	key, err := skipIfNotExistResolvable(licensePath)
	if err != nil {
		return err
	}

	_, err = ec.License.Activate(ec.Ctx(), &license.ActivateRequest{
		ActivationCode: string(key),
	})
	return err
}

func enterpriseSecretStep(_ *client.APIClient, ec *client.APIClient) error {
	secret, err := skipIfNotExistResolvable(enterpriseSecretPath)
	if err != nil {
		return err
	}

	cluster := localhostEnterpriseCluster(string(secret))
	if _, err := ec.License.AddCluster(ec.Ctx(), &cluster); err != nil {
		if !license.IsErrDuplicateClusterID(err) {
			return err
		}
	}

	config := localhostEnterpriseConfig(string(secret))
	_, err = ec.Enterprise.Activate(ec.Ctx(), &config)
	return err
}

func syncEnterpriseClusters(ec *client.APIClient, clusters []license.AddClusterRequest) error {
	for _, cluster := range clusters {
		if v, err := resolveIfEnvVar(cluster.ClusterDeploymentId); err != nil {
			return err
		} else {
			cluster.ClusterDeploymentId = v
		}

		if v, err := resolveIfEnvVar(cluster.Secret); err != nil {
			return err
		} else {
			cluster.Secret = v
		}

		if _, err := ec.License.AddCluster(ec.Ctx(), &cluster); err != nil {
			if !license.IsErrDuplicateClusterID(err) {
				return err
			}

			if _, err := ec.License.UpdateCluster(ec.Ctx(), &license.UpdateClusterRequest{
				Id:                  cluster.Id,
				Address:             cluster.Address,
				UserAddress:         cluster.UserAddress,
				ClusterDeploymentId: cluster.ClusterDeploymentId,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

func enterpriseClustersStep(_ *client.APIClient, ec *client.APIClient) error {
	var clusters []license.AddClusterRequest
	if err := loadYAML(enterpriseClustersPath, &clusters); err != nil {
		return err
	}

	return syncEnterpriseClusters(ec, clusters)
}

func syncOIDCClients(ec *client.APIClient, clients []identity.OIDCClient) error {
	for _, client := range clients {
		if v, err := resolveIfEnvVar(client.Secret); err != nil {
			return err
		} else {
			client.Secret = v
		}
		if _, err := ec.CreateOIDCClient(ec.Ctx(), &identity.CreateOIDCClientRequest{Client: &client}); err != nil {
			if !identity.IsErrAlreadyExists(err) {
				return err
			}

			if _, err := ec.UpdateOIDCClient(ec.Ctx(), &identity.UpdateOIDCClientRequest{Client: &client}); err != nil {
				return err
			}
		}
	}

	return nil
}

func oidcClientsStep(_ *client.APIClient, ec *client.APIClient) error {
	var clients []identity.OIDCClient
	if err := loadYAML(oidcClientsPath, &clients); err != nil {
		return err
	}

	return syncOIDCClients(ec, clients)
}

func updateOrCreateIDP(ec *client.APIClient, connector identity.IDPConnector, existing []*identity.IDPConnector) error {
	for _, ex := range existing {
		// If the connector config hasn't changed, don't update it
		if ex.Id == connector.Id {
			connector.ConfigVersion = ex.ConfigVersion
			if proto.Equal(ex, &connector) {
				return nil
			}

			// If we are updating the connector, increment the version
			connector.ConfigVersion = ex.ConfigVersion + 1
			_, err := ec.UpdateIDPConnector(ec.Ctx(), &identity.UpdateIDPConnectorRequest{Connector: &connector})
			return err
		}
	}

	_, err := ec.CreateIDPConnector(ec.Ctx(), &identity.CreateIDPConnectorRequest{Connector: &connector})
	return err
}

func idpsStep(_ *client.APIClient, ec *client.APIClient) error {
	var connectors []identity.IDPConnector
	if err := loadYAML(idpsPath, &connectors); err != nil {
		return err
	}

	// Normally IDP config requires a "ConfigVersion" to be incremented, but when users
	// are using the config pod we should just apply the latest version
	existing, err := ec.ListIDPConnectors(ec.Ctx(), &identity.ListIDPConnectorsRequest{})
	if err != nil {
		return err
	}

	for _, connector := range connectors {
		updateOrCreateIDP(ec, connector, existing.Connectors)
	}

	return nil
}

func roleBindingsStep(c *client.APIClient, _ *client.APIClient) error {
	var roleBinding map[string][]string
	if err := loadYAML(clusterRoleBindingsPath, &roleBinding); err != nil {
		return err
	}

	existing, err := c.GetRoleBinding(c.Ctx(), &auth.GetRoleBindingRequest{
		Resource: &auth.Resource{Type: auth.ResourceType_CLUSTER},
	})
	if err != nil {
		return err
	}

	for p := range existing.Binding.Entries {
		// `pach:` user role bindings cannot be modified
		if strings.HasPrefix(p, auth.PachPrefix) {
			continue
		}

		if _, ok := roleBinding[p]; !ok {
			if _, err := c.ModifyRoleBinding(c.Ctx(), &auth.ModifyRoleBindingRequest{
				Resource:  &auth.Resource{Type: auth.ResourceType_CLUSTER},
				Principal: p,
			}); err != nil {
				return err
			}
		}
	}

	for p, r := range roleBinding {
		if _, err := c.ModifyRoleBinding(c.Ctx(), &auth.ModifyRoleBindingRequest{
			Resource:  &auth.Resource{Type: auth.ResourceType_CLUSTER},
			Principal: p,
			Roles:     r,
		}); err != nil {
			return err
		}
	}

	return nil
}

func enterpriseConfigStep(c *client.APIClient, _ *client.APIClient) error {
	var config enterprise.ActivateRequest
	if err := loadYAML(enterpriseConfigPath, &config); err != nil {
		return err
	}

	if v, err := resolveIfEnvVar(config.Secret); err != nil {
		return err
	} else {
		config.Secret = v
	}

	_, err := c.Enterprise.Activate(c.Ctx(), &config)
	return err
}

func authConfigStep(c *client.APIClient, _ *client.APIClient) error {
	var config auth.OIDCConfig
	if err := loadYAML(authConfigPath, &config); err != nil {
		return err
	}

	if cs, err := resolveIfEnvVar(config.ClientSecret); err != nil {
		return err
	} else {
		config.ClientSecret = cs
	}

	_, err := c.SetConfiguration(c.Ctx(), &auth.SetConfigurationRequest{Configuration: &config})
	return err
}

func activateAuthStep(c *client.APIClient, _ *client.APIClient) error {
	rootToken, err := loadRootToken()
	if err != nil {
		return err
	}

	_, err = c.Activate(c.Ctx(), &auth.ActivateRequest{
		RootToken: string(rootToken),
	})

	if err != nil {
		if auth.IsErrAlreadyActivated(err) {
			return nil
		}

		return err
	}

	if _, err := c.PfsAPIClient.ActivateAuth(c.Ctx(), &pfs.ActivateAuthRequest{}); err != nil {
		return err
	}

	if _, err := c.PpsAPIClient.ActivateAuth(c.Ctx(), &pps.ActivateAuthRequest{}); err != nil {
		return err
	}

	return nil
}

func identityServiceConfigStep(c *client.APIClient, _ *client.APIClient) error {
	var config identity.IdentityServerConfig
	if err := loadYAML(identityServiceConfigPath, &config); err != nil {
		return err
	}

	_, err := c.SetIdentityServerConfig(c.Ctx(), &identity.SetIdentityServerConfigRequest{Config: &config})
	return err
}
