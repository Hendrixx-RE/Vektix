# REST API Guide

## Endpoints

### Query Endpoint

- **Path**: `/api/v1/query`
- **Method**: `POST`
- **Headers**: `Authorization: Bearer <jwt-token>`
- **Request Body**:
```json
{
  "query": "authentication claims",
  "scope": "pkg/auth",
  "limit": 5
}
```

### Auth Endpoint

- **Path**: `/api/v1/auth`
- **Method**: `POST`
- **Request Body**:
```json
{
  "username": "admin",
  "password": "secretpassword"
}
```

## Error Codes

- `401 Unauthorized`: Missing or invalid bearer token.
- `429 Too Many Requests`: Rate limit exceeded.
- `500 Internal Error`: Backend unhandled exception.
