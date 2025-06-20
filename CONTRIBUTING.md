# Contributing to Nebula Backend

We welcome and appreciate your contributions to the Nebula project\! Whether it's reporting a bug, suggesting a new feature, improving documentation, or submitting code, your help makes Nebula better for everyone.

Please take a moment to review this guide before making a contribution.

## Table of Contents

- [Code of Conduct](https://www.google.com/search?q=%23code-of-conduct)
- [How Can I Contribute?](https://www.google.com/search?q=%23how-can-i-contribute)
  - [Reporting Bugs](https://www.google.com/search?q=%23reporting-bugs)
  - [Suggesting Enhancements](https://www.google.com/search?q=%23suggesting-enhancements)
  - [Contributing Code](https://www.google.com/search?q=%23contributing-code)
- [Getting Started with Development](https://www.google.com/search?q=%23getting-started-with-development)
  - [Prerequisites](https://www.google.com/search?q=%23prerequisites)
  - [Forking the Repository](https://www.google.com/search?q=%23forking-the-repository)
  - [Cloning Your Fork](https://www.google.com/search?q=%23cloning-your-fork)
  - [Setting Up Your Environment](https://www.google.com/search?q=%23setting-up-your-environment)
  - [Running the Application](https://www.google.com/search?q=%23running-the-application)
  - [Running Tests](https://www.google.com/search?q=%23running-tests)
- [Submitting a Pull Request](https://www.google.com/search?q=%23submitting-a-pull-request)
- [Style Guides](https://www.google.com/search?q=%23style-guides)
  - [Go Style Guide](https://www.google.com/search?q=%23go-style-guide)
  - [Commit Message Guidelines](https://www.google.com/search?q=%23commit-message-guidelines)
- [Licensing](https://www.google.com/search?q=%23licensing)

---

## Code of Conduct

Please note that this project is released with a [Contributor Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct.html). By participating in this project, you agree to abide by its terms.

---

## How Can I Contribute?

There are several ways you can contribute to Nebula:

### Reporting Bugs

If you find a bug, please help us by reporting it. Before opening a new issue, please check if a similar issue already exists.

To report a bug:

1.  Go to the [Issues](https://www.google.com/search?q=https://github.com/Annany2002/nebula-backend/issues) section.
2.  Click on **"New issue"**.
3.  Select the **"Bug report"** template or use this [Bug Report Template](https://www.google.com/search?q=.github/ISSUE_TEMPLATE/bug_report.yaml).
4.  Provide a clear and concise description of the bug, including steps to reproduce it, expected behavior, and actual behavior.
5.  Include any relevant logs or error messages.

### Suggesting Enhancements

Do you have an idea for a new feature or an improvement to existing functionality? We'd love to hear it\!

To suggest an enhancement:

1.  Go to the [Issues](https://www.google.com/search?q=https://github.com/Annany2002/nebula-backend/issues) section.
2.  Click on **"New issue"**.
3.  Select the **"Feature request"** template or use this [Feature Request Template](https://www.google.com/search?q=.github/ISSUE_TEMPLATE/feature_request.yaml).
4.  Clearly describe the enhancement, its use case, and why you think it would be beneficial.

### Contributing Code

We welcome code contributions\! If you're looking to contribute code, please follow the steps outlined in the [Getting Started with Development](https://www.google.com/search?q=%23getting-started-with-development) and [Submitting a Pull Request](https://www.google.com/search?q=%23submitting-a-pull-request) sections.

---

## Getting Started with Development

To get Nebula running on your local machine for development:

### Prerequisites

- **Go:** Version 1.18+ recommended.
- **Docker and Docker Compose:** For building and running the application.

### Forking the Repository

1.  Navigate to the [Nebula Backend repository](https://www.google.com/search?q=https://github.com/Annany2002/nebula-backend) on GitHub.
2.  Click the **"Fork"** button in the top right corner. This will create a copy of the repository under your GitHub account.

### Cloning Your Fork

Once you have forked the repository, clone your fork to your local machine:

```bash
git clone https://github.com/<YOUR_USERNAME>/nebula-backend.git
cd nebula-backend
```

Replace `<YOUR_USERNAME>` with your actual GitHub username.

### Setting Up Your Environment

1.  **Create and Configure the `.env` file:**
    Copy the example environment file:

    ```bash
    cp .env.example .env
    ```

    Now, **edit the `.env` file**:

    - **CRITICAL:** Set a strong, unique `JWT_SECRET`. This is essential for security.
    - Set `ALLOWED_ORIGINS` (a space-separated list of frontend origins for CORS, e.g., `"http://localhost:3000 http://your-frontend.com"`).
    - Adjust `SERVER_PORT`, `JWT_EXPIRATION_HOURS`, etc., if needed.
    - Ensure `.env` is listed in your `.gitignore` to prevent it from being committed.

2.  **Build the Docker Image:**
    Build the Docker image for the Nebula Backend:

    ```bash
    docker build -t nebula-backend .
    ```

### Running the Application

To start the application using Docker Compose (once `docker-compose.yml` is fully implemented for development):

```bash
docker-compose up --build
```

This command will:

- Build the `nebula-backend` image.
- Start the `nebula-backend` service (typically on port `8080`) and any other services defined in `docker-compose.yml` (e.g., `nginx`).

For local development with hot-reloading, you can also use `air`:

```bash
air # Starts the local development server with hot-reloading
```

### Running Tests

Nebula uses Go's standard `testing` package along with `net/http/httptest` and `github.com/stretchr/testify/assert`.

To run all tests:

```bash
go test ./...
```

To run tests for a specific package, for example, the `auth` package:

```bash
go test ./internal/auth
```

---

## Submitting a Pull Request

When you're ready to submit your code changes:

1.  **Create a New Branch:**
    It's good practice to create a new branch for your changes:

    ```bash
    git checkout -b feat/your-feature-name # for features
    git checkout -b fix/your-bug-fix # for bug fixes
    ```

2.  **Make Your Changes:**
    Implement your feature or fix the bug. Remember to follow the [Style Guides](https://www.google.com/search?q=%23style-guides).

3.  **Test Your Changes:**
    Ensure your changes work as expected and haven't introduced any regressions by running tests.

4.  **Commit Your Changes:**
    Write clear, concise commit messages. Follow the [Commit Message Guidelines](https://www.google.com/search?q=%23commit-message-guidelines).

    ```bash
    git add .
    git commit -m "feat: Add new API endpoint for X"
    ```

5.  **Push Your Branch:**

    ```bash
    git push origin feat/your-feature-name
    ```

6.  **Open a Pull Request (PR):**

    - Go to your forked repository on GitHub.
    - You'll see a banner suggesting you open a pull request. Click on **"Compare & pull request"**.
    - Alternatively, go to the original [Nebula Backend repository](https://www.google.com/search?q=https://github.com/Annany2002/nebula-backend) and you'll see your branch listed there.
    - Fill out the pull request template: use this [Pull Request Template](https://www.google.com/search?q=.github/PULL_REQUEST_TEMPLATE.md).
    - Provide a clear title and description of your changes.
    - Reference any related issues (e.g., `Closes #123`).

We will review your pull request as soon as possible and provide feedback.

---

## Style Guides

### Go Style Guide

- Follow the standard [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).
- Use `go fmt` to format your code.
- Organize imports with `goimports`.
- Write clear and concise comments where necessary, especially for exported functions and types.

### Commit Message Guidelines

We follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) specification for commit messages. This helps with automatic changelog generation and understanding the purpose of each commit.

Examples:

- `feat: Add user registration endpoint`
- `fix: Correct typo in error message`
- `docs: Update README with setup instructions`
- `chore: Upgrade Gin dependency to v1.9.0`
- `test: Add unit tests for database management`
- `refactor: Centralize error handling logic`

---

## Licensing

By contributing to Nebula, you agree that your contributions will be licensed under the [MIT License](https://www.google.com/search?q=LICENSE).

---
