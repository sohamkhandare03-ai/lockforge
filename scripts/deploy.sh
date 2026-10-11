#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$DIR"

echo "=========================================================================="
echo "          LOCKFORGE – GITHUB & ONLINE CLOUD DEPLOYMENT HELPER             "
echo "=========================================================================="

# Check if git is initialized
if [ ! -d ".git" ]; then
  echo "Initializing git repository..."
  git init -b main
fi

# Check git status
echo -e "\n[1/3] Preparing git repository status..."
git status --short

# Check if remote exists
REMOTE_URL=$(git remote get-url origin 2>/dev/null || echo "")

if [ -n "$REMOTE_URL" ]; then
  echo -e "\nConfigured GitHub Remote: $REMOTE_URL"
  echo "Ready to push updates."
  read -p "Do you want to commit and push changes now? (y/n): " CONFIRM
  if [[ "$CONFIRM" =~ ^[Yy]$ ]]; then
    git add .
    git commit -m "feat: complete LockForge distributed lock manager with CI/CD and deployment configs" || true
    git push -u origin main
    echo -e "\nSuccessfully pushed to GitHub! CI pipeline is now running."
  fi
else
  echo -e "\n[2/3] No git remote 'origin' configured yet."
  echo "You can set up your GitHub repository in 1 of 2 ways:"
  echo ""
  echo "  Option A (Recommended): Create repo via GitHub CLI (gh)"
  echo "    gh auth login"
  echo "    gh repo create lockforge --public --source=. --remote=origin --push"
  echo ""
  echo "  Option B: Create an empty repo on https://github.com/new and run:"
  echo "    git remote add origin https://github.com/<your-username>/lockforge.git"
  echo "    git push -u origin main"
fi

echo -e "\n[3/3] Online Cloud Deployment:"
echo "Once pushed to GitHub, deploy for free with 1 click on Render or Railway:"
echo "  • Render:  Sign into https://dashboard.render.com -> New Web Service -> select 'lockforge'"
echo "  • Railway: Sign into https://railway.app -> New Project -> select 'lockforge'"
echo "See docs/DEPLOYMENT.md for full details."
echo "=========================================================================="
