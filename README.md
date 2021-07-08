## config-pod

config-pod is a Docker image which takes YAML configuration at a specified path, and applies it to a Pachyderm cluster. It can be used to store cluster configuration in a k8s secret. 

See the `examples` directory for a sample Job which runs the configuration pod, and two sample Secrets:

- `basic-secret.yaml` provides a simplified case which activates enterprise features and authentication
- `full-secret.yaml` provides an example of all the configuration keys` 
