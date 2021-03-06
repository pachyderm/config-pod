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
	pachydermOIDCClient = localhostOIDCClient(testOIDCSecret, testRedirect, []string(nil))
)

func localhostOIDCClient(secret, redirect string, trustedPeers []string) identity.OIDCClient {
	return identity.OIDCClient{
		Id:           "pachd",
		Name:         "pachd",
		Secret:       secret,
		RedirectUris: []string{redirect},
		TrustedPeers: trustedPeers,
	}
}

func localhostOIDCConfig(issuer, secret, redirect string) auth.OIDCConfig {
	return auth.OIDCConfig{
		Issuer:          issuer,
		ClientID:        "pachd",
		ClientSecret:    secret,
		RedirectURI:     redirect,
		LocalhostIssuer: true,
		Scopes:          auth.DefaultOIDCScopes,
	}
}

type StepTestSuite struct {
	suite.Suite
	c *client.APIClient
}

func (s *StepTestSuite) RequireNilOrSkipped(err error) {
	s.T().Helper()
	s.Require().True(err == nil || errors.Is(err, errSkipped))
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
	s.Require().NoError(s.c.DeleteAll())
	var err error
	configRoot, err = ioutil.TempDir("", "example")
	s.Require().NoError(err)
}

// TestSkipStep tests that every step raises errSkipped if there's no configuration
func (s *StepTestSuite) TestSkipStep() {
	for _, step := range syncSteps {
		err := step.fn(s.c, s.c)
		s.Require().ErrorIs(err, errSkipped)
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

	// configure test issuer
	s.writeYAML(identityServiceConfigPath, identity.IdentityServerConfig{Issuer: testIssuer})

	// configure oidc client
	s.writeYAML(oidcClientsPath, []identity.OIDCClient{pachydermOIDCClient})

	// configure auth config
	s.writeYAML(authConfigPath, oidcConfig)
}

// TestSimpleConfig tests configuring a single pachd with only the simple config
func (s *StepTestSuite) TestSimpleConfig() {
	s.writeSimpleConfig()

	for _, step := range syncSteps {
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
	}

	// check that we're authenticated as the root user and auth is active
	resp, err := s.c.WhoAmI(s.c.Ctx(), &auth.WhoAmIRequest{})
	s.Require().NoError(err)
	s.Require().Equal(resp.Username, "pach:root")

	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(1, len(clients.Clients))
	s.Require().Nil(clients.Clients[0].TrustedPeers)
	clients.Clients[0].TrustedPeers = []string(nil)
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
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
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
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
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
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
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
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
	}

	idps, err := s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(1, len(idps.Connectors))
	s.Require().Equal(&mockIDPConnector, idps.Connectors[0])

	mockIDPConnector.Name = "updated"

	s.writeYAML(idpsPath, []identity.IDPConnector{mockIDPConnector})
	for _, step := range syncSteps {
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
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

	// test that oidcClient.Secret value resolves if it's an environment variable
	newClientWithEnvVarSecret := identity.OIDCClient{
		Id:           "withEnvVarSecret",
		RedirectUris: []string{"http://other:1657/authorization-code/callback"},
		Name:         "withEnvVarSecret",
		Secret:       "$TEST_SECRET",
	}
	expectedNewClientWithEnvVarSecret := newClientWithEnvVarSecret

	s.Require().NoError(os.Setenv("TEST_SECRET", "test_secret_value"))
	expectedNewClientWithEnvVarSecret.Secret = "test_secret_value"

	s.writeYAML(oidcClientsPath, []identity.OIDCClient{pachydermOIDCClient, newClient, newClientWithEnvVarSecret})
	for _, step := range syncSteps {
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
	}

	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(3, len(clients.Clients))
	s.Require().Equal(&pachydermOIDCClient, clients.Clients[0])
	s.Require().Equal(&newClient, clients.Clients[1])
	s.Require().Equal(&expectedNewClientWithEnvVarSecret, clients.Clients[2])

	newClient.Name = "updated"

	s.writeYAML(oidcClientsPath, []identity.OIDCClient{newClient})
	for _, step := range syncSteps {
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
	}

	clients, err = s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(3, len(clients.Clients))
	s.Require().Equal(&newClient, clients.Clients[2])
}

// TestEnterpriseConfig tests configuring a pachd to talk to an external enterprise server
func (s *StepTestSuite) TestEnterpriseConfig() {
	externalEnterpriseCluster := license.AddClusterRequest{
		Id:                  "external",
		Address:             "grpc://localhost:1653",
		UserAddress:         "grpc://localhost:1653",
		Secret:              "externalSecret",
		ClusterDeploymentId: "cluster-deployment-1",
	}

	s.writeFile(licensePath, []byte(os.Getenv("ENT_ACT_CODE")))
	s.writeYAML(enterpriseClustersPath, []license.AddClusterRequest{externalEnterpriseCluster})
	s.writeYAML(enterpriseConfigPath, enterprise.ActivateRequest{
		Id:            "external",
		LicenseServer: "grpc://localhost:1653",
		Secret:        "externalSecret",
	})

	for _, step := range syncSteps {
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
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
		Id:                  "external2",
		Address:             "grpc://localhost:1653",
		UserAddress:         "grpc://external2:1653",
		Secret:              "externalSecret2",
		ClusterDeploymentId: "$CLUSTER_2_DEPLOYMENT",
	}
	// testing that ClusterDeploymentId can reference an environment variable
	os.Setenv("CLUSTER_2_DEPLOYMENT", "refrenced-depoyment-id")

	s.writeYAML(enterpriseClustersPath, []license.AddClusterRequest{updatedCluster, newCluster})
	for _, step := range syncSteps {
		s.RequireNilOrSkipped(step.fn(s.c, s.c))
	}

	clusters, err = s.c.License.ListClusters(s.c.Ctx(), &license.ListClustersRequest{})
	s.Require().NoError(err)
	s.Require().Equal(2, len(clusters.Clusters))
	s.Require().Equal("grpc://localhost:1653", clusters.Clusters[0].Address)
	s.Require().Equal("grpc://localhost:1650", clusters.Clusters[1].Address)

	userClusters, err := s.c.License.ListUserClusters(s.c.Ctx(), &license.ListUserClustersRequest{})
	s.Require().NoError(err)
	s.Require().Equal(2, len(userClusters.Clusters))
	s.Require().Equal("refrenced-depoyment-id", userClusters.Clusters[0].ClusterDeploymentId)
	s.Require().Equal("cluster-deployment-1", userClusters.Clusters[1].ClusterDeploymentId)
}
