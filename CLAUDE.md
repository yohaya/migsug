# Claude Code Configuration

## Auto-commit and Deploy

After making ANY code changes to this repository:
1. Automatically stage, commit, and push changes to GitHub
2. This triggers the GitHub Actions workflow to build binaries
3. Do NOT ask for confirmation - just do it automatically

## Commit Message Format

Use descriptive commit messages following this format:
- `fix:` for bug fixes
- `feat:` for new features
- `chore:` for maintenance tasks
- `refactor:` for code refactoring

## Project Context

This is a Proxmox VM Migration Suggestion Tool (migsug) - a Go TUI application that suggests optimal VM migrations based on resource availability.
