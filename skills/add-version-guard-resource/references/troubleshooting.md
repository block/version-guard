# Troubleshooting Guide

## Test Failures

**Symptom**: `go test` fails with YAML parsing errors

**Solution**:
- Check YAML syntax in config/resources.yaml
- Verify indentation uses spaces (not tabs)
- Ensure all quotes are closed
- Run: `cat config/resources.yaml | head -50` to inspect

## Missing Dependencies

**Symptom**: Prerequisites check fails

**Solution**:
- Direct user to SETUP.md
- Verify Version Guard generic infrastructure is implemented
- Check you're in correct repository directory

## Non-Standard Schema

**Symptom**: Resource has unusual endoflife.date field semantics

**Solution**:
- Note in config: `schema: {resource}-adapter`
- Inform user: "This resource requires a custom schema adapter"
- Point to: `pkg/eol/endoflife/adapters.go` for implementation
- Refer to EKS adapter as example

## endoflife.date API Down

**Symptom**: `curl` to endoflife.date fails

**Solution**:
- Check internet connectivity
- Try: `curl -I https://endoflife.date` to verify API is up
- Wait and retry if API is temporarily unavailable
