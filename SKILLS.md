# AI Skills for Version Guard

AI agent skills automate complex workflows in Version Guard, enabling any AI agent (Claude Code, Goose, Amp, etc.) to autonomously perform tasks like adding new cloud resource types, managing EOL configurations, and more.

## Overview

### What are AI Skills?

AI skills are structured instruction sets that teach AI agents how to perform specific tasks autonomously. They follow the [Agent Skills standard](https://github.com/agent-skills/specification) with:

- **YAML Frontmatter**: Metadata (name, description, version, status)
- **Tool Allowlist**: Explicit list of permitted operations
- **Markdown Instructions**: Clear, step-by-step guidance for the AI
- **Agent-Agnostic Design**: Works across platforms (Claude Code, Goose, Amp)

### Why Use AI Skills?

**Benefits:**
- **Automation**: Complex multi-step workflows executed autonomously
- **Consistency**: Standardized processes reduce human error
- **Knowledge Sharing**: Codified expertise available to all team members
- **Time Savings**: Hours of manual work reduced to minutes
- **Platform Agnostic**: Same skill works across different AI agents

**Use Cases:**
- Adding new cloud resource types to Version Guard
- Generating EOL configuration from endoflife.date API
- Auto-detecting Wiz CSV schemas
- Running tests and creating properly formatted commits

---

## Available Skills

### add-version-guard-resource

**Status**: Beta (v1.0.0)
**Location**: `skills/add-version-guard-resource/`
**Supports**: Claude Code, Goose, Amp, any Agent Skills-compliant platform

**Purpose**: Autonomously add new cloud resource types to Version Guard by querying EOL data, detecting Wiz schemas, and generating configuration.

**What it does:**
1. ✅ Validates product has EOL data on endoflife.date
2. 📝 Gathers required inputs (resource ID, Wiz report ID, display name)
3. 🔍 Auto-detects Wiz CSV schema from existing test fixtures
4. ⚙️ Checks if EOL adapter customization is needed
5. 📄 Generates `config/resources.yaml` entry
6. 🧪 Runs tests to verify configuration works
7. 📦 Creates properly formatted git commit

**Prerequisites:**
- Version Guard generic infrastructure implemented (Phase 1)
- Tools: curl, git, go, make
- Wiz credentials not required (uses example fixtures)

**Documentation:**
- [Detailed README](skills/add-version-guard-resource/README.md)
- [Setup Guide](skills/add-version-guard-resource/SETUP.md)
- [Example Configurations](skills/add-version-guard-resource/examples/)
- [Troubleshooting](skills/add-version-guard-resource/references/troubleshooting.md)

---

## Installation

### For Claude Code

Skills are automatically discovered from the `skills/` directory when working in the Version Guard OSS repository.

**Setup:**
```bash
# Clone the Version Guard OSS repository
git clone https://github.com/block/Version-Guard.git
cd Version-Guard

# Verify skill is present
ls skills/add-version-guard-resource/SKILL.md

# Skills are auto-discovered - no additional setup needed
```

**Verification:**
In a Claude Code conversation:
```
Do we have a skill for adding resources to Version Guard?
```

Claude should respond mentioning the `add-version-guard-resource` skill.

---

### For Goose

Configure Goose to discover skills from the Version Guard repository.

**Setup:**
```bash
# From Version Guard OSS repo root
cd ~/Version-Guard

# Goose will discover skills from the skills/ directory
# Consult Goose documentation for skill configuration
```

**Usage:**
```bash
# In Goose session, skills are auto-discovered from the project
goose "Add OpenSearch to Version Guard"
```

---

### For Amp

Amp auto-discovers skills from the project's directory structure.

**Setup:**
```bash
# Clone and navigate to Version Guard
git clone https://github.com/block/Version-Guard.git
cd Version-Guard

# Verify skill exists
ls skills/add-version-guard-resource/SKILL.md

# Scan for skills (optional)
amp scan skills
```

**Verification:**
```bash
# List available skills
amp skills list
```

Should show `add-version-guard-resource` in the output.

---

### For Other AI Platforms

Any AI agent that supports the Agent Skills standard can use Version Guard skills.

**Generic Setup:**
1. Clone the Version Guard repository
2. Consult your AI platform's documentation for skill discovery
3. Skills may auto-discover from `skills/` or require copying to a specific directory

**Common Patterns:**
```bash
# Auto-discovery (most platforms)
cd Version-Guard
# Skills discovered from skills/ directory

# Manual installation (if required)
cp -r skills/add-version-guard-resource ~/.config/your-agent/skills/
```

---

## Usage Examples

### Example 1: Adding Amazon OpenSearch

**Request:**
```
Use the add-version-guard-resource skill to add OpenSearch support
```

**What the AI agent does:**

1. **Validates EOL data** - Queries endoflife.date API:
   ```
   curl https://endoflife.date/api/opensearch.json
   ```

2. **Gathers inputs** - Asks for:
   - Resource ID: `opensearch`
   - Display name: `OpenSearch`
   - Wiz report ID: `wiz#report#abc123`

3. **Detects schema** - Examines existing Wiz fixtures:
   ```
   pkg/inventory/wiz/testdata/elasticache-redis.csv
   ```

4. **Checks adapters** - Verifies if EOL data needs custom parsing

5. **Generates config** - Adds to `config/resources.yaml`:
   ```yaml
   - id: opensearch
     name: OpenSearch
     wiz_report_id: "wiz#report#abc123"
     eol_product: opensearch
     version_field: EngineVersion
     name_field: ClusterName
     schema_type: standard
   ```

6. **Runs tests** - Executes:
   ```bash
   go test ./pkg/detector/generic/...
   go test ./pkg/inventory/wiz/...
   ```

7. **Creates commit**:
   ```
   Add OpenSearch resource support

   - Added OpenSearch to config/resources.yaml
   - EOL product: opensearch
   - Wiz report: wiz#report#abc123
   - Uses standard schema (EngineVersion, ClusterName)
   ```

**Time saved**: ~30-45 minutes of manual work reduced to 2-3 minutes

---

### Example 2: Adding Amazon Aurora PostgreSQL

**Request:**
```
Add Aurora PostgreSQL to Version Guard
```

**What's different:**

Aurora uses a **non-standard schema** with separate major/minor version fields:
- `EngineVersion` = `16.3`
- `EngineMajorVersion` = `16`

The skill detects this by examining existing Aurora MySQL fixtures and prompts:
```
Aurora uses non-standard schema with EngineMajorVersion.
Should we use the standard version_field or custom extraction?

Options:
1. Use EngineVersion (standard) - recommended
2. Use custom extraction with EngineMajorVersion
```

If you choose option 1, the generated config uses:
```yaml
version_field: EngineVersion  # Uses 16.3 format
```

The skill also detects that Aurora PostgreSQL needs a custom EOL adapter:
```
endoflife.date uses product ID "amazon-rds-postgresql"
but Wiz data contains "aurora-postgresql".

Created adapter in pkg/eol/endoflife/adapters.go:
- Handles "aurora-postgresql" → "amazon-rds-postgresql" mapping
```

**Time saved**: ~1-2 hours (including schema analysis and adapter creation)

---

### Example 3: Adding EKS (Kubernetes)

**Request:**
```
Enable EKS detection in Version Guard
```

**What's different:**

EKS uses a completely different Wiz schema (not AWS RDS):
- Version field: `K8sVersion` (not `EngineVersion`)
- Name field: `Name` (not `ClusterName`)

The skill auto-detects this by examining `pkg/inventory/wiz/testdata/eks.csv`:
```csv
Name,K8sVersion,Region
my-eks-cluster,1.28,us-east-1
```

Generated configuration:
```yaml
- id: eks
  name: Amazon EKS
  wiz_report_id: "wiz#report#eks123"
  eol_product: amazon-eks
  version_field: K8sVersion
  name_field: Name
  schema_type: non_standard
```

**Time saved**: ~45-60 minutes (including schema investigation)

---

## Skill Development

Want to create your own Version Guard skill? Here's how:

### 1. Create Skill Directory

```bash
mkdir -p skills/your-skill-name/examples
mkdir -p skills/your-skill-name/references
```

### 2. Create SKILL.md

**Required structure:**
```markdown
---
name: your-skill-name
description: Brief description of what the skill does
version: 1.0.0
status: beta
tool_allowlist:
  - Read
  - Write
  - Edit
  - Bash(command_pattern:*)
  - WebFetch(domain:example.com)
---

# Your Skill Name

## Overview
[What this skill does]

## Prerequisites
[What's required before using the skill]

## Workflow

### Step 1: [First step]
[Detailed instructions for AI]

### Step 2: [Second step]
[More instructions]

...

## Success Criteria
[How to verify the skill completed successfully]
```

### 3. Add Supporting Files

**README.md**: User-facing overview
```markdown
# Your Skill Name

Quick reference for users. Keep it concise.
```

**SETUP.md**: Installation and prerequisites
```markdown
# Setup Guide

Prerequisites, installation steps, verification.
```

**examples/**: Example configurations or outputs
```
examples/
  example1.yaml
  example2.yaml
```

**references/**: Detailed documentation
```
references/
  troubleshooting.md
  detailed-examples.md
```

### 4. Test Your Skill

```bash
# Verify YAML frontmatter is valid
head -20 skills/your-skill-name/SKILL.md

# Test with Claude Code
claude "Use the your-skill-name skill to [task]"

# Test with Goose (consult Goose documentation for skill setup)
goose "Use the your-skill-name skill to [task]"
```

### 5. Document Tool Allowlist

**Only include tools the skill actually needs:**

```yaml
tool_allowlist:
  - Read                         # Reading files
  - Write                        # Creating new files
  - Edit                         # Modifying existing files
  - Glob(pattern:*.yaml)         # Finding YAML files
  - Grep(pattern:*)              # Searching code
  - Bash(go test:*)              # Running tests
  - Bash(git add:*)              # Git operations
  - Bash(curl:*)                 # API calls
  - WebFetch(domain:api.example.com)  # Web requests
```

**Don't use wildcards** - be specific about what's allowed.

---

## Troubleshooting

### Skill Not Found

**Symptom**: AI agent doesn't recognize the skill

**Diagnostic:**
```bash
# Verify skill exists
ls skills/add-version-guard-resource/SKILL.md

# Check YAML frontmatter
head -20 skills/add-version-guard-resource/SKILL.md
```

**Solutions:**
- Ensure you're in the Version Guard OSS repository directory
- Verify `SKILL.md` has valid YAML frontmatter (lines 1-10)
- Check that frontmatter starts with `---` and ends with `---`
- Restart your AI agent session
- For Claude Code: Skill auto-discovery happens when you're in the repo

---

### Prerequisites Not Met

**Symptom**: Skill fails with "infrastructure missing" errors

**Diagnostic:**
```bash
cd ~/Version-Guard

# Check infrastructure
test -f config/resources.yaml && echo "✅ Config exists" || echo "❌ Missing"
test -f pkg/config/loader.go && echo "✅ Loader exists" || echo "❌ Missing"
test -f pkg/detector/generic/detector.go && echo "✅ Detector exists" || echo "❌ Missing"
```

**Solutions:**
- Implement Version Guard Phase 1 (generic infrastructure) first
- Skills cannot work without the foundation in place
- See [config-driven approach documentation](scratch/config-driven-approach/)

---

### Tools Not Available

**Symptom**: Skill fails with "command not found" errors

**Diagnostic:**
```bash
# Check required tools
which curl && echo "✅ curl" || echo "❌ curl missing"
which git && echo "✅ git" || echo "❌ git missing"
which go && echo "✅ go" || echo "❌ go missing"
which make && echo "✅ make" || echo "❌ make missing"
```

**Solutions:**
```bash
# macOS
brew install curl git go
xcode-select --install  # For make

# Linux (Ubuntu/Debian)
sudo apt-get install curl git golang make
```

---

### Tests Failing After Skill Execution

**Symptom**: Skill completes but tests fail

**Diagnostic:**
```bash
# Run tests manually
go test ./pkg/detector/generic/ -v
go test ./pkg/inventory/wiz/ -v
```

**Common Issues:**

1. **Wiz report ID incorrect**
   - Verify report ID in `config/resources.yaml`
   - Check that Wiz report exists and is accessible

2. **EOL product mismatch**
   - Confirm product exists on endoflife.date
   - Check for custom adapter requirements

3. **Schema detection wrong**
   - Manually verify Wiz CSV column names
   - Update `version_field` or `name_field` if needed

**Solution**: Review generated configuration and adjust as needed.

---

## Contributing

We welcome contributions of new skills and improvements to existing ones!

### Adding a New Skill

1. **Create the skill** following the [Skill Development](#skill-development) guide

2. **Test thoroughly**:
   ```bash
   # Test with at least 2 different AI platforms
   claude "Use your-skill-name to [task]"
   goose run your-skill-name
   ```

3. **Document comprehensively**:
   - README.md (user overview)
   - SETUP.md (installation)
   - SKILL.md (AI instructions)
   - examples/ (working examples)
   - references/ (detailed docs)

4. **Open a pull request**:
   ```bash
   git checkout -b add-skill-your-skill-name
   git add skills/your-skill-name/
   git commit -m "Add your-skill-name AI skill

   - Purpose: [What it does]
   - Automates: [Workflow steps]
   - Tested with: Claude Code, Goose
   "
   git push origin add-skill-your-skill-name
   ```

5. **Update this file** (SKILLS.md):
   - Add your skill to the "Available Skills" section
   - Include description, prerequisites, documentation links

### Improving Existing Skills

1. **Test the change**:
   ```bash
   # Make your changes
   vim skills/add-version-guard-resource/SKILL.md

   # Test with AI agent
   claude "Use add-version-guard-resource to add a test resource"
   ```

2. **Document what changed**:
   ```bash
   git commit -m "Improve add-version-guard-resource skill

   - Enhanced: [What you improved]
   - Reason: [Why this is better]
   - Tested: [How you verified it works]
   "
   ```

3. **Open a pull request** with clear description of improvements

### Guidelines

**Quality Standards:**
- Skills must be agent-agnostic (no platform-specific code)
- Include comprehensive documentation
- Provide working examples
- Test with at least 2 AI platforms
- Follow Agent Skills standard (YAML frontmatter, tool allowlist)

**What Makes a Good Skill:**
- **Clear workflow** - Step-by-step instructions
- **Defensive checks** - Validate prerequisites before starting
- **Error handling** - Guide AI on what to do when things fail
- **Progressive disclosure** - Use examples/ to keep core instructions concise
- **Explicit tool allowlist** - Only permit necessary operations

---

## Resources

**Version Guard Documentation:**
- [Main README](README.md)
- [Contributing Guide](CONTRIBUTING.md)
- [Architecture Overview](ARCHITECTURE.md)

**Skill Documentation:**
- [add-version-guard-resource README](skills/add-version-guard-resource/README.md)
- [Detailed Examples](skills/add-version-guard-resource/references/detailed-examples.md)
- [Troubleshooting Guide](skills/add-version-guard-resource/references/troubleshooting.md)

**External Resources:**
- [Agent Skills Standard](https://github.com/agent-skills/specification)
- [endoflife.date API](https://endoflife.date/docs/api/)
- [Version Guard Issues](https://github.com/block/Version-Guard/issues)

---

## Support

**For Skill Issues:**
- Create an issue: [Version Guard Issues](https://github.com/block/Version-Guard/issues)
- Include: AI platform used, skill name, error messages, steps to reproduce

**For Version Guard Issues:**
- See [CONTRIBUTING.md](CONTRIBUTING.md)
- Check existing issues first

**For endoflife.date Coverage:**
- Request new products: [endoflife.date repository](https://github.com/endoflife-date/endoflife.date)
