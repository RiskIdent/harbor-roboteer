# API docs

The API documentation page, rendered by [Redoc](https://github.com/Redocly/redoc)
and served at `/docs/`.

`redoc.standalone.js` is vendored from the `redoc` npm package (MIT, see
`LICENSE.redoc`), so that the docs work without internet access.
The version is in the header comment at the top of `redoc.standalone.js`.

`index.html` is written for Redoc 3.x, which is an ES module
(`<script type="module">`) and sends anonymous usage data to Redocly
unless `disable-telemetry="true"` is set on the `<redoc>` tag.
Redoc 2.x is a classic script and would need `index.html` changed back.

This README is not embedded in the binary,
see the `go:embed` line in `../docs.go`.

## Updating Redoc

Run from anywhere in the repo, with the new version number:

```bash
mise run docs:update-redoc <version>
```

The task, defined in `mise.toml`, downloads the package from the npm registry,
verifies it against the registry's integrity hash,
and copies the files into this directory.
Then run the tests.
