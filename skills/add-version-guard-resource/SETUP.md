# Add Version Guard Resource — Setup

This skill enables AI agents to automatically add new cloud resource types to Version Guard by generating configuration from endoflife.date and Wiz inventory schemas.

## Prerequisites

### 1. Version Guard Generic Infrastructure

This skill requires Version Guard's generic config-driven architecture to be implemented.

**Check if infrastructure exists:**

```bash
cd ~/Version-Guard

# Verify config schema exists
test -f config/resources.yaml && echo "✅ Config schema exists" || echo "❌ Config schema missing"

# Verify loader exists
test -f pkg/config/loader.go && echo "✅ Config loader exists" || echo "❌ Config loader missing"

# Verify generic detector exists
test -f pkg/detector/generic/detector.go && echo "✅ Generic detector exists" || echo "❌ Generic detector missing"

# Verify generic inventory exists
test -f pkg/inventory/wiz/generic.go && echo "✅ Generic inventory exists" || echo "❌ Generic inventory missing"

# Verify EOL adapters exist
test -f pkg/eol/endoflife/adapters.go && echo "✅ EOL adapters exist" || echo "❌ EOL adapters missing"
```

**If any check fails**, you need to implement Phase 1 first:

**Phase 1 must be complete before using this skill.**

---

### 2. Required Tools

```bash
# Check curl (for endoflife.date API)
which curl || echo "❌ Install curl: brew install curl"

# Check git (for commits)
which git || echo "❌ Install git: brew install git"

# Check go (for testing)
which go || echo "❌ Install go: brew install go"

# Check make (for test suite)
which make || echo "❌ Install make: xcode-select --install"
```

All tools should show a path (e.g., `/usr/bin/curl`). If any show "not found", install them.

---

### 3. Wiz Credentials (Optional)

**Not required for this skill** - the skill generates configuration from examples and doesn't need live Wiz access.

For testing Version Guard with real Wiz data later, you can set:

```bash
export WIZ_CLIENT_ID_SECRET="your-wiz-client-id"
export WIZ_CLIENT_SECRET_SECRET="your-wiz-client-secret"
```

---

## Installation

### For Claude Code

The skill is auto-discovered from the Version Guard repository:

```bash
# Verify skill location
ls ~/Version-Guard/skills/add-version-guard-resource/SKILL.md

# Claude Code will discover skills in the skills/ directory automatically
```

### For Goose

Consult Goose documentation for skill configuration. Goose may auto-discover skills from the `skills/` directory or require specific setup.

### For Amp

The skill is already available in the repository's skills directory:

```bash
# From Version Guard repo root
cd ~/Version-Guard

# Verify skill exists
ls skills/add-version-guard-resource/SKILL.md
```

Amp will auto-discover skills in the `skills/` directory.

### For Other AI Tools

The skill is available in the repository and can be referenced or copied:

```bash
# The skill is located at:
# ~/Version-Guard/skills/add-version-guard-resource/

# To use with your agent, consult your tool's documentation
# You may need to copy it to your agent's skills directory:
cp -r ~/Version-Guard/skills/add-version-guard-resource \
      ~/.config/agents/skills/
```

Consult your AI tool's documentation for the correct skills directory.

---

## Verify Setup

Run all prerequisite checks:

```bash
cd ~/Version-Guard

# Check infrastructure
test -f config/resources.yaml && \
test -f pkg/config/loader.go && \
test -f pkg/detector/generic/detector.go && \
test -f pkg/inventory/wiz/generic.go && \
test -f pkg/eol/endoflife/adapters.go && \
echo "✅ Ready to use add-version-guard-resource skill" || \
echo "❌ Generic infrastructure not implemented yet - see Phase 1 documentation"

# Check tools
which curl && which git && which go && which make && \
echo "✅ All required tools available" || \
echo "❌ Some tools missing - install them first"
```

If both checks show ✅, you're ready to use the skill.

---

## Quick Check

Test the skill is discoverable by your AI agent:

### Claude Code

In a conversation, type:
```
Do we have a skill for adding resources to Version Guard?
```

Claude should respond mentioning the `add-version-guard-resource` skill.

### Goose

Consult Goose documentation for listing available skills in your session.

---

## Usage

Once setup is complete, you can use the skill by asking your AI agent:

- "Add OpenSearch support to Version Guard"
- "Add RDS PostgreSQL to Version Guard"
- "Create Version Guard support for ElastiCache Redis"
- "Enable Aurora MySQL detection in Version Guard"

The AI agent will autonomously:
1. Validate endoflife.date coverage
2. Ask for Wiz report ID
3. Generate YAML configuration
4. Run tests
5. Create commit

---

## Troubleshooting

### Skill Not Discovered

**Symptom**: AI agent doesn't recognize the skill

**Solutions**:
- Verify skill exists: `ls ~/Version-Guard/skills/add-version-guard-resource/SKILL.md`
- Check `SKILL.md` has valid YAML frontmatter
- Restart your AI agent session
- For Claude Code: Skill should be auto-discovered from `skills/` directory when in the repository

### Infrastructure Missing

**Symptom**: Prerequisite checks fail

**Solution**:
- You need to implement Version Guard Phase 1 first
- The skill cannot work without the generic infrastructure in place

### Tools Not Found

**Symptom**: `which curl` or other tools show "not found"

**Solutions**:
```bash
# macOS
brew install curl git go
xcode-select --install  # For make

# Linux (Ubuntu/Debian)
sudo apt-get install curl git golang make
```

---

## Next Steps

After successful setup:

1. Navigate to Version Guard repository:
   ```bash
   cd ~/Version-Guard
   ```

2. Try adding a resource:
   ```
   Ask your AI: "Add Aurora PostgreSQL support to Version Guard"
   ```

3. The skill will guide you through the process autonomously

---

## Support

- **Skill issues**: Create issue at https://github.com/block/Version-Guard/issues
- **Version Guard issues**: https://github.com/block/Version-Guard/issues
- **endoflife.date coverage**: https://github.com/endoflife-date/endoflife.date
