package main

import (
	"errors"
	"os"

	"github.com/pachyderm/pachyderm/v2/src/client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// configRoot is the path to mount the config secret to
	configRoot = "/pachConfig"

	rootTokenPath             = "rootToken"
	licensePath               = "license"
	enterpriseClustersPath    = "enterpriseClusters"
	enterpriseConfigPath      = "enterpriseConfig"
	clusterRoleBindingsPath   = "clusterRoleBindings"
	identityServiceConfigPath = "identityServiceConfig"
	idpsPath                  = "idps"
	oidcClientsPath           = "oidcClients"
)

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
	syncStep{"configure identity service", configureIdentityService},
	syncStep{"sync oidc clients", syncOIDCClients},
	syncStep{"configure auth", configureAuth},
	syncStep{"sync identity providers", syncIDPs},
	syncStep{"sync cluster role bindings", syncRoleBindings},
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure a Pachyderm cluster by syncing with a k8s secret",
		RunE:  run,
	}
	if err := rootCmd.Execute(); err != nil {
		log.WithError(err).Errorf("failed to sync")
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromURI("grpc://pachd:1658")
	if err != nil {
		return err
	}

	rootToken, err := loadRootToken()
	if err != nil {
		if !errors.Is(err, errSkipped{}) {
			return err
		}
		log.WithField("reason", err).Info("not using auth token")
	} else {
		c.SetAuthToken(string(rootToken))
	}

	for _, step := range syncSteps {
		stepLogger := log.WithField("step", step.name)
		stepLogger.Info("running step")
		err := step.fn(c)
		if err != nil {
			if !errors.Is(err, errSkipped{}) {
				stepLogger.WithError(err).Error("error syncing cluster state")
				return err
			}
			stepLogger.WithField("reason", err).Info("skipped")
		}
	}
	return nil
}
