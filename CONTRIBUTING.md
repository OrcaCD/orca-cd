# Contributing

Any contribution is greatly appreciated. You don't need to be a developer to contribute. You can help by translating the app, reporting issues, or simply sharing your ideas for new features.

If you have any questions, please do not hesitate to contact us.

Read our [Code of Conduct](CODE_OF_CONDUCT.md) to keep our community approachable and respectable.

## Security

If you would like to report a security vulnerability, please take a look at [SECURITY.md](SECURITY.md)

## Translations

_Instructions will be added later._

## Code contributions

We welcome code contributions and encourage clear, well-documented changes that include appropriate tests.
When introducing a new feature, please ensure you add relevant tests.
For breaking changes or major new features, open an issue beforehand to discuss your proposal with the team.

### Required tools

**Backend:**

| Tool                                                             | Version | Purpose                     |
| ---------------------------------------------------------------- | ------- | --------------------------- |
| [Go](https://go.dev/dl/)                                         | 1.26+   | Language runtime            |
| [golangci-lint](https://golangci-lint.run/welcome/install/)      | v2.11+  | Linting                     |
| [just](https://just.systems/man/en/packages.html)                | latest  | Task runner                 |
| [buf](https://buf.build/docs/cli/installation/)                  | latest  | Protobuf tooling            |
| [protoc-gen-go](https://protobuf.dev/reference/go/go-generated/) | latest  | Go protobuf code generation |

**Frontend:**

| Tool                           | Version           | Purpose         |
| ------------------------------ | ----------------- | --------------- |
| [Node.js](https://nodejs.org/) | 24.x              | Runtime         |
| [npm](https://docs.npmjs.com/) | bundled with Node | Package manager |

### Setting up the development environment

1. Install all required tools listed above.
2. Clone the repository and navigate to the project directory.
3. For the frontend, navigate to the `frontend/` directory and run `npm ci` to install dependencies.
4. Run the frontend development server with `node --run dev` from the `frontend/` directory.
5. Run the backend services with `just run-hub` and `just run-agent` in the `backend/` directory.
6. Access the app at `http://localhost:3000` and the hub API at `http://localhost:8080`.

### Running locally with Docker

A Docker Compose file is provided for running the full stack locally:

```sh
docker compose -f docker-compose.dev.yaml up --build
```

The hub service will be available at `http://localhost:8080`.
