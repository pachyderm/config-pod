package main

import (
	"errors"
	"os"

	"github.com/pachyderm/pachyderm/v2/src/client"

	log "github.com/sirupsen/logrus"
)

const (
	// These are the keys for the config secret
	rootTokenPath             = "rootToken"
	enterpriseRootTokenPath   = "enterpriseRootToken"
	enterpriseServerAddress   = "enterpriseServerAddress"
	licensePath               = "license"
	enterpriseSecretPath      = "enterpriseSecret"
	enterpriseClustersPath    = "enterpriseClusters"
	enterpriseConfigPath      = "enterpriseConfig"
	clusterRoleBindingsPath   = "clusterRoleBindings"
	identityServiceConfigPath = "identityServiceConfig"
	idpsPath                  = "idps"
	oidcClientsPath           = "oidcClients"
	authConfigPath            = "authConfig"
	authPath                  = "auth"
)

// the 1st client represents the pachd instance that needs to register with an enterprise server represented by the 2nd argument ("ec")
// in the case of embedded servers, i.e. when the 'enterpriseServerAddress' path isn't populated, the two clients will be the same
type clusterSyncFn func(c *client.APIClient, ec *client.APIClient) error

type syncStep struct {
	name string
	fn   clusterSyncFn
}

var syncSteps = []syncStep{
	syncStep{"license key", licenseStep},
	syncStep{"enterprise secret", enterpriseSecretStep},
	syncStep{"sync enterprise clusters", enterpriseClustersStep},
	syncStep{"configure enterprise service", enterpriseConfigStep},
	syncStep{"activate authentication", activateAuthStep},
	syncStep{"configure identity service", identityServiceConfigStep},
	syncStep{"sync oidc clients", oidcClientsStep},
	syncStep{"configure auth", authConfigStep},
	syncStep{"sync identity providers", idpsStep},
	syncStep{"sync cluster role bindings", roleBindingsStep},
}

var (
	configRoot string
	pachAddr   string
)

func main() {
	configRoot = os.Getenv("PACH_CONFIG_ROOT")
	if configRoot == "" {
		configRoot = "/pachConfig"
	}

	pachAddr = os.Getenv("PACH_ADDR")
	if pachAddr == "" {
		pachAddr = "grpc://pachd-peer:30653"
	}

	log.WithField("addr", pachAddr).Infof("connecting to pachyderm")
	c := connectToPach(pachAddr)

	log.Infof("loading root auth token")
	rootToken, err := loadRootToken()
	if err != nil {
		if !errors.Is(err, errSkipped) {
			log.WithError(err).Error("failed to load root auth token")
			os.Exit(1)
		}
		log.WithField("reason", err).Info("not using auth token")
	} else {
		c.SetAuthToken(string(rootToken))
	}

	var ec *client.APIClient
	enterpriseServerAddrBytes, err := loadEnterpriseServerAddress()
	if err != nil {
		ec = c
	} else {
		ec = connectToPach(string(enterpriseServerAddrBytes))
		enterpriseRootToken, err := loadEnterpriseRootToken()
		if err != nil {
			log.WithError(err).Error("failed to load enterprise root auth token")
			os.Exit(1)
		}
		ec.SetAuthToken(string(enterpriseRootToken))
	}

	for _, step := range syncSteps {
		stepLogger := log.WithField("step", step.name)
		stepLogger.Info("running step")
		err := step.fn(c, ec)
		if err != nil {
			if !errors.Is(err, errSkipped) {
				stepLogger.WithError(err).Error("error syncing cluster state")
				os.Exit(1)
			}
			stepLogger.WithField("reason", err).Info("skipped")
		}
	}
}
