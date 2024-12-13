# Pivot

pivot from cluster-bootstraping to GitOps, Self-hosted, and self-managed, with a single command. Using Gitea and ArgoCD.

## Usage

```bash
$ pivot --help
Pivot is a tool for pivoting from bootstrap to GitOps

Usage:
  pivot [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  run         start pivoting

Flags:
  -h, --help   help for pivot

Use "pivot [command] --help" for more information about a command.
```

```bash
$ pivot run --help
start pivoting

Usage:
  pivot run [flags]

Flags:
  -c, --context string     context [env PIVOT_CONTEXT]
  -d, --dry-run            dry run
  -h, --help               help for run
  -n, --namespace string   namespace [env PIVOT_NAMESPACE] (default "pivot")
  -p, --password string    remote password [env PIVOT_PASSWD]
  -r, --remote string      remote repository [env PIVOT_REMOTE] (default "git.local.net")
  -u, --user string        remote user [env PIVOT_USER] (default "pivot")

```

## How it works

Pivot builds a local `infra' repository with all the necessary files to bootstrap a Gitea Instance, and ArgoCD.

It then applies the manifests to the cluster, and pushes the `infra` repository to the remote repository.

Finally it wires up the now cluster local `infra` repository to ArgoCD for continuous deployment.

## Installation

```bash
$ go install github.com/hyperspike/pivot/cmd@latest
```
