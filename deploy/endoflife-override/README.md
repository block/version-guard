# endoflife.date Local Override

Version Guard uses [endoflife.date](https://endoflife.date) for all EOL lifecycle data. Sometimes upstream PRs take time to merge, leaving gaps in coverage (products return 404 or are missing recent version cycles).

This override mechanism lets you **patch EOL data locally** without waiting for upstream merges.

## How It Works

An nginx container serves local JSON files from `api/` and proxies everything else to the upstream endoflife.date API:

```
Request: GET /api/amazon-aurora-mysql.json
  1. Check local file: deploy/endoflife-override/api/amazon-aurora-mysql.json
  2. If found → serve it (your patched data)
  3. If not found → proxy to https://endoflife.date/api/amazon-aurora-mysql.json
```

## Adding or Patching a Product

1. Fetch the current data (or create new data from a pending PR's Netlify preview):

```bash
# Existing product — fetch and patch
curl -s https://endoflife.date/api/amazon-opensearch.json | python3 -m json.tool > api/amazon-opensearch.json
# Edit the file to add missing cycles

# New product — fetch from PR deploy preview
curl -s https://deploy-preview-9534--endoflife-date.netlify.app/api/amazon-aurora-mysql.json \
  | python3 -m json.tool > api/amazon-aurora-mysql.json
```

2. Add or update the corresponding entry in `manifest.json`. The source URL,
owner, reason, review date, and review due date are required. Reviews may be
scheduled at most 30 days apart.

3. Run the override package tests, then restart docker-compose — no rebuild needed:

```bash
go test ./deploy/endoflife-override
docker compose restart endoflife
```

The validator checks the manifest schema and metadata, one-to-one coverage of
manifest entries and `api/*.json` files, and lifecycle data using the same
validation as the runtime provider. Malformed metadata, missing files, invalid
URLs, and invalid lifecycle data fail validation. An expired review due date is
warn-only so CI continues to run while making the overdue review visible.

`manifest.json` is the machine-readable source of truth for current overrides.
Update it whenever an override is added, reviewed, or removed.

## Configuration

Set `EOL_BASE_URL` to point Version Guard at the local override container:

```yaml
# docker-compose.yaml
environment:
  EOL_BASE_URL: http://endoflife:8080/api
```

When `EOL_BASE_URL` is not set, Version Guard connects directly to `https://endoflife.date/api` (the default).

## Removing Overrides

Once an upstream PR is merged, delete the local JSON file. Nginx will then proxy that product to the upstream API automatically.

Delete its `manifest.json` entry in the same change. Nginx marks local and
upstream responses with authoritative `X-Version-Guard-EOL-Source` headers;
those values flow into snapshot findings and lifecycle source metrics.
