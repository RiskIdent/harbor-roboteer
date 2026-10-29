# harbor-roboteer
Service to run next to Harbor to map OIDC to robot accounts

## Endpoints

### `POST /token`

- Authorization: `Bearer {jwt}` header
- Request body: _none_
- Response: `200 OK`
- Response body:

```json
{
  "robot": {
    "username": "...",
    "password": "..."
  },
  "deleteToken": "..."
}
```

### `DELETE /token/:username`

- Authorization: `Token {deleteToken}` header
- Request body: _none_
- Response: `201 Created`
- Response body: _none_

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
