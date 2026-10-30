# harbor-roboteer

Service to run next to Harbor to map OIDC to robot accounts

## Endpoints

### `POST /token`

Create a new Harbor `robot$...` account based on the JWT.

```bash
curl -X POST https://harbor-roboteer.example.com/token \
  -H "Authorization: Bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN"
```

Response `200 OK`:

```json
{
  "robot": {
    "username": "robot$github-actions+my-repo",
    "password": "..."
  },
  "deleteToken": "..."
}
```

### `DELETE /token`

Clean up robot account at the end of workflow.

```bash
curl -X DELETE https://harbor-roboteer.example.com/token \
  --url-query username='robot$github-actions+my-repo' \
  -H "Authorization: Token $DELETE_TOKEN"
```

Response `201 Created`

## How it works

```mermaid
sequenceDiagram
    actor GitHub Action workflow

    harbor-roboteer->>GitHub OIDC IdP: get jwks
    GitHub OIDC IdP-->>harbor-roboteer:
    Note over harbor-roboteer,GitHub OIDC IdP: preparation

    activate GitHub Action workflow
    GitHub Action workflow->>GitHub OIDC IdP: request OIDC token
    GitHub OIDC IdP-->>GitHub Action workflow:

    GitHub Action workflow->>+harbor-roboteer: request Harbor robot account
    harbor-roboteer->>+Harbor: create robot account
    Harbor-->>harbor-roboteer:
    harbor-roboteer-->>-GitHub Action workflow:

    GitHub Action workflow->>Harbor: docker login with robot account
    Harbor-->>GitHub Action workflow:
    deactivate GitHub Action workflow

    loop
        harbor-roboteer->>Harbor: get robot accounts
        Harbor-->>harbor-roboteer:
        harbor-roboteer->>Harbor: delete expired robot accounts
        Harbor-->>harbor-roboteer:
    end
```

## Development

### mise

Mise (aka "mise-en-place") is used to manage tooling to make sure we have the
same version of everything.

Recommended to set up automatic activation:

```bash
# for bash
echo 'eval "$(mise activate bash)"' >> ~/.bashrc
# for zsh
echo 'eval "$(mise activate zsh)"' >> ~/.zshrc
# for fish
echo 'mise activate fish | source' >> ~/.config/fish/config.fish
```

### pre-commit via hk

Recommended to use our pre-commit hooks, provided via [hk](https://hk.jdx.dev/)

```bash
# if using Git 2.54+ (check with "git --version"), then it's recommended to install globally
mise run pre-commit:install:global

# otherwise:
mise run pre-commit:install
```

## Prior art

| Link | Purpose | Comparison
| --- | --- | ---
| <https://github.com/dvob/hati> | Same as this project. Create robot accounts from JWTs. | Missing robot account cleanup, leading to build up of expired robot accounts.
| <https://github.com/corelyr-oss/vault-plugin-secrets-harbor> | HashiCorp Vault/OpenBao plugin to dynamically create robot accounts | Requires HashiCorp Vault or OpenBao
| <https://github.com/clique-it-tech/vault-plugin-secrets-harbor> | HashiCorp Vault/OpenBao plugin to dynamically create robot accounts | Requires HashiCorp Vault or OpenBao
| <https://github.com/AetherizeGmbH/harbor-workload-identity-bridge> | Kubelet credential provider plugin to replace need for `imagePullSecrets` in Kubernetes | Different use-case
| <https://github.com/gawsoftpl/credential-provider-kubernetes> | Kubelet credential provider plugin, but only the kubelet side. Expects a broker endpoint | Different use-case
| <https://container-registry.com/posts/keyless-harbor-workload-identity-federation/> | 8gears' blog post about their commercial feature in 8gcr. Map JWTs to existing robot accounts, inside the `docker login` flow and as a kubelet credential provider | Proprietary
