# Troubleshooting Guide

## Test Failures

**Symptom**: `go test` fails with YAML parsing errors

**Solution**:
- Check YAML syntax in pkg/config/defaults/resources.yaml
- Verify indentation uses spaces (not tabs)
- Ensure all quotes are closed
- Run: `cat pkg/config/defaults/resources.yaml | head -50` to inspect

## Missing Dependencies

**Symptom**: Prerequisites check fails

**Solution**:
- Direct user to SETUP.md
- Verify Version Guard generic infrastructure is implemented
- Check you're in correct repository directory

## Non-Standard Schema

**Symptom**: Resource has unusual endoflife.date field semantics
(e.g. `cycle.eol` doesn't mean "true EOL"; the product uses
`deprecatedSupport` instead of `extendedSupport`; the cycle ships only a
subset of the canonical fields).

**Solution**:
- Set `schema: declarative` and add an `eol.lifecycle` block under the
  resource's `eol:` config — declare which upstream cycle field maps
  to each Version Guard boundary, and what status applies in each
  window. **No Go code change is required for the common cases.**
- See `pkg/eol/endoflife/ADAPTERS.md` for the full list of supported
  field names (`support`, `eol`, `extendedSupport`) and actions
  (`extended_support`, `unsupported`, `eol`, …).
- Use `examples/eks.yaml` as a worked example of the declarative
  shape, and the `lambda` block in
  `pkg/config/defaults/resources.yaml` for the deprecated-support
  variant.
- The loader rejects `schema: declarative` without a `lifecycle`
  block (and a `lifecycle` block without `schema: declarative`), so
  YAML typos fail fast at startup.
- A custom Go adapter in `pkg/eol/endoflife/adapters.go` should only
  be needed when the lifecycle DSL can't express the product's
  semantics — that's exceptional now. Check ADAPTERS.md before
  reaching for Go.

## endoflife.date API Down

**Symptom**: `curl` to endoflife.date fails

**Solution**:
- Check internet connectivity
- Try: `curl -I https://endoflife.date` to verify API is up
- Wait and retry if API is temporarily unavailable
