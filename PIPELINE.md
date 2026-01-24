# Automated Version & Release Pipeline

This repository includes an automated pipeline that manages versioning and releases for the `migsug.sh` script.

## How It Works

### Local Automation (Git Hooks)

**Pre-Commit Hook** (`.git/hooks/pre-commit`)
- Automatically detects when `migsug.sh` is modified
- Bumps the version number (patch version by default)
- Updates the "Last updated" date
- Stages the updated file for commit

**Post-Commit Hook** (`.git/hooks/post-commit`)
- Automatically pushes commits to GitHub when `migsug.sh` is modified
- Ensures your changes are always synced with the remote repository

### Cloud Automation (GitHub Actions)

**Version Update Workflow** (`.github/workflows/version-update.yml`)
- Triggers when `migsug.sh` is pushed to the main branch
- Extracts the current version from the script
- Creates a Git tag (e.g., `v1.0.1`)
- Creates a GitHub Release with automatic release notes

## Version Bump Script

The `.scripts/bump-version.sh` script can be run manually:

```bash
# Bump patch version (1.0.0 -> 1.0.1)
.scripts/bump-version.sh patch

# Bump minor version (1.0.0 -> 1.1.0)
.scripts/bump-version.sh minor

# Bump major version (1.0.0 -> 2.0.0)
.scripts/bump-version.sh major
```

## Workflow Example

1. **Edit migsug.sh**
   ```bash
   vim migsug.sh
   # Make your changes
   ```

2. **Commit changes**
   ```bash
   git add migsug.sh
   git commit -m "Add new feature to migsug"
   ```
   - Pre-commit hook automatically bumps version to (e.g.) 1.0.1
   - Post-commit hook automatically pushes to GitHub

3. **GitHub Actions triggers**
   - Creates tag `v1.0.1`
   - Creates GitHub Release

## Disabling Auto-Push

If you want to review changes before pushing, you can disable the post-commit hook:

```bash
chmod -x .git/hooks/post-commit
```

Re-enable it with:
```bash
chmod +x .git/hooks/post-commit
```

## Manual Version Bump

If you want to bump the version without editing the script:

```bash
.scripts/bump-version.sh minor
git add migsug.sh
git commit -m "Bump version to $(grep VERSION migsug.sh | sed 's/# VERSION: //')"
```

## Versioning Scheme

This project uses [Semantic Versioning](https://semver.org/):
- **MAJOR**: Incompatible changes
- **MINOR**: New features (backwards compatible)
- **PATCH**: Bug fixes (backwards compatible)
