# <div align="center"><img src="https://raw.githubusercontent.com/OrcaCD/orca-cd/f6fb48965df90a57a2b181e9e2b359653ae5c6b9/frontend/public/assets/logo-dark-256.png" width="100"/> <br>OrcaCD</div>

<div align="center">
Simple GitOps for Docker<br><br>

<a href="https://github.com/OrcaCD/orca-cd/blob/main/LICENSE"><img alt="GitHub License" src="https://img.shields.io/github/license/OrcaCD/orca-cd"></a>
<a href="https://github.com/OrcaCD/orca-cd/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/OrcaCD/orca-cd"></a>
<a href="https://github.com/OrcaCD/orca-cd/issues"><img alt="GitHub Issues" src="https://img.shields.io/github/issues/OrcaCD/orca-cd"></a>
</div>

OrcaCD is a simple GitOps tool for Docker. It allows you to deploy your applications using Git as the source of truth. With OrcaCD, you can easily manage your Docker containers and keep them in sync with your Git repository.

> [!WARNING]
> OrcaCD is in early development and not yet production-ready. There are no stable releases. Expect breaking changes at any time.

## Getting Started

You can get started with OrcaCD by following the guide in the [documentation](https://orcacd.dev).

### Restricted deployment directories

The agent checks deployment paths for symlinks before deploying. A container can create a symlink in a writable bind mount and use it as a mount source on a later deployment to access paths outside its application directory. If directory permissions prevent the agent from inspecting a path, the deployment is rejected.

Prefer granting the agent search permission on the affected parent directories. If that is impractical, you can uncomment the `cap_add` block for `DAC_READ_SEARCH` in the agent's Compose configuration and recreate the agent container. This optional Linux capability bypasses file read and directory read/search permissions for **all files accessible to the agent**, not just the deployments directory. It does not bypass write permissions. It is disabled by default; symlink checks remain enabled.

## Contribute

You're very welcome to contribute to OrcaCD! Please follow the [contribution guide](https://github.com/OrcaCD/orca-cd/blob/main/CONTRIBUTING.md) to get started. If you wish to contribute to the documentation head over to the [repository](https://github.com/OrcaCD/docs).
