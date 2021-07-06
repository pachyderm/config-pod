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
)

func syncLicense(c *client.APIClient) error {
	key, err := skipIfNotExist(licensePath)
	if err != nil {
		return err
	}

	_, err = c.License.Activate(c.Ctx(), &license.ActivateRequest{
		ActivationCode: string(key),
	})
	return err
}

func syncEnterpriseClusters(c *client.APIClient) error {
	var clusters []license.AddClusterRequest
	if err := loadYAML(enterpriseClustersPath, &clusters); err != nil {
		return err
	}

	for _, cluster := range clusters {
		if _, err := c.License.AddCluster(c.Ctx(), &cluster); err != nil {
			if !license.IsErrDuplicateClusterID(err) {
				return err
			}

			if _, err := c.License.UpdateCluster(c.Ctx(), &license.UpdateClusterRequest{
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

func syncOIDCClients(c *client.APIClient) error {
	var clients []identity.OIDCClient
	if err := loadYAML(oidcClientsPath, &clients); err != nil {
		return err
	}

	for _, client := range clients {
		if _, err := c.CreateOIDCClient(c.Ctx(), &identity.CreateOIDCClientRequest{Client: &client}); err != nil {
			if !identity.IsErrAlreadyExists(err) {
				return err
			}

			if _, err := c.UpdateOIDCClient(c.Ctx(), &identity.UpdateOIDCClientRequest{Client: &client}); err != nil {
				return err
			}
		}
	}

	return nil
}

func syncIDPs(c *client.APIClient) error {
	var connectors []identity.IDPConnector
	if err := loadYAML(idpsPath, &connectors); err != nil {
		return err
	}

	for _, connector := range connectors {
		if _, err := c.CreateIDPConnector(c.Ctx(), &identity.CreateIDPConnectorRequest{Connector: &connector}); err != nil {
			if !identity.IsErrAlreadyExists(err) {
				return err
			}

			if _, err := c.UpdateIDPConnector(c.Ctx(), &identity.UpdateIDPConnectorRequest{Connector: &connector}); err != nil {
				return err
			}
		}
	}

	return nil
}

func syncRoleBindings(c *client.APIClient) error {
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

func configureEnterprise(c *client.APIClient) error {
	var config enterprise.ActivateRequest
	if err := loadYAML(enterpriseConfigPath, &config); err != nil {
		return err
	}

	_, err := c.Enterprise.Activate(c.Ctx(), &config)
	return err
}
func configureAuth(c *client.APIClient) error {
	var config auth.OIDCConfig
	if err := loadYAML(authConfigPath, &config); err != nil {
		return err
	}

	_, err := c.SetConfiguration(c.Ctx(), &auth.SetConfigurationRequest{Configuration: &config})
	return err
}

func activateAuth(c *client.APIClient) error {
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

func configureIdentityService(c *client.APIClient) error {
	var config identity.IdentityServerConfig
	if err := loadYAML(identityServiceConfigPath, &config); err != nil {
		return err
	}

	_, err := c.SetIdentityServerConfig(c.Ctx(), &identity.SetIdentityServerConfigRequest{Config: &config})
	return err
}
