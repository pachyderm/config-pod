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
	testRootToken  = "testroottoken"
	testIssuer     = "http://localhost:30658/"
	testOIDCSecret = "oidcsecret"
	testRedirect   = "http://localhost:30657/redirect"

	oidcConfig          = localhostOIDCConfig(testIssuer, testOIDCSecret, testRedirect)
	pachydermOIDCClient = localhostOIDCClient(testOIDCSecret, testRedirect, []string{})
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

// TestSkipStep tests that every step raises errSkipped if there's no configuration
func (s *StepTestSuite) TestSkipStep() {
	for _, step := range syncSteps {
		err := step.fn(s.c)
		s.Require().True(errors.Is(err, errSkipped))
	}
}

// writeSimpleConfig writes a simple, minimal config for a single node
func (s *StepTestSuite) writeSimpleConfig() {
	// write out an enterprise token
	s.writeFile(licensePath, []byte(os.Getenv("ENT_ACT_CODE")))

	// write out the root token
	s.writeFile(rootTokenPath, []byte(testRootToken))

	// configure enterprise secret
	s.writeFile(enterpriseSecretPath, []byte("enterpriseSecret"))

	// configure auth using the simple config
	s.writeYAML(authPath, simpleAuthConfig{
		Issuer:      testIssuer,
		Secret:      testOIDCSecret,
		RedirectURI: testRedirect,
	})
}

// TestSimpleConfig tests configuring a single pachd with only the simple config
func (s *StepTestSuite) TestSimpleConfig() {
	s.writeSimpleConfig()

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	// check that we're authenticated as the root user and auth is active
	resp, err := s.c.WhoAmI(s.c.Ctx(), &auth.WhoAmIRequest{})
	s.Require().NoError(err)
	s.Require().Equal(resp.Username, "pach:root")

	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().Equal(1, len(clients.Clients))
	s.Require().Nil(clients.Clients[0].TrustedPeers)
	clients.Clients[0].TrustedPeers = []string{}
	s.Require().Equal(&pachydermOIDCClient, clients.Clients[0])

	authConfig, err := s.c.GetConfiguration(s.c.Ctx(), &auth.GetConfigurationRequest{})
	s.Require().NoError(err)
	s.Require().Equal(&oidcConfig, authConfig.Configuration)

}

// TestFullConfig tests explicitly setting the IDP and OIDC config, rather than using the simple config
func (s *StepTestSuite) TestFullConfig() {
	// write out an enterprise token
	s.writeFile(licensePath, []byte(os.Getenv("ENT_ACT_CODE")))

	// write out the root token
	s.writeFile(rootTokenPath, []byte(testRootToken))

	// configure the identity service
	s.writeYAML(identityServiceConfigPath, identity.IdentityServerConfig{
		Issuer: testIssuer,
	})

	s.writeYAML(authConfigPath, oidcConfig)

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	authConfig, err := s.c.GetConfiguration(s.c.Ctx(), &auth.GetConfigurationRequest{})
	s.Require().NoError(err)
	s.Require().Equal(&oidcConfig, authConfig.Configuration)
}

func (s *StepTestSuite) TestRoleBindings() {
	s.writeSimpleConfig()

	// add a role binding
	s.writeYAML(clusterRoleBindingsPath, map[string][]string{
		"robot:test": []string{"repoReader"},
	})

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	roleBinding, err := s.c.GetRoleBinding(s.c.Ctx(), &auth.GetRoleBindingRequest{
		Resource: &auth.Resource{Type: auth.ResourceType_CLUSTER},
	})
	s.Require().NoError(err)
	s.Require().Equal(map[string]*auth.Roles{
		"pach:root":  &auth.Roles{Roles: map[string]bool{"clusterAdmin": true}},
		"robot:test": &auth.Roles{Roles: map[string]bool{"repoReader": true}},
	}, roleBinding.Binding.Entries)

	s.writeYAML(clusterRoleBindingsPath, map[string][]string{
		"robot:test2": []string{"repoWriter"},
	})

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	roleBinding, err = s.c.GetRoleBinding(s.c.Ctx(), &auth.GetRoleBindingRequest{
		Resource: &auth.Resource{Type: auth.ResourceType_CLUSTER},
	})
	s.Require().NoError(err)
	s.Require().Equal(map[string]*auth.Roles{
		"pach:root":   &auth.Roles{Roles: map[string]bool{"clusterAdmin": true}},
		"robot:test2": &auth.Roles{Roles: map[string]bool{"repoWriter": true}},
	}, roleBinding.Binding.Entries)

}

func (s *StepTestSuite) TestIDPs() {
	s.writeSimpleConfig()

	mockIDPConnector := identity.IDPConnector{
		Name:       "test",
		Id:         "test",
		Type:       "mockPassword",
		JsonConfig: `{"username": "admin", "password": "password"}`,
	}

	// configure an IDP connector
	s.writeYAML(idpsPath, []identity.IDPConnector{mockIDPConnector})

	for _, step := range syncSteps {
		step.fn(s.c)
	}

	idps, err := s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(1, len(idps.Connectors))
	s.Require().Equal(&mockIDPConnector, idps.Connectors[0])

	mockIDPConnector.Name = "updated"

	s.writeYAML(idpsPath, []identity.IDPConnector{mockIDPConnector})
	for _, step := range syncSteps {
		step.fn(s.c)
	}

	idps, err = s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(1, len(idps.Connectors))
	mockIDPConnector.ConfigVersion = 1
	s.Require().Equal(&mockIDPConnector, idps.Connectors[0])
}

// TestOIDCClients tests configuring additional OIDC clients
func (s *StepTestSuite) TestOIDCClients() {
	s.writeSimpleConfig()

	newClient := identity.OIDCClient{
		Id:           "new",
		RedirectUris: []string{"http://other:1657/authorization-code/callback"},
		Name:         "new",
		Secret:       "secret",
	}

	s.writeYAML(oidcClientsPath, []identity.OIDCClient{newClient})
	for _, step := range syncSteps {
		step.fn(s.c)
	}

	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(2, len(clients.Clients))
	s.Require().Equal(&newClient, clients.Clients[0])

	newClient.Name = "updated"

	s.writeYAML(oidcClientsPath, []identity.OIDCClient{newClient})
	for _, step := range syncSteps {
		step.fn(s.c)
	}

	clients, err = s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(2, len(clients.Clients))
	s.Require().Equal(&newClient, clients.Clients[0])
}

// TestEnterpriseConfig tests configuring a pachd to talk to an external enterprise server
func (s *StepTestSuite) TestEnterpriseConfig() {
	externalEnterpriseCluster := license.AddClusterRequest{
		Id:          "external",
		Address:     "grpc://localhost:1653",
		UserAddress: "grpc://localhost:1653",
		Secret:      "externalSecret",
	}

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
