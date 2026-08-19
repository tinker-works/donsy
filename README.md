# donsy

`donsy` is the go-merge daemon and host. It owns daemon state and serves the API used by go-merge clients.

## Verification

The repository keeps each verification step separate:

```text
make fmt-check
make tidy-check
make test
make cover
make vet
make lint
make build
```

`make lint` runs the pinned `golangci-lint` release in Docker, so no host linter installation is required.
