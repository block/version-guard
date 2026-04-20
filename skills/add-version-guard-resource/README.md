# add-version-guard-resource Skill

**Status**: ✅ Beta
**Version**: 1.0.0
---

## What This Skill Does

Enables AI agents to automatically add new cloud resource types to Version Guard by:
1. Validating endoflife.date API coverage
2. Auto-detecting Wiz CSV schema from examples
3. Generating YAML configuration
4. Running tests
5. Creating properly formatted commits

**Zero manual configuration needed** - the AI agent handles everything autonomously.

---

## Files Created

```
add-version-guard-resource/
├── SKILL.md                           # Main skill instructions (AI agent reads this)
├── SETUP.md                           # Prerequisites and installation
├── examples/
│   ├── elasticache.yaml               # Standard schema example
│   ├── eks.yaml                       # Non-standard schema example
│   └── aurora-pg.yaml                 # Aurora PostgreSQL example
├── references/
│   ├── detailed-examples.md           # Complete workflow examples
│   └── troubleshooting.md             # Common issues and solutions
└── README.md                          # This file
```

---

## Prerequisites (BLOCKING)

Before this skill can be used, **Version Guard Phase 1 must be implemented**:

Required files:
- `config/resources.yaml`
- `pkg/config/loader.go`
- `pkg/inventory/wiz/generic.go`
- `pkg/detector/generic/detector.go`
- `pkg/eol/endoflife/adapters.go`

**Status**: ✅ Phase 1 COMPLETED - Generic infrastructure implemented

---

## Usage Examples

### Example 1: Standard Resource

```
User: "Add OpenSearch support to Version Guard"

AI:
1. Validates endoflife.date has "opensearch" ✅
2. Asks: "What resource ID and Wiz report ID should I use?"
3. User provides:
   - Resource ID: "opensearch"
   - Wiz report ID: "abc12345-..."
4. Generates config with id: opensearch, schema: standard
5. Tests pass ✅
6. Creates commit ✅
7. Reminds user to add to WIZ_REPORT_IDS environment variable
```

### Example 2: Missing Coverage

```
User: "Add Neptune support"

AI:
1. Queries endoflife.date → No "neptune" product found ❌
2. STOPS and informs user:
   "endoflife.date doesn't have coverage for Neptune yet.
    You need to create a PR at https://github.com/endoflife-date/endoflife.date first."
```

---

## Tool Compatibility

This skill works with:
- ✅ Claude Code (auto-discovered from skills/ directory)
- ✅ Goose (consult Goose documentation for skill configuration)
- ✅ Amp (auto-discovered from skills/ directory)
- ✅ Any AI tool supporting Agent Skills specification

**Tool-agnostic design** - no platform-specific features.

---

## References

- **endoflife.date API**: https://endoflife.date/api/all.json
- **Agent Skills Spec**: https://agentskills.io/specification
- **Version Guard Repository**: https://github.com/block/Version-Guard
