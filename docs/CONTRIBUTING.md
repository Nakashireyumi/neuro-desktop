# Contributing to Neuro Desktop

> **Thank you for your interest in contributing to Neuro Desktop!**

We welcome contributions of all kinds: bug reports, feature requests, documentation improvements, code contributions, and more.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [How to Contribute](#how-to-contribute)
- [Development Guidelines](#development-guidelines)
- [Pull Request Process](#pull-request-process)
- [Style Guides](#style-guides)
- [Community](#community)

## Code of Conduct

### Our Pledge

We are committed to making participation in this project a harassment-free experience for everyone.

### Our Standards

**Positive behavior includes**:
- Using welcoming and inclusive language
- Being respectful of differing viewpoints
- Gracefully accepting constructive criticism
- Focusing on what is best for the community
- Showing empathy towards other community members

**Unacceptable behavior includes**:
- Trolling, insulting/derogatory comments, and personal or political attacks
- Public or private harassment
- Publishing others' private information without explicit permission
- Other conduct which could reasonably be considered inappropriate

## Getting Started

### Prerequisites

Before contributing, ensure you have:

1. **Development environment** set up (see [DEPLOYMENT.md](DEPLOYMENT.md))
2. **Familiarity** with the project architecture (see [ARCHITECTURE.md](ARCHITECTURE.md))
3. **GitHub account** for submitting contributions

### Fork and Clone

```bash
# 1. Fork the repository on GitHub
# Click "Fork" button on https://github.com/Nakashireyumi/neuro-desktop

# 2. Clone your fork
git clone https://github.com/cassitly/neuro-desktop.git
cd neuro-desktop

# 3. Add upstream remote
git remote add upstream https://github.com/Nakashireyumi/neuro-desktop.git

# 4. Create a branch for your changes
git checkout -b feature/my-awesome-feature
```

### Build and Test

```bash
cd desktop

# Build all components
.\scripts\build-all.ps1  # Windows
./scripts/build-all.sh   # Linux/macOS

# Run tests
cargo test               # Rust tests
go test ./...            # Go tests
npm test                 # Frontend tests (if applicable)
```

## How to Contribute

### Reporting Bugs

**Before submitting**, please:
1. Search existing issues to avoid duplicates
2. Test with the latest version
3. Try to isolate the problem

**When submitting**, include:
- **Clear title**: "Mouse movement fails on 4K displays"
- **Description**: What happened vs what should happen
- **Steps to reproduce**: Detailed, numbered steps
- **Environment**: OS, version, screen resolution
- **Logs**: Relevant error messages or logs
- **Screenshots**: If applicable

**Template**:
```markdown
## Bug Description
Brief description of the bug.

## Steps to Reproduce
1. Start Neuro Desktop
2. Send action: `{"action": "move_mouse_to", "params": {"x": 5000, "y": 5000}}`
3. Observe crash

## Expected Behavior
Mouse should move to screen edge (clamped).

## Actual Behavior
Application crashes with "coordinate out of bounds".

## Environment
- OS: Windows 11
- Version: v0.0.3b-dev
- Screen: 3840x2160 (4K)

## Logs
```
[ERROR] Panic: coordinate overflow
```

### Requesting Features

**Before requesting**, check:
1. Is it already planned? (see [Roadmap](README.md#roadmap))
2. Has someone else requested it?
3. Is it aligned with project goals?

**When requesting**, include:
- **Problem statement**: What problem does this solve?
- **Proposed solution**: How should it work?
- **Alternatives**: What other solutions exist?
- **Use case**: Real-world scenario

**Template**:
```markdown
## Feature Request: OCR Screen Reading

### Problem
Neuro cannot see what's on screen, limiting her ability to interact contextually.

### Proposed Solution
Add an `OCR_READ` action that extracts text from a screen region.

### Alternative Solutions
- Screenshot + external OCR service
- Computer vision model integration

### Use Case
Neuro needs to read error messages from dialog boxes to decide next action.

### Additional Context
This would enable Neuro to play text-based games and navigate complex UIs.
```

### Improving Documentation

Documentation improvements are always welcome!

**Areas needing help**:
- Fixing typos and grammar
- Adding examples and tutorials
- Improving clarity
- Translating to other languages

**Process**:
1. Fork repository
2. Edit markdown files in `docs/`
3. Submit pull request
4. No need to build the project!

### Contributing Code

See [Pull Request Process](#pull-request-process) below.

## Development Guidelines

### Project Structure

```
desktop/
├── apps/              # Applications
│   ├── neuro-desktop/        # Rust main app
│   └── neuro-integration/    # Go WebSocket client
├── backend/           # Backend services
│   └── python/controller/    # Python control drivers
├── frontend/          # Web UI (TypeScript/Vite)
├── config/            # Configuration
├── scripts/           # Build and deployment scripts
├── docs/              # Documentation
└── tests/             # Integration tests
```

### Coding Standards

#### Rust

**Follow Rust conventions**:
```rust
// Use descriptive names
fn process_ipc_command() -> Result<()>  // ✓
fn proc() -> Result<()>                 // ✗

// Document public APIs
/// Processes an IPC command from the queue.
///
/// # Arguments
/// * `command` - The command to process
///
/// # Errors
/// Returns error if command is invalid
pub fn process_command(command: &str) -> Result<()>

// Handle errors properly
let result = some_operation()?;  // ✓
let result = some_operation().unwrap();  // ✗ (avoid unwrap)
```

**Run formatters**:
```bash
cargo fmt
cargo clippy
```

#### Go

**Follow Go conventions**:
```go
// Use camelCase for private, PascalCase for public
func processAction(action Action) error  // ✓
func ProcessAction(action Action) error  // ✗ (should be private)

// Document exported functions
// ProcessAction handles incoming Neuro actions
func ProcessAction(action Action) error

// Handle errors explicitly
if err != nil {
    return fmt.Errorf("failed to process: %w", err)
}
```

**Run formatters**:
```bash
go fmt ./...
go vet ./...
```

#### Python

**Follow PEP 8**:
```python
# Use snake_case
def process_mouse_action(x: int, y: int):  # ✓
def ProcessMouseAction(x: int, y: int):    # ✗

# Type hints
def move_mouse(x: int, y: int) -> None:  # ✓

# Docstrings
def move_mouse(x: int, y: int) -> None:
    """
    Move mouse cursor to specified coordinates.
    
    Args:
        x: X coordinate in pixels
        y: Y coordinate in pixels
    """
```

**Run formatters**:
```bash
black backend/python/controller
pylint backend/python/controller
```

#### TypeScript

**Follow TypeScript best practices**:
```typescript
// Use interfaces for objects
interface MouseAction {
  x: number;
  y: number;
}

// Use const for immutable
const config: Config = loadConfig();  // ✓
let config: Config = loadConfig();    // ✗

// Explicit types
function moveMouse(action: MouseAction): void  // ✓
function moveMouse(action)                     // ✗
```

### Testing Guidelines

**Write tests for**:
- New features
- Bug fixes
- Edge cases

**Test structure**:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_mouse_move_valid_coordinates() {
        let cmd = IPCCommand::MoveMouseTo { x: 100, y: 200 };
        assert!(cmd.validate().is_ok());
    }
    
    #[test]
    fn test_mouse_move_negative_coordinates() {
        let cmd = IPCCommand::MoveMouseTo { x: -10, y: -10 };
        assert!(cmd.validate().is_err());
    }
}
```

**Run tests before submitting**:
```bash
cargo test
go test ./...
pytest backend/python/tests
```

### Commit Messages

**Format**:
```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Formatting, missing semicolons, etc.
- `refactor`: Code change that neither fixes nor adds
- `perf`: Performance improvement
- `test`: Adding or updating tests
- `chore`: Updating build tasks, package manager configs, etc.

**Examples**:

```
feat(mouse): add support for normalized coordinates

Adds MOVE_N and CLICK_N commands that use 0.0-1.0 coordinate ranges
instead of absolute pixels. This makes scripts resolution-independent.

Closes #42
```

```
fix(ipc): handle malformed JSON gracefully

Previously, malformed JSON would crash the IPC handler. Now it logs
the error and continues processing.

Fixes #87
```

```
docs(readme): add troubleshooting section

Added common issues and solutions to help new users.
```

### Branching Strategy

**Main branches**:
- `main` - Stable, production-ready code
- `develop` - Integration branch for features

**Feature branches**:
- `feature/feature-name` - New features
- `fix/bug-name` - Bug fixes
- `docs/doc-topic` - Documentation
- `refactor/component-name` - Refactoring

**Example workflow**:
```bash
# Create feature branch from develop
git checkout develop
git pull upstream develop
git checkout -b feature/add-ocr-support

# Make changes, commit
git add .
git commit -m "feat(ocr): add OCR screen reading support"

# Push to your fork
git push origin feature/add-ocr-support

# Create pull request on GitHub
```

## Pull Request Process

### Before Submitting

**Checklist**:
- [ ] Code follows style guidelines
- [ ] All tests pass
- [ ] New tests added for new features
- [ ] Documentation updated
- [ ] Commit messages are clear
- [ ] No merge conflicts with `develop`

### Submitting

1. **Push to your fork**:
   ```bash
   git push origin feature/my-feature
   ```

2. **Create pull request** on GitHub:
   - Base: `upstream/develop`
   - Compare: `your-fork/feature/my-feature`

3. **Fill out PR template**:
   ```markdown
   ## Description
   Brief description of changes.
   
   ## Type of Change
   - [ ] Bug fix
   - [ ] New feature
   - [ ] Documentation update
   - [ ] Refactoring
   
   ## Testing
   Describe how you tested this.
   
   ## Checklist
   - [ ] Code follows style guidelines
   - [ ] Tests added/updated
   - [ ] Documentation updated
   ```

4. **Respond to reviews**:
   - Address all feedback
   - Make requested changes
   - Explain disagreements respectfully

### Review Process

**What reviewers look for**:
- Code quality and clarity
- Test coverage
- Performance impact
- Security implications
- Documentation completeness

**Timeline**:
- Initial review: Within 3-5 days
- Follow-up reviews: 1-2 days
- Merge: After approval from 1+ maintainers

### After Merge

**Clean up**:
```bash
# Delete local branch
git branch -d feature/my-feature

# Delete remote branch
git push origin --delete feature/my-feature

# Update local develop
git checkout develop
git pull upstream develop
```

## Style Guides

### Code Comments

```rust
// Good: Explain WHY, not WHAT
// Clamp to prevent coordinate overflow on multi-monitor setups
let x = x.clamp(0, screen_width - 1);

// Bad: States the obvious
// Clamp x
let x = x.clamp(0, screen_width - 1);
```

### Error Messages

```rust
// Good: Actionable, specific
return Err("Failed to parse IPC command: missing 'type' field");

// Bad: Vague, unhelpful
return Err("Error");
```

### Variable Naming

```rust
// Good: Descriptive
let mouse_x_coordinate = 500;
let action_execution_timeout_ms = 1000;

// Bad: Too short or cryptic
let x = 500;
let t = 1000;
```

### File Organization

```rust
// Organize imports
use std::fs;           // Standard library
use anyhow::Result;    // External crates
use crate::config;     // Local modules

// Group related functions
impl IPCHandler {
    // Public API first
    pub fn new() -> Self { }
    pub fn process() -> Result<()> { }
    
    // Private helpers after
    fn validate() -> bool { }
    fn execute() -> Result<()> { }
}
```

## Community

### Communication Channels

- **GitHub Issues**: Bug reports, feature requests
- **GitHub Discussions**: General questions, ideas
- **Discord**: Real-time chat (link in README)

### Getting Help

**Before asking**:
1. Check documentation
2. Search existing issues/discussions
3. Try to solve it yourself

**When asking**:
- Be specific
- Show what you've tried
- Include error messages
- Be patient and respectful

### Recognition

Contributors are recognized in:
- `CONTRIBUTORS.md` file
- Release notes
- Project README (for significant contributions)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

## Questions?

If you have questions about contributing, feel free to:
- Open a discussion on GitHub
- Reach out on Discord
- Contact maintainers

---

**Thank you for contributing to Neuro Desktop!** 🎉

Every contribution, no matter how small, helps make this project better for everyone.
