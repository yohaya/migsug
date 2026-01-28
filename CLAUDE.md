# Claude Code Configuration

## MANDATORY: Auto-commit, Build, and Deploy

After making ANY code changes to this repository, ALWAYS do ALL of these steps automatically:
1. Stage all modified files with `git add`
2. Commit with a descriptive message
3. Pull with rebase if needed (`git pull --rebase origin main`)
4. Push to GitHub (`git push origin main`)
5. Wait for GitHub Actions workflow to complete using `gh run watch`
6. Verify the workflow succeeded and binaries were built
7. Only then report completion to the user

DO NOT ask for confirmation - execute this workflow automatically after EVERY code change.
DO NOT report completion until the GitHub Actions workflow has finished successfully.

## Commit Message Format

Use descriptive commit messages following this format:
- `fix:` for bug fixes
- `feat:` for new features
- `chore:` for maintenance tasks
- `refactor:` for code refactoring

## Project Context

This is a Proxmox VM Migration Suggestion Tool (migsug) - a Go TUI application that suggests optimal VM migrations based on resource availability.
