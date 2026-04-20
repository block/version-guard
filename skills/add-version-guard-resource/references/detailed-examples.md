# Detailed Examples

## Example 1: Aurora PostgreSQL (Standard Schema)

**User**: "Add Aurora PostgreSQL support to Version Guard"

**Agent workflow**:
1. Validates endoflife.date has `amazon-aurora-postgresql` ✅
2. Asks: "What resource ID and Wiz report ID should I use?"
3. User provides:
   - Resource ID: `aurora-postgresql`
   - Wiz report ID: `your-wiz-report-id-here`
4. Detects schema from existing `aurora.csv` fixture
5. Generates config with `id: aurora-postgresql` and `schema: standard`
6. Appends to config/resources.yaml (NO report_id_env field)
7. Runs `go test ./pkg/detector/generic -v` ✅
8. Creates commit ✅
9. Reminds user to add to WIZ_REPORT_IDS: `{"aurora-postgresql":"your-wiz-report-id-here"}`

## Example 2: RDS MySQL (Standard Schema)

**User**: "Add RDS MySQL support"

**Agent workflow**:
1. Validates endoflife.date has `amazon-rds-mysql` ✅
2. Asks: "What resource ID and Wiz report ID should I use?"
3. User provides:
   - Resource ID: `rds-mysql`
   - Wiz report ID: `abc12345-...`
4. Detects schema similar to Aurora
5. Generates config with `id: rds-mysql` and `schema: standard`
6. Tests pass ✅
7. Commits changes ✅
8. Reminds user to update WIZ_REPORT_IDS environment variable

## Example 3: Neptune (No Coverage)

**User**: "Add Neptune support"

**Agent workflow**:
1. Queries endoflife.date → No `neptune` or `amazon-neptune` product found ❌
2. **STOPS** and informs user:
   ```
   endoflife.date doesn't have coverage for Neptune yet.
   You need to create a PR at https://github.com/endoflife-date/endoflife.date first.
   See example: https://github.com/endoflife-date/endoflife.date/pull/9534
   ```
