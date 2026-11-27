# nginx · access layer (additive)

Corresponds to the `deploy` deployment phase.

## Why "additive" rather than "replacing"

On delivery sites the management node **may already have Nginx running other sites** (the current state of this host: `argus` occupies the default virtual host on 80/443, reverse-proxying to 8080). Overwriting `nginx.conf` directly would break other people's sites.

Therefore this role's rules are:

| Rule | Reason |
| --- | --- |
| Write only the single file `conf.d/winter.conf` | Do not touch `nginx.conf` or anyone else's conf |
| **Do not declare `default_server`** | The default site on the same port remains with the existing application |
| Route by `server_name` | Only requests for this platform's domain enter our block |
| Use `reload` rather than `restart` | Do not drop existing connections; other services are unaffected |
| Keep certificates and static directory separate (`/var/lib/winter/...`) | Do not mix with `/var/www` |

## Variables

| Variable | Default | Description |
| --- | --- | --- |
| `nginx.server_name` | `{{ site.domain }}` | Certificate CN and site domain, default `winter.local` |
| `nginx.https_port` | `{{ site.https_port }}` | Default 443 |
| `nginx.doc_root` | `/var/lib/winter/www` | Static assets |
| `nginx.tls_dir` | `/var/lib/winter/tls` | Self-signed certificate (generated on first run, left untouched if present) |
| `nginx.api_upstream` | `http://127.0.0.1:8081` | Server address; **8080 may be occupied by another service on the same host** |

## High availability upstreams

When `./deploy.sh application-ha` runs, the site gains `upstream winter_api` and `upstream winter_harbor`,
built from the `[winter-server]` and `[harbor]` inventory groups. `/api/v1/` continues to reach the
Server pool, while `/api/v2.0/`, `/v2/`, `/service/` and `/harbor/` are routed to Harbor. Without those
references the original single `nginx.api_upstream` is kept, so existing deployments are unchanged.

## Verification

```bash
nginx -t
curl -k -H "Host: winter.local" https://127.0.0.1/healthz      # → ok
curl -k -H "Host: winter.local" https://127.0.0.1/             # → placeholder page
```

## Known trade-offs

- A self-signed certificate is only suitable for internal/test use; for production delivery switch to an enterprise CA or ACME, then simply `reload` after changing the two files under `nginx.tls_dir`.
- `/api/` returns 502 before the Server is deployed; this is expected, not a configuration error.
- If the customer already has an LB, this role's 80/443 can be disabled entirely (`--skip-tags nginx`), using only the LB pointing to the Server.
