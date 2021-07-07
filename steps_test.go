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

const testRootToken = "testroottoken"

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

// TestConfigureSingleNodeAuth tests configuring a single pachd to authenticate using an IDP.
func (s *StepTestSuite) TestConfigureSingleNodeAuth() {
	// write out an enterprise token
	s.writeFile(licensePath, []byte(os.Getenv("ENT_ACT_CODE")))
	s.Require().NoError(syncLicense(s.c))

	// create the enterprise cluster
	s.writeYAML(enterpriseClustersPath, []license.AddClusterRequest{
		{
			Id:      "localhost",
			Secret:  "secret",
			Address: "localhost:1650",
		},
	})
	s.Require().NoError(syncEnterpriseClusters(s.c))

	// register the enterprise cluster
	s.writeYAML(enterpriseConfigPath, enterprise.ActivateRequest{
		LicenseServer: "localhost:1650",
		Id:            "localhost",
		Secret:        "secret",
	})
	s.Require().NoError(configureEnterprise(s.c))

	// write out the root token
	s.writeFile(rootTokenPath, []byte(testRootToken))
	s.Require().NoError(activateAuth(s.c))

	// check that we're authenticated as the root user and auth is active
	resp, err := s.c.WhoAmI(s.c.Ctx(), &auth.WhoAmIRequest{})
	s.Require().NoError(err)
	s.Require().Equal(resp.Username, "pach:root")

	// configure the identity service
	s.writeYAML(identityServiceConfigPath, identity.IdentityServerConfig{
		Issuer: "http://localhost:30658/",
	})
	s.Require().NoError(configureIdentityService(s.c))

	// configure an OIDC client for pachd
	client := identity.OIDCClient{
		Id:           "pachyderm",
		RedirectUris: []string{"http://pachd:1657/authorization-code/callback"},
		Name:         "pachd",
		Secret:       "oidcsecret",
	}
	s.writeYAML(oidcClientsPath, []identity.OIDCClient{client})
	s.Require().NoError(syncOIDCClients(s.c))
	clients, err := s.c.ListOIDCClients(s.c.Ctx(), &identity.ListOIDCClientsRequest{})
	s.Require().Equal(1, len(clients.Clients))
	s.Require().Equal(&client, clients.Clients[0])

	config := auth.OIDCConfig{
		Issuer:          "http://localhost:30658/",
		ClientID:        "oidcsecret",
		ClientSecret:    "notsecret",
		RedirectURI:     "http://pachd:1657/authorization-code/callback",
		LocalhostIssuer: true,
		Scopes:          auth.DefaultOIDCScopes,
	}
	// configure the auth service
	s.writeYAML(authConfigPath, config)
	s.Require().NoError(configureAuth(s.c))

	authConfig, err := s.c.GetConfiguration(s.c.Ctx(), &auth.GetConfigurationRequest{})
	s.Require().NoError(err)
	s.Require().Equal(&config, authConfig.Configuration)

	// configure an IDP connect
	connector := identity.IDPConnector{
		Name:       "test",
		Id:         "test",
		Type:       "mockPassword",
		JsonConfig: `{"username": "admin", "password": "password"}`,
	}
	s.writeYAML(idpsPath, []identity.IDPConnector{connector})
	s.Require().NoError(syncIDPs(s.c))
	idps, err := s.c.ListIDPConnectors(s.c.Ctx(), &identity.ListIDPConnectorsRequest{})
	s.Require().Equal(1, len(idps.Connectors))
	s.Require().Equal(&connector, idps.Connectors[0])

	// add a role binding
	s.writeYAML(clusterRoleBindingsPath, map[string][]string{
		"robot:test": []string{"repoReader"},
	})
	s.Require().NoError(syncRoleBindings(s.c))
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
