#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$DIR"

echo "=========================================================================="
echo "    LOCKFORGE – FULL END-TO-END AUTOMATED GITHUB & RENDER DEPLOYER        "
echo "=========================================================================="

GITHUB_USER="${1:-$GITHUB_USER}"
GITHUB_TOKEN="${2:-$GITHUB_TOKEN}"
RENDER_API_KEY="${3:-$RENDER_API_KEY}"

# Prompt for GitHub credentials if not provided
if [ -z "$GITHUB_USER" ]; then
  read -p "Enter your GitHub username: " GITHUB_USER
fi

if [ -z "$GITHUB_TOKEN" ]; then
  read -s -p "Enter your GitHub Personal Access Token (repo scope): " GITHUB_TOKEN
  echo ""
fi

if [ -z "$GITHUB_USER" ] || [ -z "$GITHUB_TOKEN" ]; then
  echo "Error: GitHub username and token are required."
  exit 1
fi

echo -e "\n[Step 1/4] Verifying GitHub authentication..."
AUTH_CHECK=$(curl -s -H "Authorization: token $GITHUB_TOKEN" -H "Accept: application/vnd.github.v3+json" https://api.github.com/user)
LOGIN_USER=$(echo "$AUTH_CHECK" | grep -o '"login": *"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")

if [ -z "$LOGIN_USER" ]; then
  echo "Error: GitHub authentication failed. Please check your Personal Access Token."
  echo "Server response: $AUTH_CHECK"
  exit 1
fi

echo "Authenticated as: $LOGIN_USER"

echo -e "\n[Step 2/4] Creating GitHub repository 'lockforge' (if not already existing)..."
REPO_RESP=$(curl -s -X POST \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3+json" \
  https://api.github.com/user/repos \
  -d '{"name":"lockforge","description":"High-Assurance Distributed Lock Manager with Raft Consensus, Leases, and Fencing Tokens","private":false,"has_issues":true,"has_wiki":true}')

REPO_URL="https://github.com/$LOGIN_USER/lockforge.git"
echo "Repository target: https://github.com/$LOGIN_USER/lockforge"

echo -e "\n[Step 3/4] Pushing code to GitHub..."
git remote remove origin 2>/dev/null || true
git remote add origin "https://${GITHUB_TOKEN}@github.com/${LOGIN_USER}/lockforge.git"

# Ensure on main
git branch -M main

# Push to GitHub
git push -u origin main --force

# Replace remote with clean URL without embedding token in git config
git remote set-url origin "https://github.com/${LOGIN_USER}/lockforge.git"

echo "Code successfully pushed to https://github.com/$LOGIN_USER/lockforge !"
echo "GitHub Actions CI pipeline has been automatically triggered."

echo -e "\n[Step 4/4] Deploying to Render..."

if [ -n "$RENDER_API_KEY" ]; then
  echo "Render API key detected. Querying Render owner account..."
  OWNERS_RESP=$(curl -s -H "Authorization: Bearer $RENDER_API_KEY" https://api.render.com/v1/owners)
  OWNER_ID=$(echo "$OWNERS_RESP" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")

  if [ -n "$OWNER_ID" ]; then
    echo "Render Owner ID: $OWNER_ID"
    echo "Creating Render Web Service for 'lockforge'..."

    SERVICE_PAYLOAD=$(cat <<EOF
{
  "type": "web_service",
  "name": "lockforge",
  "ownerId": "$OWNER_ID",
  "repo": "https://github.com/$LOGIN_USER/lockforge",
  "branch": "main",
  "autoDeploy": "yes",
  "serviceDetails": {
    "env": "docker",
    "dockerfilePath": "./Dockerfile",
    "plan": "free",
    "region": "oregon",
    "envVars": [
      {"key": "HTTP_PORT", "value": "8080"},
      {"key": "GRPC_PORT", "value": "50051"}
    ]
  }
}
EOF
)

    CREATE_RESP=$(curl -s -X POST \
      -H "Authorization: Bearer $RENDER_API_KEY" \
      -H "Content-Type: application/json" \
      https://api.render.com/v1/services \
      -d "$SERVICE_PAYLOAD")

    SERVICE_URL=$(echo "$CREATE_RESP" | grep -o '"service": *{[^}]*"serviceDetails": *{[^}]*"url": *"[^"]*"' | grep -o 'https://[^"]*' | head -1 || echo "")
    SERVICE_ID=$(echo "$CREATE_RESP" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")

    if [ -n "$SERVICE_ID" ]; then
      echo "Render Web Service created successfully! (Service ID: $SERVICE_ID)"
      if [ -n "$SERVICE_URL" ]; then
        echo -e "\nYour live service will be available at: $SERVICE_URL"
      fi
    else
      echo "Note from Render API: $CREATE_RESP"
      echo "If GitHub account is not linked in Render dashboard, use the 1-click deploy link below."
    fi
  else
    echo "Could not fetch Render owner. Response: $OWNERS_RESP"
  fi
fi

echo -e "\n=========================================================================="
echo "                  DEPLOYMENT AUTOMATION SUMMARY                           "
echo "=========================================================================="
echo "  GitHub Repo: https://github.com/$LOGIN_USER/lockforge"
echo "  CI Pipeline: https://github.com/$LOGIN_USER/lockforge/actions"
echo ""
echo "  Render 1-Click Deploy Link:"
echo "  👉 https://render.com/deploy?repo=https://github.com/$LOGIN_USER/lockforge"
echo "=========================================================================="
