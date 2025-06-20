# 🚀 Pull Request: [Title of Your PR]

## 📋 Summary

This PR introduces/fixes/enhances XYZ functionality...

## 🧾 Related Issue(s)

Closes \#123 ---

## ✅ Changes Made

- Added new endpoint `POST /api/v1/databases/{db_name}/apikeys` for API key generation.
- Refactored `internal/storage` interface for better extensibility.
- Fixed an issue where `DELETE /api/v1/databases/{db_name}` didn't properly clean up SQLite files.

---

## 🧪 Testing Steps

1.  Pull this branch and ensure your `.env` is configured (especially `JWT_SECRET`).
2.  Run the application using `docker-compose up --build` or `air`.
3.  Use your API client (e.g., Postman, curl) to test the new/modified endpoints.
4.  Run all unit tests with `go test ./...`.
5.  For specific changes, detail the exact API calls or scenarios to test.

---

## 📸 Screenshots (if applicable)

```http
# Example Request
POST /api/v1/databases/my_new_db/apikeys HTTP/1.1
Host: localhost:8080
Authorization: Bearer <YOUR_JWT_TOKEN>
Content-Type: application/json

{}
```

```http
# Example Response
HTTP/1.1 200 OK
Content-Type: application/json

{
    "apiKey": "nebula_db_1234567890abcdef"
}
```

---

## 🧠 Notes for Reviewers

- The new API key generation utilizes the existing JWT authentication for user scope.
- Database file cleanup now includes a check to ensure the user owns the database before deletion.
- Consider the impact on rate limiting for newly added endpoints.

---

## 🔒 Checklist

Please confirm the following before submitting:

- [ ] I have read the [Contributing Guide](https://www.google.com/search?q=CONTRIBUTING.md).
- [ ] The code follows the project’s coding standards and the [Go Style Guide](https://www.google.com/search?q=CONTRIBUTING.md%23go-style-guide).
- [ ] Linting passes (`go fmt`, `goimports`).
- [ ] I have added or updated unit/integration tests for the changes.
- [ ] I have updated documentation if needed (e.g., `README.md` if new features are exposed).
- [ ] All new and existing tests pass (`go test ./...`).
- [ ] My changes do **not** include sensitive info like secrets or credentials.
