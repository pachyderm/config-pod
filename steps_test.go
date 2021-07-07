package main

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/pachyderm/pachyderm/v2/src/auth"
	"github.com/pachyderm/pachyderm/v2/src/client"
	"github.com/pachyderm/pachyderm/v2/src/enterprise"
	"github.com/pachyderm/pachyderm/v2/src/identity"
	"github.com/pachyderm/pachyderm/v2/src/license"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/suite"
)

var (
	testRootToken = "testroottoken"

	oidcConfig = auth.OIDCConfig{
		Issuer:          "http://localhost:30658/",
		ClientID:        "oidcsecret",
		ClientSecret:    "notsecret",
		RedirectURI:     "http://pachd:1657/authorization-code/callback",
		LocalhostIssuer: true,
		Scopes:          auth.DefaultOIDCScopes,
	}

	pachydermOIDCClient = identity.OIDCClient{
		Id:           "pachyderm",
		RedirectUris: []string{"http://pachd:1657/authorization-code/callback"},
		Name:         "pachd",
		Secret:       "oidcsecret",
	}

	mockIDPConnector = identity.IDPConnector{
		Name:       "test",
		Id:         "test",
		Type:       "mockPassword",
		JsonConfig: `{"username": "admin", "password": "password"}`,
	}

	externalEnterpriseCluster = license.AddClusterRequest{
		Id:          "external",
		Address:     "grpc://localhost:1653",
		UserAddress: "grpc://localhost:1653",
		Secret:      "externalSecret",
	}
)

type StepTestSuite struct {
	suite.Suite
	c *client.APIClient
}

func (s *StepTestSuite) writeFile(filename string, data []byte) {
	s.Require().NoError(ioutil.WriteFile(path.Join(configRoot, filename), data, os.ModePerm))
}

func (s *StepTestSuite) writeYAML(filename string, data interface{}) {
	yamlData, err := yaml.Marshal(data)
	s.Require().NoError(err)
	s.writeFile(filename, yamlData)
}

func TestStepTestSuite(t *testing.T) {
	suite.Run(t, new(StepTestSuite))
}

func (s *StepTestSuite) SetupSuite() {
	var err error
	s.c, err = client.NewFromURI(os.Getenv("PACH_ADDRESS"))
	s.Require().NoError(err)

	s.c.SetAuthToken(testRootToken)
}

func (s *StepTestSuite) SetupTest() {
	s.c.DeleteAll()
	var err error
	configRoot, err = ioutil.TempDir("", "example")
	s.Require().NoError(err)
}

// writeSingleNodeConfig writes out the config files for
// a single enterprise-licensed pachd with auth enaabled
func (s *StepTestSuite) writeSingleNodeConfig() {
	// write out an enterprise token
	s.writeFile(licensePath, []byte(os.Getenv("ENT_ACT_CODE")))

	// write out the root token
	s.writeFile(rootTokenPath, []byte(testRootToken))

	// configure enterprise secret
	s.writeFile(enterpriseSecretPath, []byte("enterpriseSecret"))

	// configure the identity service
	s.writeYAML(identityServiceConfigPath, identity.IdentityServerConfig{
		Issuer: "http://localhost:30658/",
	})

	// configure an OIDC client for pachd
	s.writeYAML(oidcClientsPath, []identity.OIDCClient{pachydermOIDCClient})

	// configure the auth service
	s.writeYAML(authConfigPath, oidcConfig)

	// configure an IDP connector
	s.writeYAML(idpsPath, []identity.IDPConnector{mockIDPConnector})

	// add a role binding
	s.writeYAML(clusterRoleBindingsPath, map[string][]string{
		"robot:test": []string{"repoReader"},
	})
}

// TestConfigureSingleNodeAuth tests configuring a single pachd to authenticate using an IDP.
func (s *StepTestSuite) TestConfigureSingleNodeAuth() {
	s.writeSingleNodeConfig()

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	// check that we're authenticated as the root user and auth is active
	resp, err := s.c.WhoAmI(s.c.Ctx(), &auth.WhoAmIRequest{})
	s.Require().NoError(err)
	s.Require().Equal(resp.Username, "pach:root")

	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().Equal(1, len(clients.Clients))
	s.Require().Equal(&pachydermOIDCClient, clients.Clients[0])

	authConfig, err := s.c.GetConfiguration(s.c.Ctx(), &auth.GetConfigurationRequest{})
	s.Require().NoError(err)
	s.Require().Equal(&oidcConfig, authConfig.Configuration)

	idps, err := s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().Equal(1, len(idps.Connectors))
	s.Require().Equal(&mockIDPConnector, idps.Connectors[0])

	roleBinding, err := s.c.GetRoleBinding(s.c.Ctx(), &auth.GetRoleBindingRequest{
		Resource: &auth.Resource{Type: auth.ResourceType_CLUSTER},
	})
	s.Require().NoError(err)
	s.Require().Equal(map[string]*auth.Roles{
		"pach:root":  &auth.Roles{Roles: map[string]bool{"clusterAdmin": true}},
		"robot:test": &auth.Roles{Roles: map[string]bool{"repoReader": true}},
	}, roleBinding.Binding.Entries)
}

// TestSkipStep tests that every step raises errSkipped if there's no configuration
func (s *StepTestSuite) TestSkipStep() {
	for _, step := range syncSteps {
		err := step.fn(s.c)
		s.Require().True(errors.Is(err, errSkipped))
	}
}

// TestUpdateState tests that a subsequent run of the pod updates the OIDC clients, IDPs, clusters, role bindings
func (s *StepTestSuite) TestUpdateState() {
	// write the initial config and apply it
	s.writeSingleNodeConfig()
	for _, step := range syncSteps {
		step.fn(s.c)
	}

	// update the config and re-apply it
	updatedClient := pachydermOIDCClient
	updatedClient.Name = "updated"

	newClient := identity.OIDCClient{
		Id:           "new",
		RedirectUris: []string{"http://other:1657/authorization-code/callback"},
		Name:         "new",
		Secret:       "secret",
	}
	s.writeYAML(oidcClientsPath, []identity.OIDCClient{updatedClient, newClient})

	updatedIDP := mockIDPConnector
	updatedIDP.Name = "updated"

	newIDP := identity.IDPConnector{
		Name:       "new",
		Id:         "new",
		Type:       "mockPassword",
		JsonConfig: `{"username": "admin", "password": "password"}`,
	}
	s.writeYAML(idpsPath, []identity.IDPConnector{updatedIDP, newIDP})

	s.writeYAML(clusterRoleBindingsPath, map[string][]string{
		"robot:new": []string{"repoWriter"},
	})

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().Equal(2, len(clients.Clients))
	s.Require().Equal(&updatedClient, clients.Clients[0])
	s.Require().Equal(&newClient, clients.Clients[1])

	authConfig, err := s.c.GetConfiguration(s.c.Ctx(), &auth.GetConfigurationRequest{})
	s.Require().NoError(err)
	s.Require().Equal(&oidcConfig, authConfig.Configuration)

	idps, err := s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().Equal(2, len(idps.Connectors))
	// update the config version, we handle this automatically
	updatedIDP.ConfigVersion = 1
	s.Require().Equal(&updatedIDP, idps.Connectors[0])
	s.Require().Equal(&newIDP, idps.Connectors[1])

	roleBinding, err := s.c.GetRoleBinding(s.c.Ctx(), &auth.GetRoleBindingRequest{
		Resource: &auth.Resource{Type: auth.ResourceType_CLUSTER},
	})
	s.Require().NoError(err)
	s.Require().Equal(map[string]*auth.Roles{
		"pach:root": &auth.Roles{Roles: map[string]bool{"clusterAdmin": true}},
		"robot:new": &auth.Roles{Roles: map[string]bool{"repoWriter": true}},
	}, roleBinding.Binding.Entries)

	// Run again, no changes, should be idempotent
	for _, step := range syncSteps {
		step.fn(s.c)
	}

	clients, err = s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().Equal(2, len(clients.Clients))
	s.Require().Equal(&updatedClient, clients.Clients[0])
	s.Require().Equal(&newClient, clients.Clients[1])

	authConfig, err = s.c.GetConfiguration(s.c.Ctx(), &auth.GetConfigurationRequest{})
	s.Require().NoError(err)
	s.Require().Equal(&oidcConfig, authConfig.Configuration)

	idps, err = s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().Equal(2, len(idps.Connectors))
	s.Require().Equal(&updatedIDP, idps.Connectors[0])
	s.Require().Equal(&newIDP, idps.Connectors[1])

	roleBinding, err = s.c.GetRoleBinding(s.c.Ctx(), &auth.GetRoleBindingRequest{
		Resource: &auth.Resource{Type: auth.ResourceType_CLUSTER},
	})
	s.Require().NoError(err)
	s.Require().Equal(map[string]*auth.Roles{
		"pach:root": &auth.Roles{Roles: map[string]bool{"clusterAdmin": true}},
		"robot:new": &auth.Roles{Roles: map[string]bool{"repoWriter": true}},
	}, roleBinding.Binding.Entries)
}

// TestEnterpriseConfig tests configuring a pachd to talk to an external enterprise server
func (s *StepTestSuite) TestEnterpriseConfig() {
	s.writeFile(licensePath, []byte(os.Getenv("ENT_ACT_CODE")))
	s.writeYAML(enterpriseClustersPath, []license.AddClusterRequest{externalEnterpriseCluster})
	s.writeYAML(enterpriseConfigPath, enterprise.ActivateRequest{
		Id:            "external",
		LicenseServer: "grpc://localhost:1653",
		Secret:        "externalSecret",
	})

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	clusters, err := s.c.License.ListClusters(s.c.Ctx(), &license.ListClustersRequest{})
	s.Require().NoError(err)
	s.Require().Equal(1, len(clusters.Clusters))

	state, err := s.c.Enterprise.GetState(s.c.Ctx(), &enterprise.GetStateRequest{})
	s.Require().NoError(err)
	s.Require().Equal(enterprise.State_ACTIVE, state.State)

	updatedCluster := externalEnterpriseCluster
	updatedCluster.Address = "grpc://localhost:1650"

	newCluster := license.AddClusterRequest{
		Id:          "external2",
		Address:     "grpc://localhost:1653",
		UserAddress: "grpc://external2:1653",
		Secret:      "externalSecret2",
	}
	s.writeYAML(enterpriseClustersPath, []license.AddClusterRequest{updatedCluster, newCluster})
	for _, step := range syncSteps {
		step.fn(s.c)
	}

	clusters, err = s.c.License.ListClusters(s.c.Ctx(), &license.ListClustersRequest{})
	s.Require().NoError(err)
	s.Require().Equal(2, len(clusters.Clusters))
	s.Require().Equal("grpc://localhost:1653", clusters.Clusters[0].Address)
	s.Require().Equal("grpc://localhost:1650", clusters.Clusters[1].Address)
}
