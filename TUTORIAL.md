# Pivot Tutorial

Pivot is a tool designed to take a Kubernetes cluster from a bare state to a fully GitOps-managed environment in a single command. It bootstraps **Gitea** (for git hosting) and **ArgoCD** (for continuous deployment), creating a self-hosted, self-managed infrastructure.

This tutorial will guide you through installing Pivot, bootstrapping your cluster, and making changes using a GitOps workflow.

## Prerequisites

Before you begin, ensure you have the following:

*   A Kubernetes cluster (e.g., Minikube, Kind, or a cloud provider cluster).
*   `kubectl` installed and configured to access your cluster.
*   `git` installed on your local machine.

## 1. Installation

You can install `pivot` using one of the following methods.

### Option A: Download Binary (Recommended)

1.  Visit the [Releases page](https://github.com/hyperspike/pivot/releases) of the Pivot repository.
2.  Download the binary appropriate for your operating system (Linux, macOS) and architecture (amd64, arm64).
3.  Make the binary executable:
    ```bash
    chmod +x pivot
    ```
4.  Move it to a directory in your system's `PATH` (e.g., `/usr/local/bin`):
    ```bash
    sudo mv pivot /usr/local/bin/
    ```

### Option B: Install via Go

If you have Go installed (1.24+), you can install the latest version directly:

```bash
go install github.com/hyperspike/pivot/cmd@latest
```
*Note: Ensure `$GOPATH/bin` is in your system's `PATH`.*

### Option C: Build from Source

1.  Clone the repository:
    ```bash
    git clone https://github.com/hyperspike/pivot.git
    cd pivot
    ```
2.  Build the binary using the Makefile:
    ```bash
    make build
    ```
    Or to build for multiple platforms:
    ```bash
    make cli
    ```
3.  The `pivot` binary will be created in the current directory.

## 2. Start Pivoting

Once installed, you can start the bootstrapping process.

1.  **Verify Context**: Ensure your `kubectl` context is set to the target cluster.
    ```bash
    kubectl config current-context
    ```

2.  **Run Pivot**: Execute the run command to start bootstrapping.
    ```bash
    pivot run
    ```

    **What happens next?**
    *   **Repo Generation**: Pivot creates a local directory named `infra`. This is a git repository containing Kubernetes manifests for Gitea, ArgoCD, Cert-Manager, and other components.
    *   **Apply Manifests**: It applies these manifests to your cluster.
    *   **Push to Gitea**: It pushes the `infra` repository to the newly created Gitea instance running inside your cluster.
    *   **ArgoCD Sync**: It wires up ArgoCD to watch this repository.

    **Useful Flags:**
    *   `-n, --namespace string`: Specify a namespace (defaults to context default).
    *   `-p, --password string`: Set a custom password for the Gitea `pivot` user. If not set, one is generated.
    *   `-r, --remote string`: Set the remote repository domain (default "git.local.net").
    *   `-d, --dry-run`: Simulate the process without making changes to the cluster.

## 3. Post-Installation: Making Changes

After `pivot run` completes, your cluster's state is defined by the `infra` git repository. To make changes (like adding applications or modifying configurations), you must follow a GitOps workflow.

### Step 1: Edit Configuration
Navigate to the generated `infra` directory and make your changes.

```bash
cd infra
# Example: Edit the Gitea configuration
vim gitea/gitea.yaml
```

### Step 2: Commit Changes
Commit your changes to the local git repository.

```bash
git add .
git commit -m "Update Gitea configuration"
```

### Step 3: Open Proxy
Since your Gitea instance is running inside the cluster and might not be exposed externally yet, you need to use `pivot` to open a tunnel.

Open a **new terminal window** and run:
```bash
pivot proxy
```
*Leave this command running.*

### Step 4: Authenticate
If you didn't manually set a password during the run step, retrieve the generated password:

```bash
pivot password
```
*Copy the output password.*

### Step 5: Push Changes
Push your committed changes to the `local` remote (which is tunneled to your cluster via the proxy).

```bash
git push local main
```
*   **Username**: `pivot` (or the user you specified with `-u`)
*   **Password**: The password from Step 4.

Once pushed, ArgoCD will automatically detect the changes in the repository and sync them to your cluster.

## Troubleshooting

*   **View Help**:
    ```bash
    pivot --help
    pivot run --help
    ```
*   **Check Pods**: If something fails, check the status of the pods in the current namespace.
    ```bash
    kubectl get pods
    ```
*   **Logs**: View logs of specific pods for debugging.
    ```bash
    kubectl logs <pod-name>
    ```
