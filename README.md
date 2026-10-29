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
