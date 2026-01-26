# Claude Code Configuration

## MANDATORY: Auto-commit, Build, and Deploy

After making ANY code changes to this repository, ALWAYS do ALL of these steps automatically:
1. Stage all modified files with `git add`
2. Commit with a descriptive message
3. Pull with rebase if needed (`git pull --rebase origin main`)
4. Push to GitHub (`git push origin main`)
5. This triggers GitHub Actions to compile binaries for all platforms

DO NOT ask for confirmation - execute this workflow automatically after EVERY code change.

## Commit Message Format

Use descriptive commit messages following this format:
- `fix:` for bug fixes
- `feat:` for new features
- `chore:` for maintenance tasks
- `refactor:` for code refactoring

## Project Context

This is a Proxmox VM Migration Suggestion Tool (migsug) - a Go TUI application that suggests optimal VM migrations based on resource availability.
