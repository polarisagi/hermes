# Contributing to PolarisAGI Hermes

First off, thank you for considering contributing to PolarisAGI Hermes! It's people like you that make PolarisAGI Hermes such a great tool.

## How Can I Contribute?

### Reporting Bugs

This section guides you through submitting a bug report. Following these guidelines helps maintainers and the community understand your report, reproduce the behavior, and find related reports.

- Use the official Bug Report Issue Template.
- Explain the problem and include additional details to help maintainers reproduce the problem.
- Check if you can reproduce the problem in the latest version.

### Suggesting Enhancements

This section guides you through submitting an enhancement suggestion, including completely new features and minor improvements to existing functionality.

- Use the official Feature Request Issue Template.
- Provide a clear and descriptive title for the issue to identify the suggestion.
- Provide a step-by-step description of the suggested enhancement.
- Explain why this enhancement would be useful to most users.

### Pull Requests

1. Fork the repository and create your branch from `main`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes (`go test ./...`).
5. Make sure your code lints.
6. Issue that pull request!

## Local Development Setup

1. Make sure you have Go 1.22+ installed.
2. Clone the repository: `git clone https://github.com/polarisagi/hermes.git`
3. Navigate to the directory: `cd hermes`
4. Build the binary: `go build -o hermes ./cmd/polarisagi`
5. Run the server: `./hermes`

## Commit Messages

We use the [Conventional Commits](https://www.conventionalcommits.org/) specification for commit messages. For example:
- `feat: add new API endpoint`
- `fix: resolve stream interruption bug`
- `docs: update README with usage examples`
- `chore: update dependencies`

Thank you for your contributions!
