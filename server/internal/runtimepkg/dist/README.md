Generated: the runtime packages personad carries (`go generate ./internal/runtimepkg`, which the Dockerfile and the
deploy / publish scripts run before building), the files a runtime's loader imports (docs/integrations/runtimes.md
"Auto-update"). `index.json` names each package's parts; a part is a gzipped tar of a package's files under one
directory of integrations/ (`<directory>-<hash>.tar.gz`), stored once however many packages take it. Only this README
is committed.
