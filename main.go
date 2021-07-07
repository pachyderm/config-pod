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
	licensePath               = "license"
	enterpriseSecretPath      = "enterpriseSecret"
	enterpriseClustersPath    = "enterpriseClusters"
	enterpriseConfigPath      = "enterpriseConfig"
	clusterRoleBindingsPath   = "clusterRoleBindings"
	identityServiceConfigPath = "identityServiceConfig"
	idpsPath                  = "idps"
	oidcClientsPath           = "oidcClients"
	authConfigPath            = "authConfig"
)

type clusterSyncFn func(*client.APIClient) error

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
	c, err := client.NewFromURI(pachAddr)
	if err != nil {
		log.WithError(err).Error("failed to connect to pachyderm")
		os.Exit(1)
	}

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

	for _, step := range syncSteps {
		stepLogger := log.WithField("step", step.name)
		stepLogger.Info("running step")
		err := step.fn(c)
		if err != nil {
			if !errors.Is(err, errSkipped) {
				stepLogger.WithError(err).Error("error syncing cluster state")
				os.Exit(1)
			}
			stepLogger.WithField("reason", err).Info("skipped")
		}
	}
}
