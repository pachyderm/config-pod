package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pachyderm/pachyderm/v2/src/auth"
	"github.com/pachyderm/pachyderm/v2/src/client"
	"github.com/pachyderm/pachyderm/v2/src/pfs"
	"github.com/pachyderm/pachyderm/v2/src/pps"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// configRoot is the path to mount the config secret to
	configRoot = "/pachConfig"

	rootTokenPath             = "rootToken"
	licensePath               = "licenseKey"
	enterpriseClustersPath    = "enterpriseClusters"
	enterpriseConfigPath      = "enterpriseConfig"
	clusterRoleBindingsPath   = "clusterRoleBindings"
	identityServiceConfigPath = "identityServiceConfig"
	idpsPath                  = "idps"
	oidcClientsPath           = "oidcClients"
)

type errSkipped struct {
	msg string
}

func (e errSkipped) Error() string {
	return e.msg
}

type clusterSyncFn func(*client.APIClient) error

type syncStep struct {
	name string
	fn   clusterSyncFn
}

var syncSteps = []syncStep{
	syncStep{"license key", syncLicense},
	syncStep{"sync enterprise clusters", syncEnterpriseClusters},
	syncStep{"configure enterprise service", configureEnterprise},
	syncStep{"activate authentication", activateAuth},
	syncStep{"configure identity service", configureIdentity},
	syncStep{"sync oidc clients", syncOidcClients},
	syncStep{"configure auth", configureAuth},
	syncStep{"sync identity providers", syncIDPs},
	syncStep{"sync cluster role bindings", syncRoleBindings},
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure a Pachyderm cluster by syncing with a k8s secret",
		Run:   run,
	}
}

func run(cmd *cobra.Command, args []string) {
	c := client.NewFromURI("grpc://pachd:1658")

	rootToken, err := loadRootToken()
	if err != nil {
		if !errors.Is(err, errSkipped{}) {
			log.WithError(err).WithField().Error("error loading root token")
			os.Exit(1)
		}
		stepLogger.WithField("reason", err).Info("not using auth token")
	} else {
		c.SetAuthToken(string(rootToken))
	}

	for _, step := range syncSteps {
		stepLogger = log.WithField("step", step.name)
		stepLogger.Info("running step")
		err := step.fn(c)
		if err != nil {
			if !errors.Is(err, errSkipped{}) {
				log.WithError(err).WithField().Error("error syncing cluster state")
				os.Exit(1)
			}
			stepLogger.WithField("reason", err).Info("skipped")
		}
	}
}

func loadRootToken() ([]byte, error) {
	return skipIfNotExist(rootTokenPath)
}

// skipIfNotExist loads the contents of a config file, or returns
// an errSkipped if the file doesn't exist.
func skipIfNotExist(path string) ([]byte, error) {
	data, err := ioutil.ReadFile(filepath.Join(configRoot, path))
	if err != nil {
		if os.IsNotExist(err) {
			return errSkipped{fmt.Sprintf("no file %q", path)}
		}
		return err
	}
	return data, nil
}

func loadYAML(path string, target interface{}) error {
	data, err := skipIfNotExist(path)
	if err != nil {
		return err
	}
	return serde.DecodeYAML(data, &target)
}

func syncLicense(c *client.APIClient) error {
	license, err := skipIfNotExist(licenseKeyPath)
	if err != nil {
		return nil, err
	}

	req := &license.ActivateRequest{
		ActivationCode: key,
	}
	if _, err := c.License.Activate(c.Ctx(), req); err != nil {
		return err
	}
}

func syncEnterpriseClusters(c *client.APIClient) error {
	var clusters []enterprise.AddClusterRequest
	if err := loadYAML(enterpriseClustersPath, &config); err != nil {
		return err
	}

}

func configureEnterprise(c *client.APIClient) error {
	var config enterprise.ActivateRequest
	if err := loadYAML(enterpriseConfigPath, &config); err != nil {
		return err
	}

	_, err := c.Enterprise.Activate(&config)
	return err
}

func activateAuth(c *client.APIClient) error {
	rootToken, err := loadRootToken()
	if err != nil {
		return err
	}

	resp, err := c.Activate(c.Ctx(), &auth.ActivateRequest{
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
