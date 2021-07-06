## config-pod

config-pod is a Docker image which takes YAML configuration at a specified path, and applies it to a Pachyderm cluster. It can be used to store cluster configuration in a k8s secret. 

See `example-secret.yaml` for an example k8s secret to configure a single pachd node with an enterprise license and authentication enabled.
